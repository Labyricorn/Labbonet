// Package generate drives one generation end to end: persisting
// messages, acquiring the endpoint's concurrency slot, calling the
// provider adapter, and recording provenance (ARCHITECTURE.md section
// 4.2).
package generate

import (
	"context"
	"sync"
)

// Slots gates concurrent generations per endpoint to its configured
// concurrency_limit (Strata defaults to 1). A second request to an
// endpoint that is already at its limit waits for a free slot rather
// than failing (A-C9).
//
// Each endpoint keeps one gate for its lifetime: the count of active holders survives a
// limit change, so raising or lowering the limit never creates a second pool. The most
// recent Acquire's limit is the one in force; lowering it lets existing holders finish
// and admits no new holder until the active count falls below the new limit.
type Slots struct {
	mu    sync.Mutex
	gates map[int64]*gate
}

type gate struct {
	active  int
	limit   int
	waiters []*waiter // FIFO
}

type waiter struct {
	ready   chan struct{}
	granted bool
}

// NewSlots builds an empty Slots tracker.
func NewSlots() *Slots {
	return &Slots{gates: make(map[int64]*gate)}
}

// Acquire blocks until a slot for endpointID is free or ctx is done.
// limit is the endpoint's current concurrency_limit (zero or negative
// is treated as 1). The returned release function must be called
// exactly once to free the slot.
func (s *Slots) Acquire(ctx context.Context, endpointID int64, limit int) (release func(), err error) {
	if limit <= 0 {
		limit = 1
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	g, ok := s.gates[endpointID]
	if !ok {
		g = &gate{}
		s.gates[endpointID] = g
	}
	g.limit = limit
	if g.active < g.limit && len(g.waiters) == 0 {
		g.active++
		s.mu.Unlock()
		return s.releaser(g), nil
	}
	w := &waiter{ready: make(chan struct{})}
	g.waiters = append(g.waiters, w)
	// A raised limit may admit earlier waiters now.
	s.admit(g)
	s.mu.Unlock()

	select {
	case <-w.ready:
		return s.releaser(g), nil
	case <-ctx.Done():
		s.mu.Lock()
		defer s.mu.Unlock()
		if w.granted {
			// Granted at the same moment as cancellation: hand the slot back.
			g.active--
			s.admit(g)
			return nil, ctx.Err()
		}
		for i, x := range g.waiters {
			if x == w {
				g.waiters = append(g.waiters[:i], g.waiters[i+1:]...)
				break
			}
		}
		return nil, ctx.Err()
	}
}

// admit grants waiters in order while capacity remains. The caller holds s.mu.
func (s *Slots) admit(g *gate) {
	for len(g.waiters) > 0 && g.active < g.limit {
		w := g.waiters[0]
		g.waiters = g.waiters[1:]
		w.granted = true
		g.active++
		close(w.ready)
	}
}

func (s *Slots) releaser(g *gate) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			g.active--
			s.admit(g)
			s.mu.Unlock()
		})
	}
}
