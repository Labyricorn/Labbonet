package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Labyricorn/Labbonet/internal/cast"
	"github.com/Labyricorn/Labbonet/internal/prompt"
	"github.com/Labyricorn/Labbonet/internal/store"
)

type Cast struct{ Store *store.Store }

func castError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, store.ErrConflict):
		status = 409
	case errors.Is(err, store.ErrInvalidCast):
		status = 400
	case errors.Is(err, sql.ErrNoRows):
		status = 404
	}
	message := err.Error()
	if status == 500 {
		message = "cast storage operation failed"
	}
	http.Error(w, message, status)
}
func decodeCast(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		http.Error(w, "invalid or oversized JSON: "+err.Error(), 400)
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		http.Error(w, "expected one JSON object", 400)
		return false
	}
	return true
}
func (c *Cast) List(w http.ResponseWriter, r *http.Request) {
	cards, err := c.Store.ListCards(r.Context())
	if err != nil {
		castError(w, err)
		return
	}
	writeJSON(w, 200, cards)
}
func (c *Cast) Save(w http.ResponseWriter, r *http.Request) {
	var card cast.Card
	if !decodeCast(w, r, &card) {
		return
	}
	if r.Method == "POST" {
		card.ID = 0
		card.Revision = 0
	} else {
		id, err := parseIDParam(r)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		card.ID = id
		if card.Revision < 1 {
			http.Error(w, "revision is required", 400)
			return
		}
	}
	if err := card.Validate(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	out, err := c.Store.SaveCard(r.Context(), card)
	if err != nil {
		castError(w, err)
		return
	}
	status := 200
	if r.Method == "POST" {
		status = 201
	}
	writeJSON(w, status, out)
}
func (c *Cast) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var req struct {
		Revision int64 `json:"revision"`
	}
	if !decodeCast(w, r, &req) {
		return
	}
	if err = c.Store.DeleteCard(r.Context(), id, req.Revision); err != nil {
		castError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (c *Cast) Selection(w http.ResponseWriter, r *http.Request) {
	id := int64(0)
	if r.PathValue("id") != "" {
		var err error
		id, err = parseIDParam(r)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	}
	if r.Method == "GET" {
		sel, err := c.Store.GetCast(r.Context(), id)
		if err != nil {
			castError(w, err)
			return
		}
		writeJSON(w, 200, sel)
		return
	}
	var sel store.CastSelection
	if !decodeCast(w, r, &sel) {
		return
	}
	out, err := c.Store.SaveCast(r.Context(), id, sel)
	if err != nil {
		castError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (c *Cast) Greet(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var req struct {
		Revision int64 `json:"revision"`
	}
	if !decodeCast(w, r, &req) {
		return
	}
	sel, char, persona, err := c.Store.ReadCast(r.Context(), id)
	if err != nil {
		castError(w, err)
		return
	}
	if req.Revision != sel.Revision {
		castError(w, store.ErrConflict)
		return
	}
	text, err := prompt.Greeting(char, persona, sel.Greeting, time.Now())
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if text == "" {
		http.Error(w, "selected character has no greeting", 400)
		return
	}
	speaker, _ := json.Marshal(cast.Speaker(char))
	if err = c.Store.AddGreeting(r.Context(), id, sel.Revision, text, string(speaker)); err != nil {
		castError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (c *Cast) Reorder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind  string            `json:"kind"`
		Cards []store.CardOrder `json:"cards"`
	}
	if !decodeCast(w, r, &req) {
		return
	}
	if err := c.Store.ReorderCards(r.Context(), req.Kind, req.Cards); err != nil {
		castError(w, err)
		return
	}
	w.WriteHeader(204)
}
