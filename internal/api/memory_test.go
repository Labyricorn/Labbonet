package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Labyricorn/Labbonet/internal/egress"
	"github.com/Labyricorn/Labbonet/internal/generate"
	"github.com/Labyricorn/Labbonet/internal/memory"
	"github.com/Labyricorn/Labbonet/internal/secrets"
	"github.com/Labyricorn/Labbonet/internal/store"
	"github.com/Labyricorn/Labbonet/internal/store/storetest"
)

func newMemoryMux(t *testing.T, providerURL string) (*http.ServeMux, *store.Store, int64) {
	t.Helper()
	st := storetest.New(t)
	rn := &generate.Runner{Store: st, Protector: secrets.NewProtector(), Egress: egress.NewClient(), Slots: generate.NewSlots(), Registry: generate.NewRegistry()}
	m := &Memory{Store: st, Runner: rn}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/memory/settings", m.GetSettings)
	mux.HandleFunc("PUT /api/memory/settings", m.PutSettings)
	mux.HandleFunc("POST /api/memory/storybook/preview", m.PreviewStorybook)
	mux.HandleFunc("GET /api/memory/embeddings", m.EmbeddingStats)
	mux.HandleFunc("DELETE /api/memory/embeddings", m.ClearEmbeddings)
	mux.HandleFunc("GET /api/conversations/{id}/memory", m.Conversation)
	mux.HandleFunc("PUT /api/conversations/{id}/summary", m.EditSummary)
	mux.HandleFunc("POST /api/conversations/{id}/summary/compress", m.SummariseNow)
	ctx := context.Background()
	var epID *int64
	var model *string
	if providerURL != "" {
		ep, err := st.CreateEndpoint(ctx, "fake", "openai", providerURL, "local", 1)
		if err != nil {
			t.Fatal(err)
		}
		mm := "m"
		epID, model = &ep.ID, &mm
		if err = st.UpsertModel(ctx, store.Model{EndpointID: ep.ID, ModelID: "m", Type: "llm", State: "loaded", ContextLength: 4000, SeenAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	conv, err := st.CreateConversation(ctx, "T", epID, model, "", "{}")
	if err != nil {
		t.Fatal(err)
	}
	return mux, st, conv.ID
}

func TestMemoryAPI_SettingsValidationAndConflict(t *testing.T) {
	mux, _, _ := newMemoryMux(t, "")
	var s memory.Settings
	rr := doJSON(t, mux, http.MethodGet, "/api/memory/settings", nil)
	if err := json.NewDecoder(rr.Body).Decode(&s); err != nil || rr.Code != 200 || s.Revision != 1 || !s.RetrieverB {
		t.Fatalf("%d %+v %v", rr.Code, s, err)
	}
	s.WarmthTurns = 5
	rr = doJSON(t, mux, http.MethodPut, "/api/memory/settings", s)
	if rr.Code != 200 {
		t.Fatalf("save: %d %s", rr.Code, rr.Body)
	}
	// The same (now stale) revision is a conflict.
	if rr = doJSON(t, mux, http.MethodPut, "/api/memory/settings", s); rr.Code != http.StatusConflict {
		t.Fatalf("stale: %d", rr.Code)
	}
	s.Revision = 2
	s.RetrievalBudget = -1
	if rr = doJSON(t, mux, http.MethodPut, "/api/memory/settings", s); rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid: %d", rr.Code)
	}
	s.Revision = 0
	if rr = doJSON(t, mux, http.MethodPut, "/api/memory/settings", s); rr.Code != http.StatusBadRequest {
		t.Fatalf("no revision: %d", rr.Code)
	}
}

func TestMemoryAPI_StorybookPreviewParsesWithoutStoringAnything(t *testing.T) {
	mux, st, _ := newMemoryMux(t, "")
	body := map[string]string{"storybook": "# selah\ntags: angel, divine [0.5]\nrelation: ghost\nAn angel.\n---\nLoose prose."}
	rr := doJSON(t, mux, http.MethodPost, "/api/memory/storybook/preview", body)
	var out storybookPreview
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil || rr.Code != 200 {
		t.Fatalf("%d %v", rr.Code, err)
	}
	if out.Counts.Entities != 1 || out.Counts.Tags != 2 || out.Chunks != 1 || out.Docs != 2 || len(out.Entities) != 1 || out.Entities[0].Tags != 2 {
		t.Fatalf("%+v", out)
	}
	if !strings.Contains(strings.Join(out.Warnings, "|"), `relation "ghost"`) {
		t.Fatalf("warnings=%v", out.Warnings)
	}
	if cards, _ := st.ListCards(context.Background()); len(cards) != 0 {
		t.Fatal("preview stored a card")
	}
	big := map[string]string{"storybook": strings.Repeat("x", memory.MaxStorybookBytes+1)}
	if rr = doJSON(t, mux, http.MethodPost, "/api/memory/storybook/preview", big); rr.Code != http.StatusBadRequest {
		t.Fatalf("oversized: %d", rr.Code)
	}
}

func TestMemoryAPI_SummaryEditClearAndHistory(t *testing.T) {
	mux, st, id := newMemoryMux(t, "")
	ctx := context.Background()
	var ids []int64
	for _, m := range []struct{ role, text string }{{"user", "q1"}, {"assistant", "a1"}, {"user", "q2"}, {"assistant", "a2"}} {
		msg, err := st.CreateMessage(ctx, id, nil, m.role, m.text, "", "complete")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, msg.ID)
	}
	base := "/api/conversations/" + strconv.FormatInt(id, 10)
	if _, err := st.AddSummary(ctx, store.Summary{ConversationID: id, Text: "auto text", ThroughMessageID: &ids[1], AnchorMessageID: &ids[1], ArchivedCount: 2, Source: "auto"}, 0); err != nil {
		t.Fatal(err)
	}
	var mem memoryResponse
	rr := doJSON(t, mux, http.MethodGet, base+"/memory", nil)
	if err := json.NewDecoder(rr.Body).Decode(&mem); err != nil || rr.Code != 200 || mem.Summary == nil || mem.Summary.Text != "auto text" || mem.Archived != 2 || mem.Live != 2 || mem.Seq != 1 {
		t.Fatalf("%d %+v %v", rr.Code, mem, err)
	}
	// Hand edit keeps the archive boundary and bumps the revision.
	rr = doJSON(t, mux, http.MethodPut, base+"/summary", summaryEdit{Text: "edited by hand", ExpectedSeq: 1})
	var saved store.Summary
	if err := json.NewDecoder(rr.Body).Decode(&saved); err != nil || rr.Code != 200 || saved.Seq != 2 || saved.Source != "manual" || saved.ThroughMessageID == nil || *saved.ThroughMessageID != ids[1] {
		t.Fatalf("%d %+v %v", rr.Code, saved, err)
	}
	// An editor holding the old revision is refused.
	if rr = doJSON(t, mux, http.MethodPut, base+"/summary", summaryEdit{Text: "late", ExpectedSeq: 1}); rr.Code != http.StatusConflict {
		t.Fatalf("stale edit: %d", rr.Code)
	}
	// Clearing returns the archived messages to the prompt.
	if rr = doJSON(t, mux, http.MethodPut, base+"/summary", summaryEdit{Text: "", ExpectedSeq: 2}); rr.Code != 200 {
		t.Fatalf("clear: %d %s", rr.Code, rr.Body)
	}
	rr = doJSON(t, mux, http.MethodGet, base+"/memory", nil)
	mem = memoryResponse{}
	if err := json.NewDecoder(rr.Body).Decode(&mem); err != nil || mem.Summary == nil || mem.Summary.Text != "" || mem.Archived != 0 || mem.Live != 4 || len(mem.History) != 3 {
		t.Fatalf("after clear: %+v %v", mem, err)
	}
	if msgs, _ := st.ListMessages(ctx, id); len(msgs) != 4 {
		t.Fatalf("messages changed: %d", len(msgs))
	}
	if rr = doJSON(t, mux, http.MethodGet, "/api/conversations/9999/memory", nil); rr.Code != http.StatusNotFound {
		t.Fatalf("missing conversation: %d", rr.Code)
	}
}

func TestMemoryAPI_SummariseNowIsExplicitAndBounded(t *testing.T) {
	var summaryRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[{"id":"m"}]}`))
			return
		}
		summaryRequests++
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"They talked about dragons."},"finish_reason":null}]}`+"\n\n"+`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	mux, st, id := newMemoryMux(t, srv.URL)
	ctx := context.Background()
	base := "/api/conversations/" + strconv.FormatInt(id, 10)
	// Too little conversation: refused without any provider call.
	st.CreateMessage(ctx, id, nil, "user", "hi", "", "complete")
	if rr := doJSON(t, mux, http.MethodPost, base+"/summary/compress", nil); rr.Code != http.StatusBadRequest || summaryRequests != 0 {
		t.Fatalf("%d requests=%d", rr.Code, summaryRequests)
	}
	for _, m := range []struct{ role, text string }{{"assistant", "hello"}, {"user", "dragons?"}, {"assistant", "yes"}, {"user", "latest question"}} {
		st.CreateMessage(ctx, id, nil, m.role, m.text, "", "complete")
	}
	rr := doJSON(t, mux, http.MethodPost, base+"/summary/compress", nil)
	var out summariseResponse
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil || rr.Code != 200 || out.Summary == nil || out.Summary.Text != "They talked about dragons." || summaryRequests != 1 {
		t.Fatalf("%d %+v %v requests=%d", rr.Code, out, err, summaryRequests)
	}
	// The most recent user turn stays verbatim: five messages, summary covers the first four.
	var mem memoryResponse
	rr = doJSON(t, mux, http.MethodGet, base+"/memory", nil)
	json.NewDecoder(rr.Body).Decode(&mem)
	if mem.Archived != 4 || mem.Live != 1 {
		t.Fatalf("archived=%d live=%d", mem.Archived, mem.Live)
	}
	// A conversation without a model cannot be summarised.
	noModelMux, _, noModelID := newMemoryMux(t, "")
	if rr = doJSON(t, noModelMux, http.MethodPost, "/api/conversations/"+strconv.FormatInt(noModelID, 10)+"/summary/compress", nil); rr.Code != http.StatusConflict {
		t.Fatalf("no model: %d", rr.Code)
	}
}
