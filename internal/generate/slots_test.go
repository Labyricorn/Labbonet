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

// 1.4: changing the limit while a slot is held must not create a second pool.
func TestSlots_LimitChangeKeepsActiveCount(t *testing.T) {
	s := NewSlots()
	ctx := context.Background()
	release1, err := s.Acquire(ctx, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Raised to 2: one more holder fits, a third waits.
	release2, err := s.Acquire(ctx, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	third := make(chan func(), 1)
	go func() {
		r, err := s.Acquire(ctx, 1, 2)
		if err != nil {
			t.Error(err)
			return
		}
		third <- r
	}()
	select {
	case <-third:
		t.Fatal("third holder admitted beyond the limit of 2")
	case <-time.After(100 * time.Millisecond):
	}
	// Lowered to 1 while two hold: releasing one must not admit the waiter.
	lowered := make(chan func(), 1)
	go func() {
		r, err := s.Acquire(ctx, 1, 1)
		if err != nil {
			t.Error(err)
			return
		}
		lowered <- r
	}()
	time.Sleep(50 * time.Millisecond)
	release1()
	select {
	case <-third:
		t.Fatal("waiter admitted while one holder still fills the lowered limit")
	case <-time.After(100 * time.Millisecond):
	}
	release2()
	select {
	case r := <-third:
		r()
	case <-time.After(time.Second):
		t.Fatal("waiter not admitted after holders drained")
	}
	select {
	case r := <-lowered:
		r()
	case <-time.After(time.Second):
		t.Fatal("second waiter not admitted")
	}
}

func TestSlots_CancelledWaiterFreesQueueAndDoubleReleaseIsSafe(t *testing.T) {
	s := NewSlots()
	release, err := s.Acquire(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := s.Acquire(cctx, 1, 1); errc <- err }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-errc; err == nil {
		t.Fatal("cancelled waiter did not error")
	}
	release()
	release() // must not free a slot twice
	r1, err := s.Acquire(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	short, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	if _, err := s.Acquire(short, 1, 1); err == nil {
		t.Fatal("double release granted extra capacity")
	}
	r1()
}
