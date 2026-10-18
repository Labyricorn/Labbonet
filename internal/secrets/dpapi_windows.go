//go:build windows

package secrets

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// dpapiProtector protects credentials with Windows DPAPI. DPAPI has no
// machine-scope flag here, so only the same Windows user account, on
// the same machine, can unprotect a blob it created (ARCHITECTURE.md
// section 1).
type dpapiProtector struct{}

// NewProtector returns the platform Protector: DPAPI on Windows.
func NewProtector() Protector {
	return dpapiProtector{}
}

func (dpapiProtector) Protect(plaintext string) ([]byte, error) {
	if plaintext == "" {
		return nil, fmt.Errorf("secrets: cannot protect an empty credential")
	}
	in := newDataBlob([]byte(plaintext))
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("secrets: protect: %w", err)
	}
	defer freeDataBlob(out)
	return blobBytes(out), nil
}

func (dpapiProtector) Unprotect(blob []byte) (string, error) {
	if len(blob) == 0 {
		return "", fmt.Errorf("secrets: cannot unprotect an empty blob")
	}
	in := newDataBlob(blob)
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", fmt.Errorf("secrets: unprotect: %w (the credential may belong to a different Windows user, or the blob is corrupt)", err)
	}
	defer freeDataBlob(out)
	return string(blobBytes(out)), nil
}

func newDataBlob(data []byte) windows.DataBlob {
	if len(data) == 0 {
		return windows.DataBlob{}
	}
	return windows.DataBlob{
		Size: uint32(len(data)),
		Data: &data[0],
	}
}

// blobBytes copies DPAPI's output buffer into a Go-managed slice before
// it is freed.
func blobBytes(b windows.DataBlob) []byte {
	if b.Size == 0 || b.Data == nil {
		return nil
	}
	out := make([]byte, b.Size)
	copy(out, unsafe.Slice(b.Data, b.Size))
	return out
}

// freeDataBlob releases the buffer DPAPI allocated for an output blob.
func freeDataBlob(b windows.DataBlob) {
	if b.Data == nil {
		return
	}
	_, _ = windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(b.Data))))
}
