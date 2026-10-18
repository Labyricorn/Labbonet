package generate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/providers"
	"github.com/Labyricorn/Labbonet/internal/store"
)

// captureServer records every chat request body the provider receives.
func captureServer(t *testing.T) (*httptest.Server, func() []providers.ChatRequest) {
	t.Helper()
	var mu sync.Mutex
	var got []providers.ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[{"id":"m"}]}`))
			return
		}
		var req providers.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		mu.Lock()
		got = append(got, req)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(runnerTestSSE))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []providers.ChatRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]providers.ChatRequest(nil), got...)
	}
}

func sent(req providers.ChatRequest) string {
	var parts []string
	for _, m := range req.Messages {
		parts = append(parts, m.Role+":"+m.Content)
	}
	return strings.Join(parts, "|")
}

func visible(t *testing.T, st *store.Store, convID int64) string {
	t.Helper()
	p, _, err := st.ActivePath(context.Background(), convID)
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, m := range p {
		parts = append(parts, m.Content)
	}
	return strings.Join(parts, "|")
}

func TestRunner_OnlyTheChosenBranchReachesTheProvider(t *testing.T) {
	srv, requests := captureServer(t)
	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, srv.URL, "m")
	ctx := context.Background()
	noop := func(providers.Event) error { return nil }

	if err := rn.Run(ctx, Request{ConversationID: conv.ID, UserContent: "first"}, nil, noop); err != nil {
		t.Fatal(err)
	}
	if err := rn.Run(ctx, Request{ConversationID: conv.ID, UserContent: "second"}, nil, noop); err != nil {
		t.Fatal(err)
	}
	if got := visible(t, st, conv.ID); got != "first|Hello|second|Hello" {
		t.Fatal(got)
	}
	path, rev, _ := st.ActivePath(ctx, conv.ID)

	// Edit the first user message, then ask for a reply to the edit: the old second
	// turn lives in the other variant and must not be sent.
	edited, err := st.EditMessage(ctx, conv.ID, path[0].ID, "first v2", rev)
	if err != nil {
		t.Fatal(err)
	}
	if err = rn.Run(ctx, Request{ConversationID: conv.ID, RegenerateParentID: &edited.ID}, nil, noop); err != nil {
		t.Fatal(err)
	}
	reqs := requests()
	if len(reqs) != 3 {
		t.Fatalf("%d provider requests", len(reqs))
	}
	if got := sent(reqs[2]); got != "user:first v2" {
		t.Fatalf("regeneration sent %q", got)
	}
	if got := visible(t, st, conv.ID); got != "first v2|Hello" {
		t.Fatal(got)
	}

	// Reroll the reply: same prompt, new sibling, no duplicate user turn.
	path, _, _ = st.ActivePath(ctx, conv.ID)
	if err = rn.Run(ctx, Request{ConversationID: conv.ID, RegenerateParentID: &path[0].ID}, nil, noop); err != nil {
		t.Fatal(err)
	}
	reqs = requests()
	if got := sent(reqs[3]); got != "user:first v2" {
		t.Fatalf("reroll sent %q", got)
	}
	path, rev, _ = st.ActivePath(ctx, conv.ID)
	if len(path) != 2 || len(path[1].VariantIDs) != 2 || path[1].Origin != "reroll" {
		t.Fatalf("reroll must add a sibling variant: %d %v %s", len(path), path[1].VariantIDs, path[1].Origin)
	}

	// Switch back to the original first message: the original continuation returns and
	// the next send continues that branch only.
	if err = st.ActivateMessage(ctx, conv.ID, path[0].VariantIDs[0], rev); err != nil {
		t.Fatal(err)
	}
	if err = rn.Run(ctx, Request{ConversationID: conv.ID, UserContent: "third"}, nil, noop); err != nil {
		t.Fatal(err)
	}
	reqs = requests()
	if got := sent(reqs[4]); got != "user:first|assistant:Hello|user:second|assistant:Hello|user:third" {
		t.Fatalf("branch continuation sent %q", got)
	}
	// Every generation row keeps its own provenance, including the rerolled one.
	all, _ := st.ListMessages(ctx, conv.ID)
	assistants := 0
	for _, m := range all {
		if m.Role != "assistant" {
			continue
		}
		assistants++
		gens, err := st.ListGenerationsForMessage(ctx, m.ID)
		if err != nil || len(gens) != 1 || gens[0].ModelRequested != "m" {
			t.Fatalf("message %d provenance: %v %v", m.ID, gens, err)
		}
	}
	if assistants != 5 {
		t.Fatalf("expected 5 assistant variants, got %d", assistants)
	}
}

func TestRunner_RejectsStaleRevisionAndBadRegeneration(t *testing.T) {
	srv, requests := captureServer(t)
	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, srv.URL, "m")
	ctx := context.Background()
	noop := func(providers.Event) error { return nil }
	if err := rn.Run(ctx, Request{ConversationID: conv.ID, UserContent: "hi"}, nil, noop); err != nil {
		t.Fatal(err)
	}
	path, rev, _ := st.ActivePath(ctx, conv.ID)
	before, _ := st.ListMessages(ctx, conv.ID)

	// Another tab changed the tree after this client loaded it.
	if _, err := st.EditMessage(ctx, conv.ID, path[0].ID, "changed elsewhere", rev); err != nil {
		t.Fatal(err)
	}
	err := rn.Run(ctx, Request{ConversationID: conv.ID, UserContent: "stale send", ExpectedRevision: rev}, nil, noop)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale send: %v", err)
	}
	err = rn.Run(ctx, Request{ConversationID: conv.ID, RegenerateParentID: &path[0].ID, ExpectedRevision: rev}, nil, noop)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale regenerate: %v", err)
	}
	// An assistant message, a missing message or another conversation's message cannot be answered.
	for name, id := range map[string]int64{"assistant": path[1].ID, "missing": 9999} {
		id := id
		if err = rn.Run(ctx, Request{ConversationID: conv.ID, RegenerateParentID: &id}, nil, noop); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	after, _ := st.ListMessages(ctx, conv.ID)
	if len(after) != len(before)+1 { // only the edit
		t.Fatalf("rejected requests persisted messages: %d -> %d", len(before), len(after))
	}
	if n := len(requests()); n != 1 {
		t.Fatalf("rejected requests reached the provider: %d", n)
	}
}
