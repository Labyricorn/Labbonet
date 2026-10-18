// Package secrets protects and unprotects endpoint credential blobs
// before they reach internal/store. The store only ever sees the opaque
// protected bytes; plaintext never touches disk (ARCHITECTURE.md
// section 1, SECURITY-MODEL.md section 5).
package secrets

import "errors"

// ErrUnsupported is returned by Protector implementations on platforms
// with no supported credential protection. There is no plaintext
// fallback: an unsupported platform simply cannot store credentials.
var ErrUnsupported = errors.New("credentials unsupported on this platform")

// Protector protects a plaintext credential for storage and reverses
// the operation. NewProtector returns the platform implementation
// (Windows DPAPI, current-user scope, in Phase 1).
type Protector interface {
	Protect(plaintext string) ([]byte, error)
	Unprotect(blob []byte) (string, error)
}
