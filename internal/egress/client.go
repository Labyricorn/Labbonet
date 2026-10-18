package egress

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

// Default timeouts and limits (PROVIDER-SPEC.md section 1.4), used
// unless an endpoint overrides them later.
const (
	DefaultConnectTimeout   = 5 * time.Second
	DefaultFirstByteTimeout = 180 * time.Second
	DefaultMaxResponseBytes = 8 << 20 // 8 MiB, for non-streaming calls such as model lists
)

// Target names the endpoint a request goes to: its base URL, the
// privacy class it was registered with (checked against the address
// actually dialed, every time), and the credential to attach, if any.
// Credential is attached only to requests built from this Target; a
// credential for one endpoint must never be passed in another
// endpoint's Target (SECURITY-MODEL.md S11).
type Target struct {
	BaseURL      *url.URL
	PrivacyClass string
	Credential   string // bearer token; empty means no Authorization header
}

// Client is the only outbound HTTP client in Labbonet. Every request
// goes through CheckPath (the fixed path allowlist), a dial guard that
// refuses to connect if the resolved address does not match the
// target's declared privacy class or is a cloud metadata address, and a
// CheckRedirect that refuses every redirect.
type Client struct {
	// MaxResponseBytes bounds DoLimited reads. It does not apply to Do,
	// whose caller (a streaming adapter) reads the body itself.
	MaxResponseBytes int64
}

// NewClient builds a Client with the Phase 1 default response size
// limit.
func NewClient() *Client {
	return &Client{MaxResponseBytes: DefaultMaxResponseBytes}
}

// Do issues method to target.BaseURL+path. It refuses any path outside
// the allowlist, any unsupported scheme, any redirect, and any
// connection whose resolved address does not match target.PrivacyClass.
// The caller must close the response body.
func (c *Client) Do(ctx context.Context, target Target, method, path string, body io.Reader) (*http.Response, error) {
	req, err := c.newRequest(ctx, target, method, path, body)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext:           dialGuard(target.PrivacyClass).DialContext,
			ResponseHeaderTimeout: DefaultFirstByteTimeout,
		},
		CheckRedirect: refuseRedirects,
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("egress: request to %s: %w", req.URL.Host, err)
	}
	return resp, nil
}

// DoLimited is Do followed by reading at most MaxResponseBytes of the
// response body, for non-streaming calls (model lists, health checks).
// It always closes the response body.
func (c *Client) DoLimited(ctx context.Context, target Target, method, path string, body io.Reader) (*http.Response, []byte, error) {
	limit := c.MaxResponseBytes
	if limit <= 0 {
		limit = DefaultMaxResponseBytes
	}
	resp, err := c.Do(ctx, target, method, path, body)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return resp, nil, fmt.Errorf("egress: read response: %w", err)
	}
	if int64(len(data)) > limit {
		return resp, nil, fmt.Errorf("egress: response exceeds %d byte limit", limit)
	}
	return resp, data, nil
}

func (c *Client) newRequest(ctx context.Context, target Target, method, path string, body io.Reader) (*http.Request, error) {
	if err := CheckPath(path); err != nil {
		return nil, err
	}
	if target.BaseURL == nil {
		return nil, fmt.Errorf("egress: target has no base URL")
	}
	switch target.BaseURL.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("egress: unsupported scheme %q", target.BaseURL.Scheme)
	}

	reqURL := *target.BaseURL
	reqURL.Path = trimTrailingSlash(reqURL.Path) + path
	reqURL.RawQuery = ""
	reqURL.Fragment = ""

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("egress: build request: %w", err)
	}
	if target.Credential != "" {
		req.Header.Set("Authorization", "Bearer "+target.Credential)
	}
	return req, nil
}

func refuseRedirects(req *http.Request, via []*http.Request) error {
	return fmt.Errorf("egress: redirects are not allowed (to %s)", req.URL)
}

// dialGuard returns a *net.Dialer whose Control hook refuses to connect
// if the resolved address does not match the declared privacy class, or
// is a cloud metadata address (SECURITY-MODEL.md section 5). Control
// runs after the socket is created but before connect(), so a refusal
// here means no outbound packet is ever sent, and it re-runs on every
// connection, so a later DNS change is re-checked rather than trusted
// from an earlier lookup.
func dialGuard(class string) *net.Dialer {
	return &net.Dialer{
		Timeout: DefaultConnectTimeout,
		Control: func(network, address string, _ syscall.RawConn) error {
			return checkDialAddress(address, class)
		},
	}
}

// checkDialAddress is the dial guard's decision, split out so it can be
// unit tested without opening any socket (SECURITY-MODEL.md S9, S10).
func checkDialAddress(address, declaredClass string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("egress: dial guard: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("egress: dial guard: address %q did not resolve to an IP literal", address)
	}
	if IsMetadata(ip) {
		return fmt.Errorf("egress: dial guard: refused cloud metadata address %s", ip)
	}
	if actual := Classify(ip); actual != declaredClass {
		return fmt.Errorf("egress: dial guard: %s resolved to class %q, endpoint is registered as %q", ip, actual, declaredClass)
	}
	return nil
}

func trimTrailingSlash(p string) string {
	for len(p) > 0 && p[len(p)-1] == '/' {
		p = p[:len(p)-1]
	}
	return p
}
