package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/store/storetest"
)

func newConversationsHandler(t *testing.T) (*http.ServeMux, *Conversations) {
	t.Helper()
	st := storetest.New(t)
	c := &Conversations{Store: st}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/conversations", c.List)
	mux.HandleFunc("POST /api/conversations", c.Create)
	mux.HandleFunc("GET /api/conversations/{id}", c.Get)
	mux.HandleFunc("PATCH /api/conversations/{id}", c.Patch)
	mux.HandleFunc("DELETE /api/conversations/{id}", c.Delete)
	return mux, c
}

func TestConversations_CreateGetPatchDelete(t *testing.T) {
	mux, _ := newConversationsHandler(t)

	rr := doJSON(t, mux, http.MethodPost, "/api/conversations", createConversationRequest{Title: "Test chat"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var created conversationResponse
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Title != "Test chat" || created.EndpointID != nil {
		t.Fatalf("created = %+v", created)
	}

	rr = doJSON(t, mux, http.MethodGet, "/api/conversations", nil)
	var list []conversationResponse
	if err := json.NewDecoder(rr.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}

	id := strconv.FormatInt(created.ID, 10)
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/conversations/"+id, nil)
	mux.ServeHTTP(rr, req)
	var detail conversationDetailResponse
	if err := json.NewDecoder(rr.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if len(detail.Messages) != 0 {
		t.Fatalf("new conversation has messages: %+v", detail.Messages)
	}

	newTitle := "Renamed"
	rr = doJSON(t, mux, http.MethodPatch, "/api/conversations/"+id, patchConversationRequest{Title: &newTitle})
	var patched conversationResponse
	if err := json.NewDecoder(rr.Body).Decode(&patched); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if patched.Title != "Renamed" {
		t.Fatalf("patched title = %q", patched.Title)
	}

	rr = doJSON(t, mux, http.MethodDelete, "/api/conversations/"+id, nil)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/conversations/"+id, nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get after delete status = %d, want 404", rr.Code)
	}
}

// A model switch mid-conversation must keep every earlier message.
func TestConversations_ModelSwitchKeepsHistory(t *testing.T) {
	mux, c := newConversationsHandler(t)
	ctx := context.Background()

	ep1, err := c.Store.CreateEndpoint(ctx, "A", "openai", "http://127.0.0.1:8080", "local", 1)
	if err != nil {
		t.Fatalf("create endpoint a: %v", err)
	}
	conv, err := c.Store.CreateConversation(ctx, "chat", &ep1.ID, nil, "", "{}")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, err := c.Store.CreateMessage(ctx, conv.ID, nil, "user", "hello", "", "complete"); err != nil {
		t.Fatalf("create message: %v", err)
	}
	if _, err := c.Store.CreateMessage(ctx, conv.ID, nil, "assistant", "hi there", "", "complete"); err != nil {
		t.Fatalf("create message: %v", err)
	}

	ep2, err := c.Store.CreateEndpoint(ctx, "B", "openai", "http://10.138.11.11:1234", "lan", 1)
	if err != nil {
		t.Fatalf("create endpoint b: %v", err)
	}
	modelB := "model-b"
	id := strconv.FormatInt(conv.ID, 10)
	rr := doJSON(t, mux, http.MethodPatch, "/api/conversations/"+id, patchConversationRequest{EndpointID: &ep2.ID, ModelID: &modelB})
	var patched conversationResponse
	if err := json.NewDecoder(rr.Body).Decode(&patched); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if patched.EndpointID == nil || *patched.EndpointID != ep2.ID || patched.ModelID == nil || *patched.ModelID != modelB {
		t.Fatalf("patched = %+v", patched)
	}

	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/conversations/"+id, nil)
	mux.ServeHTTP(rr, req)
	var detail conversationDetailResponse
	if err := json.NewDecoder(rr.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if len(detail.Messages) != 2 {
		t.Fatalf("messages after model switch = %+v, want 2 kept", detail.Messages)
	}
}
