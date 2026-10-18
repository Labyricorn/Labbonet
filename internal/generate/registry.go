package generate

import (
	"context"
	"sync"
)

type registryEntry struct {
	cancel    context.CancelFunc
	cancelled bool
}

// Registry tracks running generations by their store ID, so
// POST /api/generations/{id}/cancel can find and cancel the right one,
// and so Runner can tell an explicit cancel (A-C4, "cancelled") apart
// from the browser simply disconnecting (A-C5, "interrupted") even
// though both abort the same context.
type Registry struct {
	mu   sync.Mutex
	gens map[int64]*registryEntry
}

// NewRegistry builds an empty Registry.
func NewRegistry() *Registry {
	return &Registry{gens: make(map[int64]*registryEntry)}
}

// Register records a running generation's cancel function.
func (r *Registry) Register(id int64, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gens[id] = &registryEntry{cancel: cancel}
}

// Unregister removes a generation once it has finished. Call this
// after reading WasCancelled, not before.
func (r *Registry) Unregister(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.gens, id)
}

// Cancel marks a running generation as explicitly cancelled and aborts
// its context, reporting whether one was found running.
func (r *Registry) Cancel(id int64) bool {
	r.mu.Lock()
	e, ok := r.gens[id]
	if ok {
		e.cancelled = true
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	e.cancel()
	return true
}

// CancelAll marks every currently running generation as explicitly
// cancelled and aborts each one's context (ARCHITECTURE.md section 6:
// on Ctrl+C, "cancel running generations (partial text saved)";
// PHASE1-ACCEPTANCE.md A-B4 expects status "cancelled", not
// "interrupted", for this case).
func (r *Registry) CancelAll() {
	r.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(r.gens))
	for _, e := range r.gens {
		e.cancelled = true
		cancels = append(cancels, e.cancel)
	}
	r.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// WasCancelled reports whether Cancel was called for a still-registered
// generation.
func (r *Registry) WasCancelled(id int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.gens[id]
	return ok && e.cancelled
}
