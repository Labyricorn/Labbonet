package generate

import (
	"context"
	"testing"
	"time"
)

// A-C9: a second request to an endpoint already at its concurrency
// limit waits for a free slot rather than failing.
func TestSlots_SecondAcquireWaitsForRelease(t *testing.T) {
	s := NewSlots()
	ctx := context.Background()

	release1, err := s.Acquire(ctx, 1, 1)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		release2, err := s.Acquire(ctx, 1, 1)
		if err != nil {
			t.Errorf("second acquire: %v", err)
			return
		}
		close(acquired)
		release2()
	}()

	select {
	case <-acquired:
		t.Fatal("second acquire succeeded before the first slot was released")
	case <-time.After(100 * time.Millisecond):
	}

	release1()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("second acquire never completed after release")
	}
}

func TestSlots_AcquireRespectsContextCancellation(t *testing.T) {
	s := NewSlots()
	ctx := context.Background()
	release, err := s.Acquire(ctx, 1, 1)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Acquire(cancelCtx, 1, 1); err == nil {
		t.Fatal("Acquire with an already-cancelled context did not error")
	}
}

func TestSlots_DifferentEndpointsDoNotContend(t *testing.T) {
	s := NewSlots()
	ctx := context.Background()
	release1, err := s.Acquire(ctx, 1, 1)
	if err != nil {
		t.Fatalf("acquire endpoint 1: %v", err)
	}
	defer release1()

	done := make(chan struct{})
	go func() {
		release2, err := s.Acquire(ctx, 2, 1)
		if err != nil {
			t.Errorf("acquire endpoint 2: %v", err)
			return
		}
		release2()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a different endpoint's acquire was blocked by endpoint 1's slot")
	}
}
