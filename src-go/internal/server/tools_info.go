package server

import (
	"context"
	"slices"
	"sort"
	"sync"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/model"
	"github.com/McAndersDK/handle-mcp-go/src-go/internal/store"
)

func (s *Server) infoTools() map[string]toolFunc {
	return map[string]toolFunc{
		"get_delivery_info": s.deliveryInfo,
		"list_delivery_slots": func(ctx context.Context, a Args) (any, error) {
			days := clamp(a.Int("days"), 1, MaxDays)
			names, err := s.names(a.List("stores"))
			if err != nil {
				return nil, err
			}
			return each(ctx, s, names, func(ctx context.Context, st store.Store) (slotsAnswer, error) {
				return freeSlots(ctx, st, days)
			}), nil
		},
		"get_orders": func(ctx context.Context, a Args) (any, error) {
			limit := clamp(a.Int("limit"), 1, MaxResults)
			orders, err := call(ctx, s, a.Str("store"), func(ctx context.Context, st store.Store) ([]model.Order, error) {
				return st.Orders(ctx, limit)
			})
			return ordersAnswer{Orders: lst(orders)}, err
		},
		"get_order": func(ctx context.Context, a Args) (any, error) {
			return call(ctx, s, a.Str("store"), func(ctx context.Context, st store.Store) (model.Order, error) {
				return st.Order(ctx, a.Str("order_id"))
			})
		},
	}
}

// deliveryInfo reads the chosen time and the terms at once. It only reads.
func (s *Server) deliveryInfo(ctx context.Context, a Args) (any, error) {
	name := a.Str("store")
	var info model.DeliveryInfo
	var terms model.StoreTerms
	var infoErr, termsErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		info, infoErr = call(ctx, s, name, func(ctx context.Context, st store.Store) (model.DeliveryInfo, error) {
			return st.DeliveryInfo(ctx)
		})
	}()
	go func() {
		defer wg.Done()
		terms, termsErr = call(ctx, s, name, func(ctx context.Context, st store.Store) (model.StoreTerms, error) {
			return st.Terms(ctx)
		})
	}()
	wg.Wait()
	if infoErr != nil {
		return nil, infoErr
	}
	return deliveryAnswer{Delivery: info, Terms: terms}, termsErr
}

// freeSlots keeps the slots that can be booked, in time order, and picks the three cheapest.
// A slot with no price comes last. Equal slots keep the store's order.
func freeSlots(ctx context.Context, st store.Store, days int) (slotsAnswer, error) {
	all, err := st.DeliverySlots(ctx, days)
	if err != nil {
		return slotsAnswer{}, err
	}
	var free []model.DeliverySlot
	for _, sl := range all {
		if sl.Available {
			free = append(free, sl)
		}
	}
	earlier := func(a, b model.DeliverySlot) bool { return a.Start.Time().Before(b.Start.Time()) }
	sort.SliceStable(free, func(i, j int) bool { return earlier(free[i], free[j]) })
	cheapest := slices.Clone(free)
	sort.SliceStable(cheapest, func(i, j int) bool {
		a, b := cheapest[i], cheapest[j]
		if (a.Price == nil) != (b.Price == nil) {
			return a.Price != nil
		}
		if a.Price != nil && a.Price.Cmp(*b.Price) != 0 {
			return a.Price.Cmp(*b.Price) < 0
		}
		return earlier(a, b)
	})
	return slotsAnswer{Cheapest: lst(cheapest[:min(3, len(cheapest))]), Slots: lst(free)}, nil
}
