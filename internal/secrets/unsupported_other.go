//go:build !windows

package secrets

// unsupportedProtector refuses every operation. Phase 1 ships DPAPI
// only (ARCHITECTURE.md section 1); there is deliberately no plaintext
// fallback on other platforms.
type unsupportedProtector struct{}

// NewProtector returns the platform Protector. On non-Windows platforms
// Phase 1 has none, so every call fails with ErrUnsupported.
func NewProtector() Protector {
	return unsupportedProtector{}
}

func (unsupportedProtector) Protect(string) ([]byte, error) {
	return nil, ErrUnsupported
}

func (unsupportedProtector) Unprotect([]byte) (string, error) {
	return "", ErrUnsupported
}
