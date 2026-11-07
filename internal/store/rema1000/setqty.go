package rema1000

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// The bodies sent to REMA, field for field as the webshop's own list code sends them.
type (
	newList struct {
		Name    string `json:"name"`
		Primary bool   `json:"primary"` // always false: the human's own list stays primary
		Items   []any  `json:"items"`
	}
	newItem struct {
		Amount      int   `json:"amount"`
		StoreID     int   `json:"store_id"` // 1 is fixed, as in REMA's own SDK; it is not a store choice
		StoreItemID int64 `json:"store_item_id"`
	}
	itemPatch struct {
		Amount int   `json:"amount"`
		Bought *bool `json:"bought,omitempty"` // false, only to bring back a line bought in the app
	}
	removeItem struct {
		ID     json.Number `json:"id"`
		Delete bool        `json:"delete"`
	}
	removeBody struct {
		Items []removeItem `json:"items"`
	}
)

const primaryWarning = "REMA gjorde handle-mcp-listen primær. Gør din egen liste primær igen i appen."

// SetQuantities sets absolute amounts on today's list; 0 removes the line. Every amount is read
// back and compared, as in the other stores. If today's list does not exist it is created, never
// as the primary list. It never orders or pays.
func (s *Store) SetQuantities(ctx context.Context, items []store.CartItem) ([]model.QuantityResult, error) {
	if _, err := s.ready(ctx); err != nil {
		return nil, err
	}
	results := make([]model.QuantityResult, len(items))
	want := map[string]int{} // the last amount asked for, per product
	var order []string
	needList := false
	failed := map[string]string{}
	for i, it := range items {
		results[i] = model.QuantityResult{ProductID: it.ProductID, Requested: it.Quantity}
		if id, err := strconv.ParseInt(it.ProductID, 10, 64); err != nil || id <= 0 || it.Quantity < 0 {
			failed[it.ProductID] = "Ugyldigt varenummer eller antal."
			continue
		}
		if _, seen := want[it.ProductID]; !seen {
			order = append(order, it.ProductID)
		}
		want[it.ProductID] = it.Quantity
		needList = needList || it.Quantity > 0
	}
	applied, warn, err := s.apply(ctx, order, want, needList, failed)
	if err != nil {
		return nil, err
	}
	for i := range results {
		r := &results[i]
		if msg, bad := failed[r.ProductID]; bad {
			r.Message = &msg
			continue
		}
		n := applied[r.ProductID]
		r.Applied = &n
		r.OK = n == r.Requested && want[r.ProductID] == r.Requested
		switch {
		case !r.OK:
			msg := fmt.Sprintf("Listen har %d i stedet for %d.", n, r.Requested)
			r.Message = &msg
		case warn != "":
			r.Message = &warn
		}
	}
	return results, nil
}

// addAttempts is how often a POST that creates something may be sent. Each repeat comes only
// after the list was read again and showed the first one did not land.
const addAttempts = 3

// apply does the changes and returns what each product ends at. failed gets the products REMA
// refused. warn is a text for the human, set when the new list is primary. One call at a time,
// from finding today's list to the read-back, so two calls never both create it.
func (s *Store) apply(ctx context.Context, order []string, want map[string]int, needList bool, failed map[string]string) (map[string]int, string, error) {
	applied := map[string]int{}
	if len(order) == 0 {
		return applied, "", nil
	}
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	list, err := s.findToday(ctx)
	if err != nil {
		return nil, "", err
	}
	warn := ""
	switch {
	case list == nil && !needList:
		return applied, "", nil // nothing to remove from a list that is not there
	case list == nil:
		if list, err = s.createToday(ctx); err != nil {
			return nil, "", err
		}
	}
	full, err := s.readList(ctx, list.ID)
	if err != nil {
		return nil, "", err
	}
	if full.Primary {
		warn = primaryWarning
	}
	have := map[string][]apiItem{}
	for _, it := range full.Items {
		if it.StoreItemID != "" {
			have[string(it.StoreItemID)] = append(have[string(it.StoreItemID)], it)
		}
	}
	var gone []removeItem
	for _, id := range order {
		g, err := s.reconcile(ctx, list.ID, id, want[id], have[id], failed)
		if err != nil {
			return nil, "", err
		}
		gone = append(gone, g...)
	}
	if len(gone) > 0 {
		if _, err := s.write(ctx, call{method: "PATCH", path: "v2/shoppinglists/" + string(list.ID),
			body: removeBody{Items: gone}}, "", failed); err != nil {
			return nil, "", err
		}
	}
	back, err := s.readList(ctx, list.ID)
	if err != nil {
		return nil, "", err
	}
	for _, it := range back.Items {
		if !it.Bought && it.StoreItemID != "" {
			applied[string(it.StoreItemID)] += qty(it.Amount) // a product on several lines counts as their sum
		}
	}
	return applied, warn, nil
}

// reconcile brings one product to the amount q, given its lines now. The first line is set and any
// extra line is returned for removal. A product with only bought lines gets the first one back.
func (s *Store) reconcile(ctx context.Context, listID json.Number, id string, q int, lines []apiItem, failed map[string]string) ([]removeItem, error) {
	var active, bought []apiItem
	for _, it := range lines {
		if it.Bought {
			bought = append(bought, it)
		} else {
			active = append(active, it)
		}
	}
	itemPath := func(it apiItem) string { return "v2/shoppinglists/" + string(listID) + "/items/" + string(it.ID) }
	var gone []removeItem
	switch {
	case q == 0:
		for _, it := range active {
			gone = append(gone, removeItem{ID: it.ID, Delete: true})
		}
	case len(active) > 0:
		for _, it := range active[1:] {
			gone = append(gone, removeItem{ID: it.ID, Delete: true})
		}
		if qty(active[0].Amount) != q {
			_, err := s.write(ctx, call{method: "PATCH", path: itemPath(active[0]), body: itemPatch{Amount: q}}, id, failed)
			return gone, err
		}
	case len(bought) > 0:
		no := false
		_, err := s.write(ctx, call{method: "PATCH", path: itemPath(bought[0]), body: itemPatch{Amount: q, Bought: &no}}, id, failed)
		return nil, err
	default:
		found, err := s.addLine(ctx, listID, id, q, failed)
		if err != nil || len(found) == 0 {
			return nil, err
		}
		return s.reconcile(ctx, listID, id, q, found, failed) // an unsure POST that did land: set its amount
	}
	return gone, nil
}

// addLine adds a product with a POST that is never retried by the sender. When the answer is lost
// (timeout, network error, 5xx) it reads the list again and only POSTs again if the product is
// still not there. It returns the product's lines when an earlier POST turned out to have landed.
func (s *Store) addLine(ctx context.Context, listID json.Number, id string, q int, failed map[string]string) ([]apiItem, error) {
	n, _ := strconv.ParseInt(id, 10, 64)
	c := call{method: "POST", path: "v2/shoppinglists/" + string(listID) + "/items", once: true,
		body: newItem{Amount: q, StoreID: 1, StoreItemID: n}}
	for attempt := 0; attempt < addAttempts; attempt++ {
		unsure, err := s.write(ctx, c, id, failed)
		if err != nil || !unsure {
			return nil, err
		}
		full, err := s.readList(ctx, listID)
		if err != nil {
			return nil, err
		}
		var found []apiItem
		for _, it := range full.Items {
			if string(it.StoreItemID) == id {
				found = append(found, it)
			}
		}
		if len(found) > 0 {
			return found, nil
		}
	}
	return nil, store.Errorf("REMA 1000 svarer ikke, og varen kom ikke på listen. Prøv igen om lidt.")
}

// write sends one change. A 4xx is REMA refusing that product: it is noted and the rest goes on.
// Any other failure stops everything. For a call with once set, a lost answer or a 5xx is not an
// error but unsure: the caller must read the list before it decides to send it again.
func (s *Store) write(ctx context.Context, c call, product string, failed map[string]string) (unsure bool, err error) {
	resp, unsure, err := s.send(ctx, c)
	if err != nil || unsure {
		return unsure, err
	}
	switch {
	case isOK(resp):
		return false, nil
	case resp.Status >= 400 && resp.Status < 500 && product != "":
		failed[product] = fmt.Sprintf("REMA 1000 afviste varen (%d). Findes den i sortimentet?", resp.Status)
		return false, nil
	}
	return false, store.Errorf("REMA 1000 afviste ændringen af listen (%d).", resp.Status)
}

// send is one authorised request. For a call with once set, unsure is true when the answer was lost
// or was a 429 or 5xx, because the change may or may not have been made.
func (s *Store) send(ctx context.Context, c call) (resp *store.Response, unsure bool, err error) {
	c.auth = true
	resp, err = s.api(ctx, c)
	var se *store.StoreError
	switch {
	case c.once && errors.As(err, &se) && se.Unsure:
		return nil, true, nil
	case err != nil:
		return nil, false, err
	case c.once && (resp.Status >= 500 || resp.Status == 429):
		return nil, true, nil
	}
	return resp, false, nil
}

// createToday makes today's list, never as primary, and reads it back by name. A lost answer is
// followed by a read of the lists, and a second POST only if the list is still not there.
func (s *Store) createToday(ctx context.Context) (*apiList, error) {
	body := newList{Name: s.listName(), Primary: false, Items: []any{}}
	for attempt := 0; attempt < addAttempts; attempt++ {
		resp, unsure, err := s.send(ctx, call{method: "POST", path: "v2/shoppinglists", body: body, once: true})
		if err != nil {
			return nil, err
		}
		if !unsure && !isOK(resp) {
			return nil, store.Errorf("REMA 1000 afviste at oprette listen %q (%d).", s.listName(), resp.Status)
		}
		list, err := s.findToday(ctx)
		if err != nil {
			return nil, err
		}
		if list != nil {
			return list, nil
		}
		if !unsure {
			return nil, store.Errorf("REMA 1000: listen %q blev ikke fundet efter oprettelsen. API'et kan have ændret sig.", s.listName())
		}
	}
	return nil, store.Errorf("REMA 1000 svarer ikke, og listen %q blev ikke oprettet. Prøv igen om lidt.", s.listName())
}
