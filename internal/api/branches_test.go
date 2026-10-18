package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/store"
	"github.com/Labyricorn/Labbonet/internal/store/storetest"
)

func newBranchMux(t *testing.T) (*http.ServeMux, *store.Store, int64) {
	t.Helper()
	st := storetest.New(t)
	c := &Conversations{Store: st}
	b := &Branches{Store: st}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/conversations", c.List)
	mux.HandleFunc("GET /api/conversations/search", b.Search)
	mux.HandleFunc("PUT /api/conversations/order", b.Order)
	mux.HandleFunc("GET /api/conversations/{id}", c.Get)
	mux.HandleFunc("PUT /api/conversations/{id}/organize", b.Organize)
	mux.HandleFunc("POST /api/conversations/{id}/messages/{mid}/edit", b.Edit)
	mux.HandleFunc("POST /api/conversations/{id}/messages/{mid}/activate", b.Activate)
	mux.HandleFunc("POST /api/conversations/{id}/messages/{mid}/fork", b.Fork)
	mux.HandleFunc("DELETE /api/conversations/{id}/messages/{mid}", b.DeleteMessage)
	mux.HandleFunc("GET /api/folders", b.ListFolders)
	mux.HandleFunc("POST /api/folders", b.CreateFolder)
	mux.HandleFunc("PUT /api/folders/order", b.ReorderFolders)
	mux.HandleFunc("PATCH /api/folders/{id}", b.RenameFolder)
	mux.HandleFunc("DELETE /api/folders/{id}", b.DeleteFolder)
	conv, err := st.CreateConversation(context.Background(), "T", nil, nil, "", "{}")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct{ role, text string }{{"user", "q1"}, {"assistant", "a1"}} {
		if _, err := st.CreateMessage(context.Background(), conv.ID, nil, m.role, m.text, "", "complete"); err != nil {
			t.Fatal(err)
		}
	}
	return mux, st, conv.ID
}

func TestFolderOrderAPI(t *testing.T) {
	mux, st, _ := newBranchMux(t)
	a, _ := st.CreateFolder(t.Context(), "A")
	b, _ := st.CreateFolder(t.Context(), "B")
	for _, tc := range []struct {
		body   any
		status int
	}{
		{map[string]any{"ids": []int64{a.ID, b.ID}}, 400},
		{map[string]any{"expected": []int64{a.ID, b.ID}, "ids": []int64{a.ID, a.ID}}, 400},
		{map[string]any{"expected": []int64{a.ID, b.ID}, "ids": []int64{b.ID, a.ID}}, 204},
		{map[string]any{"expected": []int64{a.ID, b.ID}, "ids": []int64{a.ID, b.ID}}, 409},
	} {
		rr := doJSON(t, mux, "PUT", "/api/folders/order", tc.body)
		if rr.Code != tc.status {
			t.Fatal(rr.Code, rr.Body.String())
		}
	}
	got, _ := st.ListFolders(t.Context())
	if got[0].ID != b.ID {
		t.Fatal("order lost")
	}
}

func detail(t *testing.T, mux *http.ServeMux, id int64) conversationDetailResponse {
	t.Helper()
	rr := doJSON(t, mux, http.MethodGet, "/api/conversations/"+strconv.FormatInt(id, 10), nil)
	var d conversationDetailResponse
	if err := json.NewDecoder(rr.Body).Decode(&d); err != nil || rr.Code != 200 {
		t.Fatal(rr.Code, err)
	}
	return d
}

func TestBranches_EditSwitchDeleteAndStaleRevision(t *testing.T) {
	mux, _, id := newBranchMux(t)
	base := "/api/conversations/" + strconv.FormatInt(id, 10)
	d := detail(t, mux, id)
	if len(d.Messages) != 2 || d.Revision == 0 {
		t.Fatalf("%+v", d)
	}
	first, rev := d.Messages[0].ID, d.Revision

	// A tree edit without a revision is refused rather than skipping the conflict check.
	rr := doJSON(t, mux, http.MethodPost, base+"/messages/"+strconv.FormatInt(first, 10)+"/edit", map[string]any{"content": "x"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing revision: %d", rr.Code)
	}
	rr = doJSON(t, mux, http.MethodPost, base+"/messages/"+strconv.FormatInt(first, 10)+"/edit", map[string]any{"content": "q1 edited", "revision": rev})
	if rr.Code != 200 {
		t.Fatalf("edit: %d %s", rr.Code, rr.Body.String())
	}
	var edited messageResponse
	_ = json.NewDecoder(rr.Body).Decode(&edited)
	if edited.Origin != "edit" || edited.OriginID == nil || *edited.OriginID != first {
		t.Fatalf("%+v", edited)
	}
	d = detail(t, mux, id)
	if len(d.Messages) != 1 || d.Messages[0].Content != "q1 edited" || len(d.Messages[0].Variants) != 2 || d.Messages[0].VariantIndex != 1 {
		t.Fatalf("%+v", d.Messages)
	}
	// The tab that still holds the old revision conflicts.
	rr = doJSON(t, mux, http.MethodPost, base+"/messages/"+strconv.FormatInt(first, 10)+"/edit", map[string]any{"content": "stale", "revision": rev})
	if rr.Code != http.StatusConflict {
		t.Fatalf("stale: %d", rr.Code)
	}
	rr = doJSON(t, mux, http.MethodDelete, base+"/messages/"+strconv.FormatInt(first, 10), map[string]any{"revision": rev})
	if rr.Code != http.StatusConflict {
		t.Fatalf("stale delete: %d", rr.Code)
	}
	// Switch back to the original variant, with its reply.
	rr = doJSON(t, mux, http.MethodPost, base+"/messages/"+strconv.FormatInt(first, 10)+"/activate", map[string]any{"revision": d.Revision})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("activate: %d", rr.Code)
	}
	d = detail(t, mux, id)
	if len(d.Messages) != 2 || d.Messages[0].Content != "q1" {
		t.Fatalf("%+v", d.Messages)
	}
	rr = doJSON(t, mux, http.MethodPost, base+"/messages/999/activate", map[string]any{"revision": d.Revision})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown message: %d", rr.Code)
	}
	rr = doJSON(t, mux, http.MethodPost, base+"/messages/abc/activate", map[string]any{"revision": d.Revision})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", rr.Code)
	}
	rr = doJSON(t, mux, http.MethodPost, base+"/messages/"+strconv.FormatInt(first, 10)+"/edit", map[string]any{"content": "  ", "revision": d.Revision})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("blank edit: %d", rr.Code)
	}
	rr = doJSON(t, mux, http.MethodDelete, base+"/messages/"+strconv.FormatInt(d.Messages[1].ID, 10), map[string]any{"revision": d.Revision})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rr.Code)
	}
	if d = detail(t, mux, id); len(d.Messages) != 1 {
		t.Fatalf("%+v", d.Messages)
	}
}

func TestBranches_ForkOrganizeOrderSearchFolders(t *testing.T) {
	mux, st, id := newBranchMux(t)
	base := "/api/conversations/" + strconv.FormatInt(id, 10)
	d := detail(t, mux, id)
	rr := doJSON(t, mux, http.MethodPost, base+"/messages/"+strconv.FormatInt(d.Messages[0].ID, 10)+"/fork", nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("fork: %d %s", rr.Code, rr.Body.String())
	}
	var forked conversationResponse
	_ = json.NewDecoder(rr.Body).Decode(&forked)
	if forked.Title != "Fork of T" || forked.ForkedFrom == nil || *forked.ForkedFrom != id {
		t.Fatalf("%+v", forked)
	}
	if fd := detail(t, mux, forked.ID); len(fd.Messages) != 1 {
		t.Fatalf("%+v", fd.Messages)
	}

	rr = doJSON(t, mux, http.MethodPost, "/api/folders", map[string]any{"name": "Work"})
	var folder folderResponse
	_ = json.NewDecoder(rr.Body).Decode(&folder)
	if rr.Code != http.StatusCreated {
		t.Fatalf("folder: %d", rr.Code)
	}
	if rr = doJSON(t, mux, http.MethodPost, "/api/folders", map[string]any{"name": "work"}); rr.Code != http.StatusBadRequest {
		t.Fatalf("duplicate folder: %d", rr.Code)
	}
	rr = doJSON(t, mux, http.MethodPut, base+"/organize", map[string]any{"pinned": true, "folder_id": folder.ID, "tags": []string{"alpha", "beta"}})
	var org conversationResponse
	_ = json.NewDecoder(rr.Body).Decode(&org)
	if rr.Code != 200 || !org.Pinned || org.FolderID == nil || len(org.Tags) != 2 {
		t.Fatalf("%d %+v", rr.Code, org)
	}
	if rr = doJSON(t, mux, http.MethodPut, base+"/organize", map[string]any{"folder_id": 4242}); rr.Code != http.StatusBadRequest {
		t.Fatalf("unknown folder: %d", rr.Code)
	}
	if rr = doJSON(t, mux, http.MethodPut, "/api/conversations/999/organize", map[string]any{"pinned": true}); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown conversation: %d", rr.Code)
	}

	// Order must list every conversation.
	if rr = doJSON(t, mux, http.MethodPut, "/api/conversations/order", map[string]any{"ids": []int64{id}}); rr.Code != http.StatusConflict {
		t.Fatalf("partial order: %d", rr.Code)
	}
	if rr = doJSON(t, mux, http.MethodPut, "/api/conversations/order", map[string]any{"ids": []int64{forked.ID, id}}); rr.Code != http.StatusNoContent {
		t.Fatalf("order: %d", rr.Code)
	}

	rr = doJSON(t, mux, http.MethodGet, "/api/conversations/search?q=a1", nil)
	var hits []searchHitResponse
	_ = json.NewDecoder(rr.Body).Decode(&hits)
	if rr.Code != 200 || len(hits) != 1 || hits[0].ConversationID != id || hits[0].Where != "message" || !strings.Contains(hits[0].Snippet, "a1") {
		t.Fatalf("%d %+v", rr.Code, hits)
	}
	if rr = doJSON(t, mux, http.MethodGet, "/api/conversations/search?q=a", nil); rr.Code != http.StatusBadRequest {
		t.Fatalf("short query: %d", rr.Code)
	}

	if rr = doJSON(t, mux, http.MethodPatch, "/api/folders/"+strconv.FormatInt(folder.ID, 10), map[string]any{"name": "Play"}); rr.Code != http.StatusNoContent {
		t.Fatalf("rename: %d", rr.Code)
	}
	if rr = doJSON(t, mux, http.MethodDelete, "/api/folders/"+strconv.FormatInt(folder.ID, 10), nil); rr.Code != http.StatusNoContent {
		t.Fatalf("delete folder: %d", rr.Code)
	}
	got, _ := st.GetConversation(context.Background(), id)
	if got.FolderID != nil || !got.Pinned {
		t.Fatalf("%+v", got)
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/folders", nil))
	if strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Fatalf("%s", rr.Body.String())
	}
}
