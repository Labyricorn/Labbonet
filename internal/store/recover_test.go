package store_test

import (
	"context"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/store"
	"github.com/Labyricorn/Labbonet/internal/store/storetest"
)

// A reply left "streaming" by a killed process keeps its flushed text, becomes
// "interrupted", and no new turn is created.
func TestRecoverInterruptedMarksStreamingRepliesAndKeepsText(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	ep, err := st.CreateEndpoint(ctx, "e", "openai", "http://127.0.0.1:1", "local", 1)
	if err != nil {
		t.Fatal(err)
	}
	model := "m"
	conv, err := st.CreateConversation(ctx, "c", &ep.ID, &model, "", "{}")
	if err != nil {
		t.Fatal(err)
	}
	sel, _, _, err := st.ReadCast(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	user := "hello"
	msgID, genID, err := st.StartTurn(ctx, store.Turn{ConversationID: conv.ID, EndpointID: ep.ID, Model: model, User: &user, UserSpeaker: "{}", AssistantSpeaker: "{}", Params: "{}", CastRevision: sel.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateMessageContent(ctx, msgID, "partial", "", "streaming"); err != nil {
		t.Fatal(err)
	}
	if g, m, err := st.StreamingGeneration(ctx, conv.ID); err != nil || g != genID || m != msgID {
		t.Fatalf("streaming generation = %d %d %v", g, m, err)
	}

	n, err := st.RecoverInterrupted(ctx)
	if err != nil || n != 1 {
		t.Fatalf("recovered %d, %v", n, err)
	}
	msgs, _ := st.ListMessages(ctx, conv.ID)
	if len(msgs) != 2 || msgs[1].Status != "interrupted" || msgs[1].Content != "partial" {
		t.Fatalf("messages = %+v", msgs)
	}
	gen, _ := st.GetGeneration(ctx, genID)
	if gen.FinishedAt == nil || gen.ErrorKind != "server_restart" {
		t.Fatalf("generation = %+v", gen)
	}
	if _, _, err := st.StreamingGeneration(ctx, conv.ID); err == nil {
		t.Fatal("recovered conversation still reports a streaming generation")
	}
	if n, _ := st.RecoverInterrupted(ctx); n != 0 {
		t.Fatalf("second recovery changed %d rows", n)
	}
}
