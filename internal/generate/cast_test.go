package generate

import (
	"encoding/json"
	"errors"
	"github.com/Labyricorn/Labbonet/internal/cast"
	"github.com/Labyricorn/Labbonet/internal/prompt"
	"github.com/Labyricorn/Labbonet/internal/providers"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunnerFreezesCastPromptAndParametersBeforeQueue(t *testing.T) {
	var received providers.ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[{"id":"m"}]}`))
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(runnerTestSSE))
	}))
	defer srv.Close()
	rn, st := newRunner(t)
	ep, conv := mustEndpointAndConversation(t, st, srv.URL, "m")
	ctx := t.Context()
	temp := 0.4
	character, err := st.SaveCard(ctx, cast.Card{Kind: "character", Name: "Archivist", Description: "{{char}} greets {{user}} {{a}}", Macros: map[string]string{"a": "resolved"}, ReplyLimit: 20, Temperature: &temp, Carousel: []string{"rotate"}})
	if err != nil {
		t.Fatal(err)
	}
	persona, err := st.SaveCard(ctx, cast.Card{Kind: "persona", Name: "Reader", Description: "curious", PersonaFrequency: 1})
	if err != nil {
		t.Fatal(err)
	}
	sel, _ := st.GetCast(ctx, conv.ID)
	sel.CharacterID = &character.ID
	sel.PersonaID = &persona.ID
	if _, err = st.SaveCast(ctx, conv.ID, sel); err != nil {
		t.Fatal(err)
	}
	release, err := rn.Slots.Acquire(ctx, ep.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	err = rn.Run(ctx, Request{ConversationID: conv.ID, UserContent: "hello"}, func(_ int64) {
		character.Name = "Changed while queued"
		if _, err := st.SaveCard(ctx, *character); err != nil {
			t.Error(err)
		}
		release()
	}, func(providers.Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	// ChatRequest has no JSON tags; decode provider's wire shape separately below.
	msgs, _ := st.ListMessages(ctx, conv.ID)
	if len(msgs) != 2 {
		t.Fatal(msgs)
	}
	if !strings.Contains(msgs[1].SpeakerJSON, `"name":"Archivist"`) {
		t.Fatal(msgs[1].SpeakerJSON)
	}
	gens, _ := st.ListGenerationsForMessage(ctx, msgs[1].ID)
	var params struct{ Prompt prompt.Snapshot }
	if err = json.Unmarshal([]byte(gens[0].ParamsJSON), &params); err != nil {
		t.Fatal(err)
	}
	if params.Prompt.MaxTokens != 20 || *params.Prompt.Temperature != temp || !strings.Contains(params.Prompt.Messages[0].Content, "Archivist greets Reader resolved") {
		t.Fatal(params)
	}
	if len(received.Messages) == 0 || !strings.Contains(received.Messages[0].Content, "Archivist greets Reader resolved") {
		t.Fatal(received)
	}
}

func TestRunnerInvalidPromptPersistsNothingAndImmediateCancelWorks(t *testing.T) {
	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, "http://127.0.0.1:1", "m")
	err := rn.Run(t.Context(), Request{ConversationID: conv.ID, UserContent: strings.Repeat("x", 9000)}, nil, func(providers.Event) error { return nil })
	if !errors.Is(err, ErrPrompt) {
		t.Fatal(err)
	}
	msgs, _ := st.ListMessages(t.Context(), conv.ID)
	if len(msgs) != 0 {
		t.Fatal(msgs)
	}
	err = rn.Run(t.Context(), Request{ConversationID: conv.ID, UserContent: "hi"}, func(id int64) {
		if !rn.Registry.Cancel(id) {
			t.Error("cancel not registered at onStarted")
		}
	}, func(providers.Event) error { return nil })
	if err == nil {
		t.Fatal("cancelled generation succeeded")
	}
	msgs, _ = st.ListMessages(t.Context(), conv.ID)
	if len(msgs) != 2 || msgs[1].Status != "cancelled" {
		t.Fatal(msgs)
	}
}
