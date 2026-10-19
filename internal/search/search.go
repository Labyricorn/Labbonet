// Package search holds Labbonet's opt-in web search: settings, the request to the single
// fixed search service, and sanitising of what it returns. It is pure apart from the one
// egress.SearchClient call; storage and credential handling live with the caller.
// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Behavior/design reference: GobboNet js/11-search.js and internal/proxy @
// 314bdc9cae204ae38fdc1002ac6a9fb1437c109f (Copyright (C) 2026 Elodine / GoblinCorps):
// a keyed POST of {query,max_results} answered with {results:[{title,url,content}]}.
// No upstream source is copied. Exact attribution boundaries: docs/REUSE-LOG.md.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Labyricorn/Labbonet/internal/egress"
)

// Limits. Search text is untrusted: it is bounded before it is stored, shown or sent to a
// model.
const (
	MaxQueryRunes       = 400
	MaxResults          = 10
	DefaultResults      = 5
	MaxTitleRunes       = 200
	MaxURLBytes         = 500
	MaxContentRunes     = 1200
	ConnectionTestQuery = "labbonet connection test"
)

// Settings are the search choices. Search is off until the owner enables it AND stores a
// key; each message must then also ask for it with the composer toggle.
type Settings struct {
	Revision   int64 `json:"revision"`
	Enabled    bool  `json:"enabled"`
	MaxResults int   `json:"max_results"`
}

// Defaults: off, five results.
func Defaults() Settings { return Settings{MaxResults: DefaultResults} }

var errSetting = errors.New("invalid search setting")

// IsSettingError reports whether err came from Validate.
func IsSettingError(err error) bool { return errors.Is(err, errSetting) }

// Validate checks ranges.
func (s Settings) Validate() error {
	if s.MaxResults < 1 || s.MaxResults > MaxResults {
		return fmt.Errorf("%w: results must be 1 through %d", errSetting, MaxResults)
	}
	return nil
}

// Configuration errors: nothing was sent anywhere.
var (
	ErrDisabled = errors.New("web search is turned off; enable it in MODS → SEARCH")
	ErrNoKey    = errors.New("web search has no API key; store one in MODS → SEARCH")
)

// Result is one sanitised hit.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

var attachHeader = regexp.MustCompile(`(?m)^\[Attached file:`)

var macroTag = regexp.MustCompile(`\{\{[A-Za-z][A-Za-z0-9_]*\}\}`)

// Query turns a user message into the single-line query sent to the service: the typed
// text before any attachment block, with {{macro}} tags removed (they are neither expanded
// nor sent), whitespace collapsed, bounded. Empty means there is nothing to search for.
func Query(message string) string {
	// Attachments are appended as "[Attached file: name]" blocks. Cut at the first such header
	// at the start of any line (start of message, after LF or CRLF), so a regenerated, edited or
	// imported message never sends file content, whatever blank lines surround the block.
	if loc := attachHeader.FindStringIndex(message); loc != nil {
		message = message[:loc[0]]
	}
	return clean(macroTag.ReplaceAllString(message, " "), MaxQueryRunes)
}

// clean collapses whitespace and drops control characters, then truncates to n runes.
func clean(s string, n int) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case r == utf8.RuneError, r == ' ', r == ' ':
			r = ' '
			fallthrough
		case unicode.IsSpace(r):
			space = true
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
		default:
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		}
	}
	out := b.String()
	if utf8.RuneCountInString(out) > n {
		out = string([]rune(out)[:n])
	}
	return out
}

// Run posts query to the fixed service and returns the sanitised results.
func Run(ctx context.Context, client *egress.SearchClient, key, query string, max int) ([]Result, error) {
	if key == "" {
		return nil, ErrNoKey
	}
	if query == "" {
		return nil, errors.New("there is nothing to search for")
	}
	if max < 1 || max > MaxResults {
		max = DefaultResults
	}
	body, err := json.Marshal(struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}{query, max})
	if err != nil {
		return nil, err
	}
	status, data, err := client.Post(ctx, key, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	switch {
	case status == 401 || status == 403:
		return nil, fmt.Errorf("the search service rejected the API key (HTTP %d)", status)
	case status == 429:
		return nil, errors.New("the search service is rate limiting this key (HTTP 429)")
	case status < 200 || status > 299:
		return nil, fmt.Errorf("the search service answered HTTP %d", status)
	}
	return Parse(data, max)
}

// Parse reads a service response. Hits without a plain http(s) URL are dropped; text is
// cleaned and bounded; at most max hits are kept.
func Parse(data []byte, max int) ([]Result, error) {
	var raw struct {
		Error   any `json:"error"`
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errors.New("the search service sent a response that is not valid JSON")
	}
	if raw.Error != nil {
		return nil, errors.New("the search service reported an error")
	}
	out := []Result{}
	for _, r := range raw.Results {
		if len(out) >= max {
			break
		}
		link := strings.TrimSpace(r.URL)
		if len(link) > MaxURLBytes || !webURL(link) {
			continue
		}
		title := clean(r.Title, MaxTitleRunes)
		if title == "" {
			title = link
		}
		out = append(out, Result{Title: title, URL: link, Content: clean(r.Content, MaxContentRunes)})
	}
	return out, nil
}

// webURL accepts only absolute http(s) URLs with a host and no credentials.
func webURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}
