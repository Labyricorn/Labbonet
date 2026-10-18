// Package egress is the only package in Labbonet that makes outbound
// HTTP requests to provider endpoints (SECURITY-MODEL.md section 5,
// ARCHITECTURE.md section 4). It enforces the fixed request path
// allowlist, a dial guard matching the resolved address to the
// endpoint's declared privacy class, no redirects, and response size
// limits.
package egress

import "net"

// Privacy classes, matching the endpoints.privacy_class column
// (ARCHITECTURE.md section 5).
const (
	ClassLocal    = "local"
	ClassLAN      = "lan"
	ClassExternal = "external"
)

// metadataAddrs are cloud metadata addresses that are always refused,
// regardless of declared privacy class (SECURITY-MODEL.md section 5).
var metadataAddrs = []net.IP{
	net.ParseIP("169.254.169.254"),
	net.ParseIP("fd00:ec2::254"),
}

// IsMetadata reports whether ip is a cloud metadata address.
func IsMetadata(ip net.IP) bool {
	for _, a := range metadataAddrs {
		if ip.Equal(a) {
			return true
		}
	}
	return false
}

// Classify returns the privacy class an address falls into: local
// (loopback), lan (RFC 1918, link-local or unique local) or external
// (public). It does not special-case metadata addresses; callers must
// check IsMetadata first, since a metadata address falls in the lan
// range but must never be treated as lan-trusted.
func Classify(ip net.IP) string {
	if ip.IsLoopback() {
		return ClassLocal
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return ClassLAN
	}
	return ClassExternal
}
