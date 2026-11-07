package compare

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// Target is a store and the name it was asked for.
type Target struct {
	Name  string
	Store store.Store
}

// Result is what one store answered. Err is set when the work failed or panicked.
type Result[T any] struct {
	Name  string
	Value T
	Err   error
}

// PanicError is a panic in one store's goroutine. The server shows "uventet fejl" for it, like
// any error that is not a StoreError.
type PanicError struct{ Value any }

func (e *PanicError) Error() string { return fmt.Sprintf("panic: %v", e.Value) }

// Gather runs `work` for every store at once. One store failing, or panicking, never stops
// the others (Python: gather with return_exceptions=True). The results keep the order of
// `targets`; each goroutine writes only its own slot.
func Gather[T any](ctx context.Context, targets []Target, work func(context.Context, store.Store) (T, error)) []Result[T] {
	results := make([]Result[T], len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		results[i].Name = t.Name
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					// The type only: a panic value's text can hold a URL with ids or tokens.
					slog.Error("store panicked", "store", t.Name, "panic", fmt.Sprintf("%T", r), "stack", string(debug.Stack()))
					results[i] = Result[T]{Name: t.Name, Err: &PanicError{Value: r}}
				}
			}()
			v, err := work(ctx, t.Store)
			results[i].Value, results[i].Err = v, err
		}()
	}
	wg.Wait()
	return results
}
