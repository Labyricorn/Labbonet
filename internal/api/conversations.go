package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Labyricorn/Labbonet/internal/store"
)

// Conversations holds the dependencies for the /api/conversations
// handlers (ARCHITECTURE.md section 4.1).
type Conversations struct {
	Store *store.Store
}

type conversationResponse struct {
	ID           int64     `json:"id"`
	Title        string    `json:"title"`
	EndpointID   *int64    `json:"endpoint_id"`
	ModelID      *string   `json:"model_id"`
	SystemPrompt string    `json:"system_prompt"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func toConversationResponse(c *store.Conversation) conversationResponse {
	return conversationResponse{
		ID: c.ID, Title: c.Title, EndpointID: c.EndpointID, ModelID: c.ModelID,
		SystemPrompt: c.SystemPrompt, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

type messageResponse struct {
	ID        int64     `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Reasoning string    `json:"reasoning"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func toMessageResponse(m *store.Message) messageResponse {
	return messageResponse{ID: m.ID, Role: m.Role, Content: m.Content, Reasoning: m.Reasoning, Status: m.Status, CreatedAt: m.CreatedAt}
}

// List handles GET /api/conversations.
func (c *Conversations) List(w http.ResponseWriter, r *http.Request) {
	convs, err := c.Store.ListConversations(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]conversationResponse, 0, len(convs))
	for _, cv := range convs {
		out = append(out, toConversationResponse(cv))
	}
	writeJSON(w, http.StatusOK, out)
}

type createConversationRequest struct {
	Title        string  `json:"title"`
	EndpointID   *int64  `json:"endpoint_id"`
	ModelID      *string `json:"model_id"`
	SystemPrompt string  `json:"system_prompt"`
}

// Create handles POST /api/conversations.
func (c *Conversations) Create(w http.ResponseWriter, r *http.Request) {
	var req createConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	title := req.Title
	if title == "" {
		title = "New conversation"
	}
	conv, err := c.Store.CreateConversation(r.Context(), title, req.EndpointID, req.ModelID, req.SystemPrompt, "{}")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, toConversationResponse(conv))
}

type conversationDetailResponse struct {
	conversationResponse
	Messages []messageResponse `json:"messages"`
}

// Get handles GET /api/conversations/{id}: the conversation with its
// full message history.
func (c *Conversations) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	conv, err := c.Store.GetConversation(r.Context(), id)
	if err != nil {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}
	msgs, err := c.Store.ListMessages(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := conversationDetailResponse{conversationResponse: toConversationResponse(conv), Messages: make([]messageResponse, 0, len(msgs))}
	for _, m := range msgs {
		out.Messages = append(out.Messages, toMessageResponse(m))
	}
	writeJSON(w, http.StatusOK, out)
}

type patchConversationRequest struct {
	Title      *string `json:"title"`
	EndpointID *int64  `json:"endpoint_id"`
	ModelID    *string `json:"model_id"`
}

// Patch handles PATCH /api/conversations/{id}: rename, or switch model
// (a model switch keeps every prior message, PHASE1-PLAN.md step 5).
func (c *Conversations) Patch(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	existing, err := c.Store.GetConversation(r.Context(), id)
	if err != nil {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}

	var req patchConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Title != nil {
		if *req.Title == "" {
			http.Error(w, "title cannot be empty", http.StatusBadRequest)
			return
		}
		if err := c.Store.RenameConversation(r.Context(), id, *req.Title); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if req.EndpointID != nil || req.ModelID != nil {
		endpointID := existing.EndpointID
		modelID := existing.ModelID
		if req.EndpointID != nil {
			endpointID = req.EndpointID
		}
		if req.ModelID != nil {
			modelID = req.ModelID
		}
		if err := c.Store.SetConversationModel(r.Context(), id, endpointID, modelID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	updated, err := c.Store.GetConversation(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, toConversationResponse(updated))
}

// Delete handles DELETE /api/conversations/{id}; its messages and
// generations go with it via ON DELETE CASCADE.
func (c *Conversations) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := c.Store.DeleteConversation(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
