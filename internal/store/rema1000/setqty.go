package rema1000

import (
	"context"
	"encoding/json"
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

// apply does the changes and returns what each product ends at. failed gets the products REMA
// refused. warn is a text for the human, set when the list had to be created.
func (s *Store) apply(ctx context.Context, order []string, want map[string]int, needList bool, failed map[string]string) (map[string]int, string, error) {
	applied := map[string]int{}
	if len(order) == 0 {
		return applied, "", nil
	}
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
	have := map[string]apiItem{}
	for _, it := range full.Items {
		if it.StoreItemID != "" {
			have[string(it.StoreItemID)] = it
		}
	}
	var gone []removeItem
	for _, id := range order {
		it, there := have[id]
		var err error
		switch q := want[id]; {
		case q == 0:
			if there && !it.Bought {
				gone = append(gone, removeItem{ID: it.ID, Delete: true})
			}
		case !there:
			n, _ := strconv.ParseInt(id, 10, 64)
			err = s.write(ctx, call{method: "POST", path: "v2/shoppinglists/" + string(list.ID) + "/items",
				body: newItem{Amount: q, StoreID: 1, StoreItemID: n}}, id, failed)
		case it.Bought || qty(it.Amount) != q:
			patch := itemPatch{Amount: q}
			if it.Bought {
				no := false
				patch.Bought = &no
			}
			err = s.write(ctx, call{method: "PATCH", path: "v2/shoppinglists/" + string(list.ID) + "/items/" + string(it.ID),
				body: patch}, id, failed)
		}
		if err != nil {
			return nil, "", err
		}
	}
	if len(gone) > 0 {
		if err := s.write(ctx, call{method: "PATCH", path: "v2/shoppinglists/" + string(list.ID),
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
			applied[string(it.StoreItemID)] = qty(it.Amount)
		}
	}
	return applied, warn, nil
}

// write sends one change. A 4xx is REMA refusing that product: it is noted and the rest goes on.
// Any other failure stops everything.
func (s *Store) write(ctx context.Context, c call, product string, failed map[string]string) error {
	c.auth = true
	resp, err := s.api(ctx, c)
	if err != nil {
		return err
	}
	switch {
	case isOK(resp):
		return nil
	case resp.Status >= 400 && resp.Status < 500 && product != "":
		failed[product] = fmt.Sprintf("REMA 1000 afviste varen (%d). Findes den i sortimentet?", resp.Status)
		return nil
	}
	return store.Errorf("REMA 1000 afviste ændringen af listen (%d).", resp.Status)
}

// createToday makes today's list, never as primary, and reads it back by name.
func (s *Store) createToday(ctx context.Context) (*apiList, error) {
	body := newList{Name: s.listName(), Primary: false, Items: []any{}}
	resp, err := s.api(ctx, call{method: "POST", path: "v2/shoppinglists", body: body, auth: true})
	if err != nil {
		return nil, err
	}
	if !isOK(resp) {
		return nil, store.Errorf("REMA 1000 afviste at oprette listen %q (%d).", s.listName(), resp.Status)
	}
	list, err := s.findToday(ctx)
	if err != nil {
		return nil, err
	}
	if list == nil {
		return nil, store.Errorf("REMA 1000: listen %q blev ikke fundet efter oprettelsen. API'et kan have ændret sig.", s.listName())
	}
	return list, nil
}
