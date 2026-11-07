// Package store is what every store has to do. A new store is one package that implements
// Store (phase 5 and 6 of the port; nothing here logs in).
//
// Store methods may only read, except SetQuantities. Ordering, paying, choosing a delivery time
// and changing the address must never exist here (ADR 0001).
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/McAndersDK/handle-mcp-go/internal/model"
)

// StoreError is a clear, user-facing failure from one store (login failed, API changed, ...).
// The text is Danish and may be shown to the human.
type StoreError struct{ Msg string }

func (e *StoreError) Error() string { return e.Msg }

// Errorf makes a StoreError with a formatted Danish message.
func Errorf(format string, args ...any) error { return &StoreError{Msg: fmt.Sprintf(format, args...)} }

// ErrNotImplemented is returned when a store cannot do something (Python: NotImplementedError).
// The tool says so politely.
var ErrNotImplemented = errors.New("not implemented")

// CartItem is an absolute quantity for one product. A slice keeps the order of the request,
// as Python's dict did. 0 removes the line.
type CartItem struct {
	ProductID string
	Quantity  int
}

// Store has the methods every store needs. Each takes a context first.
type Store interface {
	Name() string

	Search(ctx context.Context, query string, limit int) ([]model.Product, error)
	Recipes(ctx context.Context, query string, limit int) ([]model.Recipe, error)
	Recipe(ctx context.Context, recipeID string, persons *int) (model.RecipeDetails, error)
	FeaturedRecipes(ctx context.Context, limit int) ([]model.Recipe, error)
	RecipesByTag(ctx context.Context, tags []string, category *string, limit int) ([]model.Recipe, error)
	Offers(ctx context.Context, limit int) ([]model.Product, error)
	GetCart(ctx context.Context) (model.Cart, error)
	SetQuantities(ctx context.Context, items []CartItem) ([]model.QuantityResult, error)
	DeliveryInfo(ctx context.Context) (model.DeliveryInfo, error)
	DeliverySlots(ctx context.Context, days int) ([]model.DeliverySlot, error)
	Terms(ctx context.Context) (model.StoreTerms, error)
	Orders(ctx context.Context, limit int) ([]model.Order, error)
	Order(ctx context.Context, orderID string) (model.Order, error)

	// Session is what to save after a login: never a password. nil when there is none.
	Session() map[string]string
}
