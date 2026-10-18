// Package endpoints validates and classifies provider endpoint
// registrations before they reach internal/store (ARCHITECTURE.md
// section 4, SECURITY-MODEL.md section 5).
package endpoints

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/Labyricorn/Labbonet/internal/egress"
)

// ValidateBaseURL parses and validates a user-supplied base URL: http
// or https scheme, no embedded credentials, and no path, query or
// fragment, so the fixed request paths in PROVIDER-SPEC.md are the only
// thing ever appended to it. The returned URL has its path, query and
// fragment cleared.
func ValidateBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("base URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("base URL scheme must be http or https")
	}
	if u.User != nil {
		return nil, errors.New("base URL must not contain a username or password")
	}
	if u.Hostname() == "" {
		return nil, errors.New("base URL must include a host")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, errors.New("base URL must not include a path")
	}
	if u.RawQuery != "" {
		return nil, errors.New("base URL must not include a query string")
	}
	if u.Fragment != "" {
		return nil, errors.New("base URL must not include a fragment")
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u, nil
}

// ErrExternalNeedsAcknowledgement is returned by ClassifyAddress when an
// endpoint resolves to a public address and the caller has not set
// acknowledgeExternal (SECURITY-MODEL.md section 5).
var ErrExternalNeedsAcknowledgement = errors.New("this endpoint resolves to a public address and must be acknowledged as external before it can be registered")

// ErrMetadataAddress is returned when an endpoint resolves to a cloud
// metadata address, which is always refused (SECURITY-MODEL.md S10).
var ErrMetadataAddress = errors.New("this endpoint resolves to a cloud metadata address and cannot be registered")

// ClassifyAddress resolves host (via DNS if it is not already an IP
// literal) and returns the privacy class the address falls into. It
// refuses metadata addresses outright and requires acknowledgeExternal
// for any address that would classify as external. This is a
// suggestion made once at registration time; internal/egress's dial
// guard re-checks the resolved class on every real connection.
func ClassifyAddress(ctx context.Context, host string, acknowledgeExternal bool) (string, error) {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", host, err)
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("resolve %q: no addresses found", host)
	}

	class := egress.Classify(ips[0].IP)
	for _, addr := range ips {
		if egress.IsMetadata(addr.IP) {
			return "", ErrMetadataAddress
		}
		if c := egress.Classify(addr.IP); c != class {
			// A hostname resolving to addresses of more than one class is
			// treated at the least trusted of the classes seen: the dial
			// guard re-checks the actual address on every real
			// connection anyway, so this only affects the registration
			// suggestion and the external-acknowledgement requirement.
			class = widerClass(class, c)
		}
	}

	if class == egress.ClassExternal && !acknowledgeExternal {
		return "", ErrExternalNeedsAcknowledgement
	}
	return class, nil
}

var classRank = map[string]int{
	egress.ClassLocal:    0,
	egress.ClassLAN:      1,
	egress.ClassExternal: 2,
}

func widerClass(a, b string) string {
	if classRank[b] > classRank[a] {
		return b
	}
	return a
}
