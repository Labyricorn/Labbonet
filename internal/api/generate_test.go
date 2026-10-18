package api

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Labyricorn/Labbonet/internal/egress"
	"github.com/Labyricorn/Labbonet/internal/generate"
	"github.com/Labyricorn/Labbonet/internal/providers/fakeprovider"
	"github.com/Labyricorn/Labbonet/internal/secrets"
	"github.com/Labyricorn/Labbonet/internal/store"
	"github.com/Labyricorn/Labbonet/internal/store/storetest"
)

const generateTestSSE = `data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}` + "\n\n" +
	`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"model":"m","usage":{"prompt_tokens":1,"completion_tokens":1}}` + "\n\n" +
	"data: [DONE]\n\n"

func newGenerateHandler(t *testing.T, baseURL string) (*http.ServeMux, *store.Store, *generate.Runner, int64) {
	t.Helper()
	st := storetest.New(t)
	ctx := context.Background()
	ep, err := st.CreateEndpoint(ctx, "test", "openai", baseURL, "local", 1)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	modelID := "m"
	conv, err := st.CreateConversation(ctx, "chat", &ep.ID, &modelID, "", "{}")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	runner := &generate.Runner{
		Store: st, Protector: secrets.NewProtector(), Egress: egress.NewClient(),
		Slots: generate.NewSlots(), Registry: generate.NewRegistry(),
	}
	g := &Generate{Runner: runner}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/conversations/{id}/generate", g.Generate)
	mux.HandleFunc("POST /api/generations/{id}/cancel", g.Cancel)
	return mux, st, runner, conv.ID
}

func TestGenerate_StreamsSSEAndPersists(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{ChatSSE: generateTestSSE, ModelsPath: "/v1/models", ModelsBody: `{"data":[]}`})
	defer srv.Close()

	mux, st, _, convID := newGenerateHandler(t, srv.URL)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+strconv.FormatInt(convID, 10)+"/generate",
		bytes.NewReader([]byte(`{"content":"hi"}`)))
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}

	var sawDone bool
	scanner := bufio.NewScanner(strings.NewReader(rr.Body.String()))
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), `"kind":"done"`) {
			sawDone = true
		}
	}
	if !sawDone {
		t.Fatalf("SSE body missing a done event: %s", rr.Body.String())
	}

	msgs, err := st.ListMessages(context.Background(), convID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 || msgs[1].Content != "Hello" || msgs[1].Status != "complete" {
		t.Fatalf("messages = %+v", msgs)
	}
}

func TestGenerate_NoModelSelected(t *testing.T) {
	st := storetest.New(t)
	conv, err := st.CreateConversation(context.Background(), "chat", nil, nil, "", "{}")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	runner := &generate.Runner{Store: st, Protector: secrets.NewProtector(), Egress: egress.NewClient(), Slots: generate.NewSlots(), Registry: generate.NewRegistry()}
	g := &Generate{Runner: runner}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/conversations/{id}/generate", g.Generate)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+strconv.FormatInt(conv.ID, 10)+"/generate", bytes.NewReader([]byte(`{"content":"hi"}`)))
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s, want 409", rr.Code, rr.Body.String())
	}
}

// S15 at the API layer: a not-loaded model refuses the request with no
// event stream started.
func TestGenerate_RefusesNotLoadedModel(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{ChatSSE: generateTestSSE})
	defer srv.Close()
	mux, st, _, convID := newGenerateHandler(t, srv.URL)

	conv, err := st.GetConversation(context.Background(), convID)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if err := st.UpsertModel(context.Background(), store.Model{EndpointID: *conv.EndpointID, ModelID: *conv.ModelID, Type: "llm", State: "not-loaded", SeenAt: time.Now()}); err != nil {
		t.Fatalf("upsert model: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+strconv.FormatInt(convID, 10)+"/generate", bytes.NewReader([]byte(`{"content":"hi"}`)))
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s, want 409", rr.Code, rr.Body.String())
	}
}

// A-C4: cancelling a running generation through the API aborts it and
// keeps the partial reply, marked cancelled.
func TestGenerate_Cancel(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{
		ChatSSE: generateTestSSE, ChatFirstByteDelay: 300 * time.Millisecond,
		ModelsPath: "/v1/models", ModelsBody: `{"data":[]}`,
	})
	defer srv.Close()
	mux, st, runner, convID := newGenerateHandler(t, srv.URL)

	go func() {
		time.Sleep(50 * time.Millisecond)
		msgs, err := st.ListMessages(context.Background(), convID)
		if err != nil || len(msgs) < 2 {
			return
		}
		gens, err := st.ListGenerationsForMessage(context.Background(), msgs[1].ID)
		if err != nil || len(gens) == 0 {
			return
		}
		runner.Registry.Cancel(gens[0].ID)
	}()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+strconv.FormatInt(convID, 10)+"/generate", bytes.NewReader([]byte(`{"content":"hi"}`)))
	mux.ServeHTTP(rr, req)

	msgs, err := st.ListMessages(context.Background(), convID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 || msgs[1].Status != "cancelled" {
		t.Fatalf("assistant message = %+v, want status cancelled", msgs[1])
	}
}

func TestGenerate_CancelHandlerReportsWhetherRunning(t *testing.T) {
	runner := &generate.Runner{Registry: generate.NewRegistry()}
	g := &Generate{Runner: runner}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/generations/{id}/cancel", g.Cancel)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generations/999/cancel", nil)
	mux.ServeHTTP(rr, req)
	if !strings.Contains(rr.Body.String(), `"cancelled":false`) {
		t.Fatalf("body = %s, want cancelled:false for an unknown id", rr.Body.String())
	}
}

// The reply is a detached job: closing the request that started it must not stop it, and the
// stored outcome plus a re-attached stream show the same finished reply.
func TestGenerate_SurvivesClientDisconnectAndReattaches(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{
		ChatSSE: generateTestSSE, ChatFirstByteDelay: 300 * time.Millisecond,
		ModelsPath: "/v1/models", ModelsBody: `{"data":[]}`,
	})
	defer srv.Close()
	mux, st, runner, convID := newGenerateHandler(t, srv.URL)
	g := &Generate{Runner: runner}
	mux = http.NewServeMux()
	mux.HandleFunc("POST /api/conversations/{id}/generate", g.Generate)
	mux.HandleFunc("GET /api/generations/{id}/events", g.Events)
	mux.HandleFunc("GET /api/conversations/{id}/generation", g.Active)
	web := httptest.NewServer(mux)
	defer web.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, web.URL+"/api/conversations/"+strconv.FormatInt(convID, 10)+"/generate", strings.NewReader(`{"content":"hi"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	first := bufio.NewReader(resp.Body)
	line, _ := first.ReadString('\n')
	if !strings.Contains(line, `"kind":"snapshot"`) {
		t.Fatalf("first event = %q, want a snapshot", line)
	}
	cancel() // the browser goes away mid-reply
	resp.Body.Close()

	active, err := http.Get(web.URL + "/api/conversations/" + strconv.FormatInt(convID, 10) + "/generation")
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	body.ReadFrom(active.Body)
	active.Body.Close()
	if !strings.Contains(body.String(), `"active":true`) {
		t.Fatalf("active = %s, want the reply still running", body.String())
	}
	genID := regexpGenerationID(t, body.String())

	ev, err := http.Get(web.URL + "/api/generations/" + genID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	var all bytes.Buffer
	all.ReadFrom(ev.Body)
	ev.Body.Close()
	if !strings.Contains(all.String(), `"kind":"snapshot"`) || !strings.Contains(all.String(), `"kind":"end"`) || !strings.Contains(all.String(), `"status":"complete"`) {
		t.Fatalf("re-attached stream = %s", all.String())
	}
	msgs, _ := st.ListMessages(context.Background(), convID)
	if len(msgs) != 2 || msgs[1].Content != "Hello" || msgs[1].Status != "complete" {
		t.Fatalf("messages = %+v", msgs)
	}

	if r, _ := http.Get(web.URL + "/api/generations/987654/events"); r.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown generation status = %d", r.StatusCode)
	}
}

func regexpGenerationID(t *testing.T, body string) string {
	t.Helper()
	const key = `"generation_id":`
	i := strings.Index(body, key)
	if i < 0 {
		t.Fatalf("no generation id in %s", body)
	}
	rest := body[i+len(key):]
	end := strings.IndexAny(rest, ",}")
	return rest[:end]
}

func TestGenerate_RejectsBadAttachmentsAndLimits(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{ChatSSE: generateTestSSE, ModelsPath: "/v1/models", ModelsBody: `{"data":[]}`})
	defer srv.Close()
	mux, st, _, convID := newGenerateHandler(t, srv.URL)
	for _, body := range []string{
		`{"content":"hi","attachments":[{"name":"a.txt","content":"x\u0000y"}]}`,
		`{"content":"hi","max_reply_chars":5}`,
		`{"content":"hi","reasoning_limit_chars":9999999}`,
	} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/conversations/"+strconv.FormatInt(convID, 10)+"/generate", strings.NewReader(body)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d body = %s", body, rr.Code, rr.Body.String())
		}
	}
	if msgs, _ := st.ListMessages(context.Background(), convID); len(msgs) != 0 {
		t.Fatalf("rejected requests stored %d messages", len(msgs))
	}
}
