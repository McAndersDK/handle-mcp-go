package nemlig

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// settingsVals are the timestamp, timeslot and zone the search gateway wants. They are sent as
// they came from nemlig.
type settingsVals struct{ timestamp, timeslotUtc, zone any }

type settingsCache struct {
	at     time.Time
	vals   settingsVals
	logged bool // the values came from a logged-in basket
}

func (v settingsVals) addTo(p params) params {
	return p.add("timestamp", v.timestamp).add("timeslotUtc", v.timeslotUtc).add("deliveryZoneId", v.zone)
}

// searchSettings are the timestamp, timeslot and zone for search, from /webapi/ only: page URLs
// such as /?GetAsJson=1 can land in the queue-it waiting room. Defaults if a call fails.
func (s *Store) searchSettings(ctx context.Context) (settingsVals, error) {
	logged := s.hasLogin(ctx) // once per search: the keychain is not free
	s.mu.Lock()
	cached := s.settings
	s.mu.Unlock()
	if cached != nil && s.now().Sub(cached.at) < settingsTTL && (cached.logged || !logged) {
		return cached.vals, nil // anonymous values die on login
	}
	var app, basket obj
	err := func() error {
		data, err := s.public(ctx, call{method: "GET", url: base + "/webapi/v2/AppSettings/Website"})
		if err != nil {
			return err
		}
		app = objOrEmpty(data)
		if logged { // without a login, nemlig answers for a default zone and time
			basket, err = s.basket(ctx)
		}
		return err
	}()
	var se *store.StoreError
	var lr *refusedLogin
	switch {
	case errors.As(err, &lr): // one refused login is enough: Search must not try again
		return settingsVals{}, err
	case errors.As(err, &se): // search still works, only less exact
		slog.Warn("nemlig: søger med standardværdier", "error", se.Msg)
	case err != nil:
		return settingsVals{}, err
	}
	vals := settingsVals{
		timestamp:   withDefault(app, "CombinedProductsAndSitecoreTimestamp", ""),
		timeslotUtc: withDefault(basket, "TimeslotUtc", ""),
		zone:        jsonOne,
	}
	if z := basket["DeliveryZoneId"]; truthy(z) {
		vals.zone = z
	}
	if len(basket) > 0 || (len(app) > 0 && !logged) { // a failed call is tried again next time
		slot := ""
		if id := objOrEmpty(basket["DeliveryTimeSlot"])["Id"]; truthy(id) {
			slot = pyStr(id)
		}
		s.mu.Lock()
		s.settings = &settingsCache{at: s.now(), vals: vals, logged: len(basket) > 0}
		s.slotID = slot
		s.mu.Unlock()
	}
	return vals, nil
}

// timeslotID is the delivery time in the basket, so recipe products and offers fit it (sold out
// on that day, prices). Empty if none: nemlig then answers for a default time.
func (s *Store) timeslotID(ctx context.Context) (string, error) {
	if _, err := s.searchSettings(ctx); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.slotID, nil
}
