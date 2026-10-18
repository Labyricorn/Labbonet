package egress

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// The web-search service is one fixed destination. Unlike provider endpoints it is not
// registered by the user, so its host, scheme and path are constants here and no setting,
// request body or model output can change them (SECURITY-MODEL.md section 16).
const (
	SearchHost         = "ollama.com"
	SearchPath         = "/api/web_search"
	searchMaxBodyBytes = 1 << 20
	searchTimeout      = 20 * time.Second
)

// SearchClient posts one search request to the fixed service. It shares the provider
// client's dial guard (the resolved address must be public, never metadata) and refuses
// every redirect.
type SearchClient struct {
	base  *url.URL
	class string
}

// NewSearchClient is the production client: https://ollama.com, public addresses only.
func NewSearchClient() *SearchClient {
	return &SearchClient{base: &url.URL{Scheme: "https", Host: SearchHost}, class: ClassExternal}
}

// NewSearchClientFor points the client at another base URL and privacy class. It exists
// for tests that need a loopback fake; no production code path calls it.
func NewSearchClientFor(base *url.URL, class string) *SearchClient {
	return &SearchClient{base: base, class: class}
}

// Post sends body (JSON) with the bearer key and returns the status and at most 1 MiB of
// the response. The key is only ever placed in the Authorization header of this one call.
func (c *SearchClient) Post(ctx context.Context, key string, body io.Reader) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	u := *c.base
	u.Path, u.RawQuery, u.Fragment = SearchPath, "", ""
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), body)
	if err != nil {
		return 0, nil, fmt.Errorf("egress: build search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext:           dialGuard(c.class).DialContext,
			ResponseHeaderTimeout: searchTimeout,
		},
		CheckRedirect: refuseRedirects,
	}
	resp, err := client.Do(req)
	if err != nil {
		// The URL error text names the host only; it never contains the key.
		return 0, nil, fmt.Errorf("egress: search request to %s: %w", u.Host, ctxOrErr(ctx, err))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, searchMaxBodyBytes+1))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("egress: read search response: %w", err)
	}
	if len(data) > searchMaxBodyBytes {
		return resp.StatusCode, nil, fmt.Errorf("egress: search response exceeds %d bytes", searchMaxBodyBytes)
	}
	return resp.StatusCode, data, nil
}

func ctxOrErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
