package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Labyricorn/Labbonet/internal/cast"
	"github.com/Labyricorn/Labbonet/internal/memory"
	"github.com/Labyricorn/Labbonet/internal/providers"
	"github.com/Labyricorn/Labbonet/internal/store"
)

// memProvider is a scripted OpenAI-compatible server that tells summarisation requests
// apart from ordinary chat and answers /v1/embeddings from a fixed vector table.
type memProvider struct {
	*httptest.Server
	mu          sync.Mutex
	chats       []providers.ChatRequest // ordinary chats, in order
	summaries   []providers.ChatRequest // summarisation requests, in order
	embedCalls  [][]string              // inputs of each embeddings request
	embedModel  []string
	summary     string // what the summariser answers
	onSummary   func() // runs while a summarisation request is being served
	vectors     func(string) []float32
	embedStatus int
}

func newMemProvider(t *testing.T) *memProvider {
	t.Helper()
	p := &memProvider{summary: "The travellers met a dragon and made peace.",
		vectors: func(s string) []float32 {
			if strings.Contains(s, "winter") {
				return []float32{0, 1}
			}
			return []float32{1, 0}
		}}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/completions":
			var req providers.ChatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			isSummary := len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "running summary")
			p.mu.Lock()
			if isSummary {
				p.summaries = append(p.summaries, req)
			} else {
				p.chats = append(p.chats, req)
			}
			text, hook := "ok", p.onSummary
			if isSummary {
				text = p.summary
			}
			p.mu.Unlock()
			if isSummary && hook != nil {
				hook()
			}
			w.Header().Set("Content-Type", "text/event-stream")
			raw, _ := json.Marshal(text)
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":null}]}\n\n", raw)
			w.Write([]byte(`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"model":"m"}` + "\n\ndata: [DONE]\n\n"))
		case "/v1/embeddings":
			if p.embedStatus != 0 {
				http.Error(w, `{"error":{"message":"embedding server down"}}`, p.embedStatus)
				return
			}
			var req struct {
				Model string   `json:"model"`
				Input []string `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			p.mu.Lock()
			p.embedCalls = append(p.embedCalls, req.Input)
			p.embedModel = append(p.embedModel, req.Model)
			p.mu.Unlock()
			var data []map[string]any
			for i, in := range req.Input {
				data = append(data, map[string]any{"index": i, "embedding": p.vectors(in)})
			}
			json.NewEncoder(w).Encode(map[string]any{"data": data, "model": req.Model})
		default:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[{"id":"m"}]}`))
		}
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *memProvider) counts() (chats, summaries, embeds int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.chats), len(p.summaries), len(p.embedCalls)
}

func (p *memProvider) lastChat() providers.ChatRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.chats[len(p.chats)-1]
}

// fillConversation appends n user/assistant pairs of padded text so the live history is
// larger than a 1000-unit context window can hold.
func fillConversation(t *testing.T, st *store.Store, convID int64, pairs int) {
	t.Helper()
	for i := 0; i < pairs; i++ {
		u := fmt.Sprintf("OLD-USER-%02d %s", i, strings.Repeat("u", 120))
		a := fmt.Sprintf("OLD-ASSISTANT-%02d %s", i, strings.Repeat("a", 120))
		for _, m := range [][2]string{{"user", u}, {"assistant", a}} {
			if _, err := st.CreateMessage(t.Context(), convID, nil, m[0], m[1], "", "complete"); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func setModel(t *testing.T, st *store.Store, epID int64, id, typ, state string, ctxLen int) {
	t.Helper()
	if err := st.UpsertModel(t.Context(), store.Model{EndpointID: epID, ModelID: id, Type: typ, State: state, ContextLength: ctxLen, SeenAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func enableSummaries(t *testing.T, st *store.Store, mutate func(*memory.Settings)) {
	t.Helper()
	s, err := st.GetMemorySettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	s.SummarizeEnabled, s.SummaryMaxChars = true, 500
	if mutate != nil {
		mutate(&s)
	}
	if _, err = st.SaveMemorySettings(t.Context(), s); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, rn *Runner, convID int64, text string) error {
	t.Helper()
	return rn.Run(context.Background(), Request{ConversationID: convID, UserContent: text, MaxTokens: 50}, nil, func(providers.Event) error { return nil })
}

func summaryRows(t *testing.T, st *store.Store, convID int64) []store.Summary {
	t.Helper()
	rows, err := st.ListSummaries(t.Context(), convID, 20)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestPlanArchiveTriggersLandsAndKeepsTheRecentTail(t *testing.T) {
	costs := make([]int, 12)
	isUser := make([]bool, 12)
	for i := range costs {
		costs[i], isUser[i] = 150, i%2 == 0
	}
	// Fits with room for a reply: nothing to do.
	if n := planArchive(4000, 100, 0, costs, isUser); n != 0 {
		t.Fatalf("archived %d though everything fits", n)
	}
	// Does not fit: archive from the oldest, stop at a turn boundary, keep the tail.
	n := planArchive(1000, 100, 0, costs, isUser)
	if n == 0 || n >= len(costs) || !isUser[n] || n%2 != 0 {
		t.Fatalf("n=%d", n)
	}
	remaining := 100
	for _, c := range costs[n:] {
		remaining += c
	}
	if remaining > 1000*90/100*landingPct/100 {
		t.Fatalf("lands at %d, above the target", remaining)
	}
	// A single message is never archived, and a tiny conversation is left alone.
	if planArchive(300, 100, 0, []int{500}, []bool{true}) != 0 || planArchive(300, 100, 0, nil, nil) != 0 {
		t.Fatal("archived a lone message")
	}
}

func TestCleanSummary(t *testing.T) {
	good, err := cleanSummary("<think>plan</think>\nSummary: They won.", 100)
	if err != nil || good != "They won." {
		t.Fatalf("%q %v", good, err)
	}
	if _, err = cleanSummary("   ", 100); err == nil {
		t.Fatal("empty accepted")
	}
	if _, err = cleanSummary(strings.Repeat("x", 151), 100); err == nil {
		t.Fatal("far oversized accepted")
	}
	cut, err := cleanSummary(strings.Repeat("Sentence one. ", 8), 100)
	if err != nil || len(cut) > 100 || !strings.HasSuffix(cut, ".") {
		t.Fatalf("%q %v", cut, err)
	}
}

func TestRunnerSummarisesOldMessagesAndNeverLosesSourceHistory(t *testing.T) {
	p := newMemProvider(t)
	rn, st := newRunner(t)
	ep, conv := mustEndpointAndConversation(t, st, p.URL, "m")
	setModel(t, st, ep.ID, "m", "llm", "loaded", 4000)
	fillConversation(t, st, conv.ID, 10)
	enableSummaries(t, st, nil)

	if err := run(t, rn, conv.ID, "and then?"); err != nil {
		t.Fatal(err)
	}
	chats, sums, _ := p.counts()
	if chats != 1 || sums != 1 {
		t.Fatalf("chats=%d summaries=%d", chats, sums)
	}
	rows := summaryRows(t, st, conv.ID)
	if len(rows) != 1 || rows[0].Source != "auto" || rows[0].Seq != 1 || rows[0].ThroughMessageID == nil || rows[0].Model != "m" {
		t.Fatalf("rows=%+v", rows)
	}
	sent := sent(p.lastChat())
	if !strings.Contains(sent, "system:") || !strings.Contains(sent, "[Summary of earlier conversation]\nThe travellers met a dragon and made peace.") {
		t.Fatalf("prompt lacks the summary: %s", sent)
	}
	if strings.Contains(sent, "OLD-USER-00") || !strings.Contains(sent, "OLD-ASSISTANT-09") || !strings.HasSuffix(sent, "user:and then?") {
		t.Fatalf("archived text leaked or recent tail lost: %s", sent)
	}
	// The summariser saw the old messages (and only reasoning-free text), as a bounded request.
	if tr := p.summaries[0].Messages[1].Content; !strings.Contains(tr, "OLD-USER-00") || !strings.Contains(tr, "[Existing summary]") {
		t.Fatalf("summariser input: %s", tr)
	}
	// Source history is untouched.
	msgs, err := st.ListMessages(t.Context(), conv.ID)
	if err != nil || len(msgs) != 20+2 {
		t.Fatalf("messages=%d err=%v", len(msgs), err)
	}
	// Provenance records the summary revision and archived count, and the inspector has a record.
	gens, _ := st.ListRecentGenerations(t.Context(), 1)
	if !strings.Contains(gens[0].ParamsJSON, `"summary_seq":1`) || !strings.Contains(gens[0].ParamsJSON, `"archived_messages"`) {
		t.Fatalf("params lack memory provenance: %s", gens[0].ParamsJSON)
	}
	recs, _ := st.ListRetrievalRecords(t.Context(), conv.ID, 5)
	if len(recs) != 1 || !strings.Contains(string(recs[0].Record), "summarised") {
		t.Fatalf("records=%v", recs)
	}
	// Next turn fits: no second summarisation, but the summary keeps being used.
	if err = run(t, rn, conv.ID, "go on"); err != nil {
		t.Fatal(err)
	}
	if _, sums, _ = p.counts(); sums != 1 {
		t.Fatalf("summarised again: %d", sums)
	}
	if s := sent2(p); !strings.Contains(s, "[Summary of earlier conversation]") || strings.Contains(s, "OLD-USER-00") {
		t.Fatalf("second prompt: %s", s)
	}
}

func sent2(p *memProvider) string { return sent(p.lastChat()) }

func TestRunnerDoesNotSummariseUnlessEnabledOrNeeded(t *testing.T) {
	p := newMemProvider(t)
	rn, st := newRunner(t)
	ep, conv := mustEndpointAndConversation(t, st, p.URL, "m")
	setModel(t, st, ep.ID, "m", "llm", "loaded", 4000)
	fillConversation(t, st, conv.ID, 10)
	if err := run(t, rn, conv.ID, "hi"); err != nil { // disabled by default
		t.Fatal(err)
	}
	if _, sums, _ := p.counts(); sums != 0 {
		t.Fatal("summarised while disabled")
	}
	if rows := summaryRows(t, st, conv.ID); len(rows) != 0 {
		t.Fatalf("rows=%+v", rows)
	}
	// Enabled but everything fits: still no call.
	_, short := mustEndpointAndConversation(t, st, p.URL, "m")
	enableSummaries(t, st, nil)
	if err := run(t, rn, short.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	if _, sums, _ := p.counts(); sums != 0 {
		t.Fatal("summarised a short conversation")
	}
}

func TestRunnerFailedSummaryKeepsHistoryReportsGapAndBacksOff(t *testing.T) {
	p := newMemProvider(t)
	p.summary = "   "
	rn, st := newRunner(t)
	ep, conv := mustEndpointAndConversation(t, st, p.URL, "m")
	setModel(t, st, ep.ID, "m", "llm", "loaded", 4000)
	fillConversation(t, st, conv.ID, 16)
	enableSummaries(t, st, nil)
	if err := run(t, rn, conv.ID, "hello"); err != nil {
		t.Fatal(err)
	}
	rows := summaryRows(t, st, conv.ID)
	if len(rows) != 1 || rows[0].Source != "failed" || rows[0].Text != "" || !strings.Contains(rows[0].Note, "no summary") {
		t.Fatalf("rows=%+v", rows)
	}
	// The turn still proceeded: older text is trimmed from the prompt, not from the database.
	if s := sent(p.lastChat()); strings.Contains(s, "OLD-USER-00") || !strings.Contains(s, "OLD-ASSISTANT-15") || strings.Contains(s, "Summary of earlier") {
		t.Fatalf("prompt: %s", s)
	}
	if msgs, _ := st.ListMessages(t.Context(), conv.ID); len(msgs) != 34 {
		t.Fatalf("messages=%d", len(msgs))
	}
	recs, _ := st.ListRetrievalRecords(t.Context(), conv.ID, 1)
	if len(recs) != 1 || !strings.Contains(string(recs[0].Record), "without a summary") || !strings.Contains(string(recs[0].Record), "summary failed") {
		t.Fatalf("gap not reported: %v", recs)
	}
	// An immediate retry is paused instead of calling the model again.
	if err := run(t, rn, conv.ID, "again"); err != nil {
		t.Fatal(err)
	}
	if _, sums, _ := p.counts(); sums != 1 {
		t.Fatalf("retried within the backoff: %d", sums)
	}
	// Far-oversized output is a failure too.
	p2 := newMemProvider(t)
	p2.summary = strings.Repeat("x", 9000)
	rn2, st2 := newRunner(t)
	ep2, conv2 := mustEndpointAndConversation(t, st2, p2.URL, "m")
	setModel(t, st2, ep2.ID, "m", "llm", "loaded", 4000)
	fillConversation(t, st2, conv2.ID, 10)
	enableSummaries(t, st2, nil)
	if err := run(t, rn2, conv2.ID, "hello"); err != nil {
		t.Fatal(err)
	}
	if rows := summaryRows(t, st2, conv2.ID); len(rows) != 1 || rows[0].Source != "failed" {
		t.Fatalf("oversized summary accepted: %+v", rows)
	}
}

func TestRunnerNeverCallsANotLoadedCompressionModel(t *testing.T) {
	p := newMemProvider(t)
	rn, st := newRunner(t)
	ep, conv := mustEndpointAndConversation(t, st, p.URL, "m")
	setModel(t, st, ep.ID, "m", "llm", "loaded", 4000)
	setModel(t, st, ep.ID, "small", "llm", "not-loaded", 1000)
	fillConversation(t, st, conv.ID, 10)
	enableSummaries(t, st, func(s *memory.Settings) { s.CompressionEndpointID, s.CompressionModel = &ep.ID, "small" })
	if err := run(t, rn, conv.ID, "hello"); err != nil {
		t.Fatal(err)
	}
	// The not-loaded model was skipped; the loaded chat model wrote the summary.
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.summaries {
		if s.Model == "small" {
			t.Fatal("a not-loaded model received a request")
		}
	}
	if len(p.summaries) != 1 || p.summaries[0].Model != "m" {
		t.Fatalf("summaries=%+v", p.summaries)
	}
}

func TestRunnerNotLoadedEverywhereFailsWithoutAnyCall(t *testing.T) {
	p := newMemProvider(t)
	rn, st := newRunner(t)
	ep, conv := mustEndpointAndConversation(t, st, p.URL, "m")
	setModel(t, st, ep.ID, "m", "llm", "not-loaded", 1000)
	fillConversation(t, st, conv.ID, 10)
	enableSummaries(t, st, nil)
	if err := rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "x", MaxTokens: 50, ConfirmLoad: true}, nil, func(providers.Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, sums, _ := p.counts(); sums != 0 {
		t.Fatal("summarisation request reached a not-loaded model")
	}
	if rows := summaryRows(t, st, conv.ID); len(rows) != 1 || rows[0].Source != "failed" {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestRunnerManualEditDuringPassWinsOverTheAutomaticResult(t *testing.T) {
	p := newMemProvider(t)
	rn, st := newRunner(t)
	ep, conv := mustEndpointAndConversation(t, st, p.URL, "m")
	setModel(t, st, ep.ID, "m", "llm", "loaded", 4000)
	fillConversation(t, st, conv.ID, 10)
	enableSummaries(t, st, nil)
	p.onSummary = func() {
		if _, err := st.AddSummary(context.Background(), store.Summary{ConversationID: conv.ID, Text: "HAND-WRITTEN", Source: "manual"}, 0); err != nil {
			t.Error(err)
		}
	}
	if err := run(t, rn, conv.ID, "hello"); err != nil {
		t.Fatal(err)
	}
	rows := summaryRows(t, st, conv.ID)
	if len(rows) != 1 || rows[0].Text != "HAND-WRITTEN" {
		t.Fatalf("automatic result overwrote a manual edit: %+v", rows)
	}
	if !strings.Contains(sent(p.lastChat()), "HAND-WRITTEN") {
		t.Fatalf("manual summary not used: %s", sent(p.lastChat()))
	}
}

func TestSummaryAppliesOnlyToItsOwnBranch(t *testing.T) {
	p := newMemProvider(t)
	rn, st := newRunner(t)
	ep, conv := mustEndpointAndConversation(t, st, p.URL, "m")
	setModel(t, st, ep.ID, "m", "llm", "loaded", 4000)
	fillConversation(t, st, conv.ID, 2)
	msgs, _ := st.ListMessages(t.Context(), conv.ID)
	through := msgs[1].ID
	if _, err := st.AddSummary(t.Context(), store.Summary{ConversationID: conv.ID, Text: "BRANCH-SUMMARY", ThroughMessageID: &through, AnchorMessageID: &through, ArchivedCount: 2, Source: "manual"}, 0); err != nil {
		t.Fatal(err)
	}
	if err := run(t, rn, conv.ID, "next"); err != nil {
		t.Fatal(err)
	}
	s := sent(p.lastChat())
	if !strings.Contains(s, "BRANCH-SUMMARY") || strings.Contains(s, "OLD-USER-00") || !strings.Contains(s, "OLD-USER-01") {
		t.Fatalf("summary boundary: %s", s)
	}
	// Edit the first message: the new variant is a different branch, which does not contain
	// the archived message, so the summary must not apply and the full text returns.
	rev := conv.Revision
	if c, err := st.GetConversation(t.Context(), conv.ID); err == nil {
		rev = c.Revision
	}
	if _, err := st.EditMessage(t.Context(), conv.ID, msgs[0].ID, "EDITED-OPENING", rev); err != nil {
		t.Fatal(err)
	}
	if err := run(t, rn, conv.ID, "after edit"); err != nil {
		t.Fatal(err)
	}
	s = sent(p.lastChat())
	if strings.Contains(s, "BRANCH-SUMMARY") || !strings.Contains(s, "EDITED-OPENING") {
		t.Fatalf("summary leaked onto another branch: %s", s)
	}
}

func TestStopDuringPreparationStoresNothing(t *testing.T) {
	p := newMemProvider(t)
	rn, st := newRunner(t)
	ep, conv := mustEndpointAndConversation(t, st, p.URL, "m")
	setModel(t, st, ep.ID, "m", "llm", "loaded", 4000)
	fillConversation(t, st, conv.ID, 10)
	enableSummaries(t, st, nil)
	prep, cancel := context.WithCancel(context.Background())
	p.onSummary = func() { cancel(); time.Sleep(300 * time.Millisecond) }
	_, err := NewJobs(rn).StartContext(prep, Request{ConversationID: conv.ID, UserContent: "never sent", MaxTokens: 50})
	if err == nil {
		t.Fatal("start succeeded after the request was cancelled")
	}
	msgs, _ := st.ListMessages(t.Context(), conv.ID)
	if len(msgs) != 20 {
		t.Fatalf("a turn was stored: %d messages", len(msgs))
	}
	if rows := summaryRows(t, st, conv.ID); len(rows) != 0 {
		t.Fatalf("summary stored after cancel: %+v", rows)
	}
}

func saveCharacter(t *testing.T, st *store.Store, convID int64, storybook string) {
	t.Helper()
	card, err := st.SaveCard(t.Context(), cast.Card{Kind: "character", Name: "Archivist", Storybook: storybook})
	if err != nil {
		t.Fatal(err)
	}
	sel, _ := st.GetCast(t.Context(), convID)
	sel.CharacterID = &card.ID
	if _, err = st.SaveCast(t.Context(), convID, sel); err != nil {
		t.Fatal(err)
	}
}

const testBook = "# selah\ntags: angel\nSelah is an angel who keeps the gate.\n---\nThe long winter follows the harvest.\n"

func TestRunnerInjectsRetrievedLoreAndRecordsIt(t *testing.T) {
	p := newMemProvider(t)
	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, p.URL, "m")
	saveCharacter(t, st, conv.ID, testBook)
	if err := run(t, rn, conv.ID, "tell me about the weather"); err != nil {
		t.Fatal(err)
	}
	if s := sent2(p); strings.Contains(s, "Relevant lore") {
		t.Fatalf("lore injected without a trigger: %s", s)
	}
	if err := run(t, rn, conv.ID, "is that an angel?"); err != nil {
		t.Fatal(err)
	}
	s := sent2(p)
	if !strings.Contains(s, "system:[Relevant lore]\nSelah is an angel who keeps the gate.|user:is that an angel?") {
		t.Fatalf("lore missing or misplaced: %s", s)
	}
	if _, _, embeds := p.counts(); embeds != 0 {
		t.Fatal("embeddings requested although semantic retrieval is off")
	}
	recs, _ := st.ListRetrievalRecords(t.Context(), conv.ID, 5)
	if len(recs) != 2 || !strings.Contains(string(recs[0].Record), `"injected":["selah"]`) {
		t.Fatalf("records=%v", recs)
	}
	if strings.Contains(string(recs[0].Record), "keeps the gate") || strings.Contains(string(recs[0].Record), "angel?") {
		t.Fatalf("record stores text: %s", recs[0].Record)
	}
	gens, _ := st.ListRecentGenerations(t.Context(), 1)
	if !strings.Contains(gens[0].ParamsJSON, `"retrieved":["selah"]`) {
		t.Fatalf("provenance lacks retrieval: %s", gens[0].ParamsJSON)
	}
	// Turning retrieval off removes injection.
	set, _ := st.GetMemorySettings(t.Context())
	set.RetrievalEnabled = false
	if _, err := st.SaveMemorySettings(t.Context(), set); err != nil {
		t.Fatal(err)
	}
	if err := run(t, rn, conv.ID, "another angel"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sent2(p), "Relevant lore") {
		t.Fatal("lore injected while disabled")
	}
}

func semanticSetup(t *testing.T, p *memProvider, state string) (*Runner, *store.Store, *store.Endpoint, *store.Conversation) {
	t.Helper()
	rn, st := newRunner(t)
	ep, conv := mustEndpointAndConversation(t, st, p.URL, "m")
	setModel(t, st, ep.ID, "embed-a", "embeddings", state, 2048)
	saveCharacter(t, st, conv.ID, testBook)
	set, _ := st.GetMemorySettings(t.Context())
	set.RetrieverA, set.EmbeddingEndpointID, set.EmbeddingModel = true, &ep.ID, "embed-a"
	set.EmbedDocPrefix, set.EmbedQueryPrefix = "doc: ", "query: "
	if _, err := st.SaveMemorySettings(t.Context(), set); err != nil {
		t.Fatal(err)
	}
	return rn, st, ep, conv
}

func TestRunnerSemanticRetrievalUsesAndCachesEmbeddings(t *testing.T) {
	p := newMemProvider(t)
	rn, st, ep, conv := semanticSetup(t, p, "loaded")
	// "snow" shares no tag with the storybook, but the query vector points at the winter prose.
	p.vectors = func(s string) []float32 {
		if strings.Contains(s, "snow") || strings.Contains(s, "winter") {
			return []float32{0, 1}
		}
		return []float32{1, 0}
	}
	if err := run(t, rn, conv.ID, "is snow coming?"); err != nil {
		t.Fatal(err)
	}
	if s := sent2(p); !strings.Contains(s, "[Relevant lore]\nThe long winter follows the harvest.") {
		t.Fatalf("semantic hit missing: %s", s)
	}
	// One request carried the query and both documents, with the configured prefixes.
	if len(p.embedCalls) != 1 || len(p.embedCalls[0]) != 3 || !strings.HasPrefix(p.embedCalls[0][0], "query: ") || !strings.HasPrefix(p.embedCalls[0][1], "doc: ") || p.embedModel[0] != "embed-a" {
		t.Fatalf("embed calls=%v models=%v", p.embedCalls, p.embedModel)
	}
	// Second turn: documents come from the cache, only the query is embedded.
	if err := run(t, rn, conv.ID, "more snow please"); err != nil {
		t.Fatal(err)
	}
	if len(p.embedCalls) != 2 || len(p.embedCalls[1]) != 1 {
		t.Fatalf("documents were re-embedded: %v", p.embedCalls)
	}
	// Changing the document text invalidates exactly that document.
	saveCharacterAgain := func(book string) {
		cards, _ := st.ListCards(t.Context())
		c := cards[0]
		c.Storybook = book
		if _, err := st.SaveCard(t.Context(), c); err != nil {
			t.Fatal(err)
		}
	}
	saveCharacterAgain(strings.Replace(testBook, "harvest", "first frost", 1))
	if err := run(t, rn, conv.ID, "snow again"); err != nil {
		t.Fatal(err)
	}
	if len(p.embedCalls) != 3 || len(p.embedCalls[2]) != 2 || !strings.Contains(p.embedCalls[2][1], "first frost") {
		t.Fatalf("changed doc not re-embedded alone: %v", p.embedCalls)
	}
	// A different embedding model never reuses another model's vectors.
	setModel(t, st, ep.ID, "embed-b", "embeddings", "loaded", 2048)
	set, _ := st.GetMemorySettings(t.Context())
	set.EmbeddingModel = "embed-b"
	if _, err := st.SaveMemorySettings(t.Context(), set); err != nil {
		t.Fatal(err)
	}
	if err := run(t, rn, conv.ID, "snow once more"); err != nil {
		t.Fatal(err)
	}
	if len(p.embedCalls) != 4 || len(p.embedCalls[3]) != 3 || p.embedModel[3] != "embed-b" {
		t.Fatalf("cache reused across models: %v %v", p.embedCalls, p.embedModel)
	}
	stats, _ := st.EmbeddingCacheStats(t.Context())
	if len(stats) != 2 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestRunnerSemanticFallsBackToTagsAndNeverLoadsAnEmbeddingModel(t *testing.T) {
	p := newMemProvider(t)
	rn, st, _, conv := semanticSetup(t, p, "not-loaded")
	if err := run(t, rn, conv.ID, "an angel appears"); err != nil {
		t.Fatal(err)
	}
	if _, _, embeds := p.counts(); embeds != 0 {
		t.Fatal("an embedding request reached a not-loaded model")
	}
	if !strings.Contains(sent2(p), "Selah is an angel") {
		t.Fatalf("tag retrieval did not carry on: %s", sent2(p))
	}
	recs, _ := st.ListRetrievalRecords(t.Context(), conv.ID, 1)
	if !strings.Contains(string(recs[0].Record), "not loaded") {
		t.Fatalf("reason not recorded: %s", recs[0].Record)
	}
}

func TestRunnerEmbeddingFailureDegradesToTags(t *testing.T) {
	p := newMemProvider(t)
	p.embedStatus = http.StatusInternalServerError
	rn, st, _, conv := semanticSetup(t, p, "loaded")
	if err := run(t, rn, conv.ID, "an angel appears"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent2(p), "Selah is an angel") {
		t.Fatalf("tag retrieval lost with the embedding server down: %s", sent2(p))
	}
	recs, _ := st.ListRetrievalRecords(t.Context(), conv.ID, 1)
	if !strings.Contains(string(recs[0].Record), "embedding request failed") {
		t.Fatalf("failure not recorded: %s", recs[0].Record)
	}
	if stats, _ := st.EmbeddingCacheStats(t.Context()); len(stats) != 0 {
		t.Fatalf("cache written after a failure: %+v", stats)
	}
}

// 1.1: retrieval works on a private copy of the warmth map and publishes it only for a stored turn.
func TestWarmthCopyIsPrivateUntilCommitted(t *testing.T) {
	rn := &Runner{}
	rn.commitWarmth(1, memory.Warmth{"a": 3})
	w := rn.warmthCopy(1)
	w["a"], w["b"] = 1, 2
	if got := rn.warmthCopy(1); len(got) != 1 || got["a"] != 3 {
		t.Fatalf("committed warmth changed before commit: %v", got)
	}
	rn.commitWarmth(1, w)
	if got := rn.warmthCopy(1); got["a"] != 1 || got["b"] != 2 {
		t.Fatalf("commit lost: %v", got)
	}
	// Overlapping readers/writers must be race-free (run with -race).
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c := rn.warmthCopy(1)
				c["x"]++
				rn.commitWarmth(1, c)
			}
		}()
	}
	wg.Wait()
}

func TestRefusedTurnDoesNotAgeOrAddWarmth(t *testing.T) {
	p := newMemProvider(t)
	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, p.URL, "m")
	saveCharacter(t, st, conv.ID, testBook)
	if err := run(t, rn, conv.ID, "is that an angel?"); err != nil {
		t.Fatal(err)
	}
	before := rn.warmthCopy(conv.ID)
	if len(before) == 0 {
		t.Skip("default settings keep no warmth; nothing to protect")
	}
	err := rn.Run(t.Context(), Request{ConversationID: conv.ID, UserContent: "angel again", ExpectedRevision: 9999}, nil, noop)
	if err == nil {
		t.Fatal("stale revision was accepted")
	}
	after := rn.warmthCopy(conv.ID)
	if len(after) != len(before) {
		t.Fatalf("warmth changed by a refused turn: %v -> %v", before, after)
	}
	for id, n := range before {
		if after[id] != n {
			t.Fatalf("warmth aged by a refused turn: %v -> %v", before, after)
		}
	}
}
