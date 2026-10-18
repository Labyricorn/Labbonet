package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/store/storetest"
)

// A-D1/A-D2: diagnostics shows endpoint/model/status/timings and never
// touches message content.
// Regression: the generations table has no status column of its own
// (ARCHITECTURE.md section 5) — only a message is ever marked
// "cancelled". A generation finished via Runner's explicit-cancel path
// still has an empty error_kind (cancelling is not an error), so
// deriving diagnostics status from finished_at/error_kind alone showed
// "complete" for a generation the user had actually stopped. Found
// live, through the UI, during the step 6 manual walkthrough.
func TestDiagnostics_StatusReflectsCancelledMessageNotJustErrorKind(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()

	ep, err := st.CreateEndpoint(ctx, "test", "openai", "http://127.0.0.1:8080", "local", 1)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	conv, err := st.CreateConversation(ctx, "chat", &ep.ID, nil, "", "{}")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	msg, err := st.CreateMessage(ctx, conv.ID, nil, "assistant", "partial", "", "cancelled")
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	gen, err := st.CreateGeneration(ctx, msg.ID, &ep.ID, "m", "{}")
	if err != nil {
		t.Fatalf("create generation: %v", err)
	}
	// Mirrors Runner's own cancel path: finished with no finish_reason
	// and no error_kind.
	if err := st.FinishGeneration(ctx, gen.ID, "", "{}", "{}", ""); err != nil {
		t.Fatalf("finish generation: %v", err)
	}

	d := &Diagnostics{Store: st}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/diagnostics", d.List)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics", nil)
	mux.ServeHTTP(rr, req)

	var entries []diagnosticsEntry
	if err := json.NewDecoder(rr.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 1 || entries[0].Status != "cancelled" {
		t.Fatalf("entries = %+v, want one entry with status cancelled", entries)
	}
}

func TestDiagnostics_ListNeverIncludesMessageContent(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()

	ep, err := st.CreateEndpoint(ctx, "test", "openai", "http://127.0.0.1:8080", "local", 1)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	conv, err := st.CreateConversation(ctx, "chat", &ep.ID, nil, "", "{}")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	const secretText = "this is the private conversation content, never for diagnostics"
	msg, err := st.CreateMessage(ctx, conv.ID, nil, "assistant", secretText, "", "complete")
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	gen, err := st.CreateGeneration(ctx, msg.ID, &ep.ID, "m", "{}")
	if err != nil {
		t.Fatalf("create generation: %v", err)
	}
	if err := st.FinishGeneration(ctx, gen.ID, "stop", `{"prompt_tokens":1}`, `{"tokens_per_second":5}`, ""); err != nil {
		t.Fatalf("finish generation: %v", err)
	}

	d := &Diagnostics{Store: st}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/diagnostics", d.List)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics", nil)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, secretText) {
		t.Fatalf("diagnostics response leaked message content: %s", body)
	}

	var entries []diagnosticsEntry
	if err := json.NewDecoder(rr.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.EndpointID == nil || *e.EndpointID != ep.ID || e.ModelRequested != "m" || e.Status != "complete" || e.FinishReason != "stop" {
		t.Fatalf("entry = %+v", e)
	}
	if e.DurationMillis == nil {
		t.Fatalf("entry missing duration: %+v", e)
	}
}
