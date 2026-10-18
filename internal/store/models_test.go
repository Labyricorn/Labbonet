package store

import (
	"testing"
	"time"
)

func TestModels_UpsertAndList(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)

	ep, err := st.CreateEndpoint(ctx, "LM Studio", "openai", "http://10.138.11.11:1234", "lan", 1)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}

	m := Model{
		EndpointID:       ep.ID,
		ModelID:          "qwen3.8-flash-next",
		Type:             "chat",
		State:            "not-loaded",
		ContextLength:    8192,
		CapabilitiesJSON: `{"vision":false}`,
		SeenAt:           time.Now(),
	}
	if err := st.UpsertModel(ctx, m); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	list, err := st.ListModels(ctx, ep.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ModelID != "qwen3.8-flash-next" || list[0].ContextLength != 8192 {
		t.Fatalf("list = %+v", list)
	}

	// Upsert again with a changed state refreshes in place rather than
	// adding a second row for the same (endpoint, model) pair.
	m.State = "loaded"
	m.SeenAt = time.Now()
	if err := st.UpsertModel(ctx, m); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	list, err = st.ListModels(ctx, ep.ID)
	if err != nil {
		t.Fatalf("list after re-upsert: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) after re-upsert = %d, want 1", len(list))
	}
	if list[0].State != "loaded" {
		t.Fatalf("state after re-upsert = %q, want loaded", list[0].State)
	}
}
