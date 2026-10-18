package generate

import "testing"

func TestRegistry_CancelAndWasCancelled(t *testing.T) {
	r := NewRegistry()
	called := false
	r.Register(1, func() { called = true })

	if r.WasCancelled(1) {
		t.Fatal("WasCancelled true before Cancel")
	}
	if !r.Cancel(1) {
		t.Fatal("Cancel(1) = false, want true (registered)")
	}
	if !called {
		t.Fatal("Cancel did not invoke the cancel function")
	}
	if !r.WasCancelled(1) {
		t.Fatal("WasCancelled false after Cancel")
	}
	if r.Cancel(2) {
		t.Fatal("Cancel(2) = true, want false (never registered)")
	}

	r.Unregister(1)
	if r.WasCancelled(1) {
		t.Fatal("WasCancelled true after Unregister")
	}
}

func TestRegistry_CancelAll(t *testing.T) {
	r := NewRegistry()
	var calledA, calledB bool
	r.Register(1, func() { calledA = true })
	r.Register(2, func() { calledB = true })

	r.CancelAll()

	if !calledA || !calledB {
		t.Fatalf("CancelAll did not cancel both: a=%v b=%v", calledA, calledB)
	}
	if !r.WasCancelled(1) || !r.WasCancelled(2) {
		t.Fatal("CancelAll did not mark both as cancelled")
	}
}
