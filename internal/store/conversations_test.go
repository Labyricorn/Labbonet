package store

import "testing"

func TestConversations_CreateGetListModelSwitchDelete(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)

	ep, err := st.CreateEndpoint(ctx, "Strata", "openai", "http://127.0.0.1:8080", "local", 1)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	modelA := "qwen3.8-flash-next"

	conv, err := st.CreateConversation(ctx, "Test chat", &ep.ID, &modelA, "", "{}")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if conv.Title != "Test chat" || conv.EndpointID == nil || *conv.EndpointID != ep.ID {
		t.Fatalf("created conversation = %+v", conv)
	}

	got, err := st.GetConversation(ctx, conv.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ModelID == nil || *got.ModelID != modelA {
		t.Fatalf("got conversation model = %+v", got.ModelID)
	}

	// Switching models (the Exit Scenario, A-C3) must not touch history;
	// that is exercised at the message layer (see TestMessages_...), here
	// we only check the conversation row itself updates.
	ep2, err := st.CreateEndpoint(ctx, "LM Studio", "openai", "http://10.138.11.11:1234", "lan", 1)
	if err != nil {
		t.Fatalf("create second endpoint: %v", err)
	}
	modelB := "some-lm-studio-model"
	if err := st.SetConversationModel(ctx, conv.ID, &ep2.ID, &modelB); err != nil {
		t.Fatalf("set conversation model: %v", err)
	}
	got, err = st.GetConversation(ctx, conv.ID)
	if err != nil {
		t.Fatalf("get after switch: %v", err)
	}
	if got.EndpointID == nil || *got.EndpointID != ep2.ID || got.ModelID == nil || *got.ModelID != modelB {
		t.Fatalf("conversation after switch = %+v", got)
	}

	list, err := st.ListConversations(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}

	if err := st.DeleteConversation(ctx, conv.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, err = st.ListConversations(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("len(list) after delete = %d, want 0", len(list))
	}
}

func TestConversations_WithoutModelChosenYet(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)

	conv, err := st.CreateConversation(ctx, "Untitled", nil, nil, "", "{}")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if conv.EndpointID != nil || conv.ModelID != nil {
		t.Fatalf("conversation with no model chosen = %+v", conv)
	}
}
