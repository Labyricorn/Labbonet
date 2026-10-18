package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Labyricorn/Labbonet/internal/store"
)

// Branches serves message-tree edits (edit, delete, variant switch, fork) and the
// sidebar organization endpoints (folders, tags, pins, order, search). Tree edits echo
// the conversation's revision so a stale tab gets a conflict instead of overwriting
// another tab's work.
type Branches struct {
	Store *store.Store
}

func pathInt(r *http.Request, name string) (int64, error) {
	raw := r.PathValue(name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid %s %q", name, raw)
	}
	return id, nil
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

func treeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "conversation or message not found", http.StatusNotFound)
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "The conversation changed in another tab or window; reload it before editing", http.StatusConflict)
	case errors.Is(err, store.ErrBusy):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, store.ErrInvalidMessage), errors.Is(err, store.ErrInvalidOrganization):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, "request failed", http.StatusInternalServerError)
	}
}

// messageIDs reads {id} and {mid}.
func messageIDs(w http.ResponseWriter, r *http.Request) (conv, msg int64, ok bool) {
	var err error
	if conv, err = pathInt(r, "id"); err == nil {
		msg, err = pathInt(r, "mid")
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return 0, 0, false
	}
	return conv, msg, true
}

type revisionBody struct {
	Revision int64  `json:"revision"`
	Content  string `json:"content"`
	Title    string `json:"title"`
}

// treeRevision requires a revision on every tree edit: zero would skip the check.
func treeRevision(w http.ResponseWriter, b revisionBody) bool {
	if b.Revision <= 0 {
		http.Error(w, "revision is required", http.StatusBadRequest)
		return false
	}
	return true
}

// Edit handles POST /api/conversations/{id}/messages/{mid}/edit: the edited text is
// stored as a new variant beside the original.
func (b *Branches) Edit(w http.ResponseWriter, r *http.Request) {
	conv, msg, ok := messageIDs(w, r)
	if !ok {
		return
	}
	var body revisionBody
	if !decodeBody(w, r, &body) || !treeRevision(w, body) {
		return
	}
	m, err := b.Store.EditMessage(r.Context(), conv, msg, body.Content, body.Revision)
	if err != nil {
		treeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMessageResponse(m))
}

// DeleteMessage handles DELETE /api/conversations/{id}/messages/{mid}: the message and
// its continuation are removed; sibling variants stay.
func (b *Branches) DeleteMessage(w http.ResponseWriter, r *http.Request) {
	conv, msg, ok := messageIDs(w, r)
	if !ok {
		return
	}
	var body revisionBody
	if !decodeBody(w, r, &body) || !treeRevision(w, body) {
		return
	}
	if err := b.Store.DeleteMessage(r.Context(), conv, msg, body.Revision); err != nil {
		treeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Activate handles POST /api/conversations/{id}/messages/{mid}/activate: switch the
// visible branch to this variant.
func (b *Branches) Activate(w http.ResponseWriter, r *http.Request) {
	conv, msg, ok := messageIDs(w, r)
	if !ok {
		return
	}
	var body revisionBody
	if !decodeBody(w, r, &body) || !treeRevision(w, body) {
		return
	}
	if err := b.Store.ActivateMessage(r.Context(), conv, msg, body.Revision); err != nil {
		treeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Fork handles POST /api/conversations/{id}/messages/{mid}/fork: a new conversation
// holding the chain up to and including the message.
func (b *Branches) Fork(w http.ResponseWriter, r *http.Request) {
	conv, msg, ok := messageIDs(w, r)
	if !ok {
		return
	}
	var body revisionBody
	if r.ContentLength != 0 && !decodeBody(w, r, &body) {
		return
	}
	src, err := b.Store.GetConversation(r.Context(), conv)
	if err != nil {
		treeError(w, err)
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = "Fork of " + src.Title
	}
	if len([]rune(title)) > 200 {
		title = string([]rune(title)[:200])
	}
	forked, err := b.Store.ForkConversation(r.Context(), conv, msg, title)
	if err != nil {
		treeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toConversationResponse(forked))
}

type organizeRequest struct {
	Pinned      *bool     `json:"pinned"`
	FolderID    *int64    `json:"folder_id"`
	ClearFolder bool      `json:"clear_folder"`
	Tags        *[]string `json:"tags"`
}

// Organize handles PUT /api/conversations/{id}/organize: pin, folder and tags.
func (b *Branches) Organize(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req organizeRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if err := b.Store.SetOrganization(r.Context(), id, store.Organization{Pinned: req.Pinned, FolderID: req.FolderID, ClearFolder: req.ClearFolder, Tags: req.Tags}); err != nil {
		treeError(w, err)
		return
	}
	conv, err := b.Store.GetConversation(r.Context(), id)
	if err != nil {
		treeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toConversationResponse(conv))
}

type orderRequest struct {
	IDs []int64 `json:"ids"`
}

// Order handles PUT /api/conversations/order: the complete manual order.
func (b *Branches) Order(w http.ResponseWriter, r *http.Request) {
	var req orderRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if err := b.Store.ReorderConversations(r.Context(), req.IDs); err != nil {
		treeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type searchHitResponse struct {
	ConversationID int64  `json:"conversation_id"`
	Where          string `json:"where"`
	Snippet        string `json:"snippet"`
}

// Search handles GET /api/conversations/search?q=: title, tag and message-content
// matches across every branch.
func (b *Branches) Search(w http.ResponseWriter, r *http.Request) {
	hits, err := b.Store.SearchConversations(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		treeError(w, err)
		return
	}
	out := make([]searchHitResponse, 0, len(hits))
	for _, h := range hits {
		out = append(out, searchHitResponse{ConversationID: h.ConversationID, Where: h.Where, Snippet: h.Snippet})
	}
	writeJSON(w, http.StatusOK, out)
}

type folderResponse struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

// ListFolders handles GET /api/folders.
func (b *Branches) ListFolders(w http.ResponseWriter, r *http.Request) {
	folders, err := b.Store.ListFolders(r.Context())
	if err != nil {
		treeError(w, err)
		return
	}
	out := make([]folderResponse, 0, len(folders))
	for _, f := range folders {
		out = append(out, folderResponse{ID: f.ID, Name: f.Name, Position: f.Position})
	}
	writeJSON(w, http.StatusOK, out)
}

type folderRequest struct {
	Name string `json:"name"`
}

// ReorderFolders handles PUT /api/folders/order with a complete expected order.
func (b *Branches) ReorderFolders(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Expected []int64 `json:"expected"`
		IDs      []int64 `json:"ids"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Expected == nil || req.IDs == nil {
		http.Error(w, "expected and ids are required", 400)
		return
	}
	if err := b.Store.ReorderFolders(r.Context(), req.Expected, req.IDs); err != nil {
		treeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CreateFolder handles POST /api/folders.
func (b *Branches) CreateFolder(w http.ResponseWriter, r *http.Request) {
	var req folderRequest
	if !decodeBody(w, r, &req) {
		return
	}
	f, err := b.Store.CreateFolder(r.Context(), req.Name)
	if err != nil {
		treeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, folderResponse{ID: f.ID, Name: f.Name, Position: f.Position})
}

// RenameFolder handles PATCH /api/folders/{id}.
func (b *Branches) RenameFolder(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req folderRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if err := b.Store.RenameFolder(r.Context(), id, req.Name); err != nil {
		treeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteFolder handles DELETE /api/folders/{id}; its threads become unfiled.
func (b *Branches) DeleteFolder(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := b.Store.DeleteFolder(r.Context(), id); err != nil {
		treeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
