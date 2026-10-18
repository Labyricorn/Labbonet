package generate

import (
	"context"
	"errors"

	"github.com/Labyricorn/Labbonet/internal/prompt"
	"github.com/Labyricorn/Labbonet/internal/search"
)

// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Behavior reference: GobboNet 10-chat.js / 11-search.js @ 314bdc9cae204ae38fdc1002ac6a9fb1437c109f
// (Copyright (C) 2026 Elodine / GoblinCorps): search runs before the prompt is built, a failed
// search does not stop the reply, and the user is told what happened. No upstream source copied;
// see docs/REUSE-LOG.md.

// ErrSearch reports that a message asked for web search but search is not usable (off, no
// key, or not wired). Nothing was sent anywhere and no turn was stored. The error text is
// the reason, written for the user; errors.Is(err, ErrSearch) identifies the class.
var ErrSearch = errors.New("web search unavailable")

type searchRefusal struct{ reason string }

func (e searchRefusal) Error() string        { return e.reason }
func (e searchRefusal) Is(target error) bool { return target == ErrSearch }

func refuseSearch(reason string) error { return searchRefusal{reason} }

// SearchRecord is the stored account of one search: what was asked, which hits came back
// (titles, links and short snippets), and how many reached the prompt. Error is set when
// the search failed; the reply then continued without results.
type SearchRecord struct {
	Query   string          `json:"query"`
	Max     int             `json:"max_results"`
	Results []search.Result `json:"results"`
	Used    int             `json:"used"`
	Omitted bool            `json:"omitted,omitempty"`
	Error   string          `json:"error,omitempty"`
}

const recordSnippetRunes = 400

// searchPlan is a verified-usable search configuration for one send.
type searchPlan struct {
	key string
	max int
}

// planSearch checks that search is enabled and a key is stored, before anything else
// happens, and unprotects the key for this one send.
func (rn *Runner) planSearch(ctx context.Context) (*searchPlan, error) {
	if rn.Search == nil {
		return nil, refuseSearch("web search is not available in this build")
	}
	settings, err := rn.Store.GetSearchSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, refuseSearch(search.ErrDisabled.Error())
	}
	blob, err := rn.Store.SearchCredentialBlob(ctx)
	if err != nil {
		return nil, err
	}
	if len(blob) == 0 {
		return nil, refuseSearch(search.ErrNoKey.Error())
	}
	key, err := rn.Protector.Unprotect(blob)
	if err != nil {
		return nil, refuseSearch("the web search key could not be unprotected; store it again in MODS → SEARCH")
	}
	return &searchPlan{key: key, max: settings.MaxResults}, nil
}

// runSearch performs the request. A failure other than cancellation is recorded and the
// reply goes ahead without results. ctx ending (the request was abandoned) aborts the send.
func (rn *Runner) runSearch(ctx context.Context, plan *searchPlan, message string) (*SearchRecord, []prompt.WebResult, error) {
	rec := &SearchRecord{Query: search.Query(message), Max: plan.max, Results: []search.Result{}}
	if rec.Query == "" {
		rec.Error = "there was no text to search for"
		return rec, nil, nil
	}
	hits, err := search.Run(ctx, rn.Search, plan.key, rec.Query, plan.max)
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	if err != nil {
		rec.Error = err.Error()
		return rec, nil, nil
	}
	web := make([]prompt.WebResult, 0, len(hits))
	for _, h := range hits {
		web = append(web, prompt.WebResult{Title: h.Title, URL: h.URL, Content: h.Content})
		snippet := []rune(h.Content)
		if len(snippet) > recordSnippetRunes {
			h.Content = string(snippet[:recordSnippetRunes])
		}
		rec.Results = append(rec.Results, h)
	}
	return rec, web, nil
}
