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
type Slots struct {
	mu   sync.Mutex
	sems map[int64]chan struct{}
	caps map[int64]int
}

// NewSlots builds an empty Slots tracker.
func NewSlots() *Slots {
	return &Slots{sems: make(map[int64]chan struct{}), caps: make(map[int64]int)}
}

// Acquire blocks until a slot for endpointID is free or ctx is done.
// limit is the endpoint's current concurrency_limit (zero or negative
// is treated as 1). The returned release function must be called
// exactly once to free the slot.
func (s *Slots) Acquire(ctx context.Context, endpointID int64, limit int) (release func(), err error) {
	sem := s.semFor(endpointID, limit)
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Slots) semFor(endpointID int64, limit int) chan struct{} {
	if limit <= 0 {
		limit = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sem, ok := s.sems[endpointID]
	if !ok || s.caps[endpointID] != limit {
		sem = make(chan struct{}, limit)
		s.sems[endpointID] = sem
		s.caps[endpointID] = limit
	}
	return sem
}
