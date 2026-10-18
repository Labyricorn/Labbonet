package store

import "testing"

func TestMessages_CreateGetListAndModelSwitchKeepsHistory(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)

	ep, err := st.CreateEndpoint(ctx, "Strata", "openai", "http://127.0.0.1:8080", "local", 1)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	conv, err := st.CreateConversation(ctx, "Chat", &ep.ID, nil, "", "{}")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	m1, err := st.CreateMessage(ctx, conv.ID, nil, "user", "hello", "", "complete")
	if err != nil {
		t.Fatalf("create message 1: %v", err)
	}
	m2, err := st.CreateMessage(ctx, conv.ID, &m1.ID, "assistant", "hi there", "thinking...", "streaming")
	if err != nil {
		t.Fatalf("create message 2: %v", err)
	}
	if m2.ParentID == nil || *m2.ParentID != m1.ID {
		t.Fatalf("message 2 parent = %+v, want %d", m2.ParentID, m1.ID)
	}

	if err := st.UpdateMessageContent(ctx, m2.ID, "hi there!", "thought about it", "complete"); err != nil {
		t.Fatalf("update content: %v", err)
	}

	// Switching the conversation's model must not touch earlier messages
	// (the Exit Scenario, A-C3).
	ep2, err := st.CreateEndpoint(ctx, "LM Studio", "openai", "http://10.138.11.11:1234", "lan", 1)
	if err != nil {
		t.Fatalf("create second endpoint: %v", err)
	}
	if err := st.SetConversationModel(ctx, conv.ID, &ep2.ID, nil); err != nil {
		t.Fatalf("switch model: %v", err)
	}

	msgs, err := st.ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if msgs[0].Content != "hello" || msgs[1].Content != "hi there!" || msgs[1].Status != "complete" {
		t.Fatalf("msgs = %+v", msgs)
	}
}

func TestMessages_DeletedWithConversation(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)

	conv, err := st.CreateConversation(ctx, "Chat", nil, nil, "", "{}")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, err := st.CreateMessage(ctx, conv.ID, nil, "user", "hello", "", "complete"); err != nil {
		t.Fatalf("create message: %v", err)
	}

	if err := st.DeleteConversation(ctx, conv.ID); err != nil {
		t.Fatalf("delete conversation: %v", err)
	}

	msgs, err := st.ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("len(msgs) after conversation delete = %d, want 0", len(msgs))
	}
}
