package secrets

import (
	"runtime"
	"testing"
)

func TestProtector_RoundTrip(t *testing.T) {
	p := NewProtector()

	blob, err := p.Protect("sk-test-credential-value")
	if runtime.GOOS != "windows" {
		if err != ErrUnsupported {
			t.Fatalf("Protect on %s: err = %v, want ErrUnsupported", runtime.GOOS, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("Protect: %v", err)
	}
	if len(blob) == 0 {
		t.Fatal("Protect returned an empty blob")
	}

	got, err := p.Unprotect(blob)
	if err != nil {
		t.Fatalf("Unprotect: %v", err)
	}
	if got != "sk-test-credential-value" {
		t.Fatalf("Unprotect = %q, want original plaintext", got)
	}
}

func TestProtector_DifferentPlaintextsDifferentBlobs(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI is Windows-only")
	}
	p := NewProtector()

	a, err := p.Protect("credential-a")
	if err != nil {
		t.Fatalf("protect a: %v", err)
	}
	b, err := p.Protect("credential-b")
	if err != nil {
		t.Fatalf("protect b: %v", err)
	}
	if string(a) == string(b) {
		t.Fatal("different plaintexts protected to the same blob")
	}
}

func TestProtector_RejectsEmpty(t *testing.T) {
	p := NewProtector()
	if _, err := p.Protect(""); err == nil {
		t.Fatal("Protect(\"\") did not error")
	}
	if _, err := p.Unprotect(nil); err == nil {
		t.Fatal("Unprotect(nil) did not error")
	}
}

func TestProtector_UnprotectGarbageFails(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI is Windows-only")
	}
	p := NewProtector()
	if _, err := p.Unprotect([]byte("not a real DPAPI blob")); err == nil {
		t.Fatal("Unprotect of garbage bytes did not error")
	}
}
