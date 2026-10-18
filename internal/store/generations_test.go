package store

import "testing"

// A-C11: provenance (endpoint, requested and reported model, params,
// start, first token, finish, usage, finish reason) is stored.
func TestGenerations_CreateFirstTokenFinishProvenance(t *testing.T) {
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
	msg, err := st.CreateMessage(ctx, conv.ID, nil, "assistant", "", "", "streaming")
	if err != nil {
		t.Fatalf("create message: %v", err)
	}

	gen, err := st.CreateGeneration(ctx, msg.ID, &ep.ID, "qwen3.8-flash-next", `{"temperature":0.7}`)
	if err != nil {
		t.Fatalf("create generation: %v", err)
	}
	if gen.FirstTokenAt != nil || gen.FinishedAt != nil {
		t.Fatalf("new generation already has first_token_at/finished_at: %+v", gen)
	}

	if err := st.RecordFirstToken(ctx, gen.ID, "qwen3.8-flash-next-reported"); err != nil {
		t.Fatalf("record first token: %v", err)
	}
	if err := st.FinishGeneration(ctx, gen.ID, "stop", `{"total_tokens":42}`, `{"tokens_per_second":12.5}`, ""); err != nil {
		t.Fatalf("finish generation: %v", err)
	}

	got, err := st.GetGeneration(ctx, gen.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ModelReported != "qwen3.8-flash-next-reported" {
		t.Fatalf("model_reported = %q", got.ModelReported)
	}
	if got.FirstTokenAt == nil || got.FinishedAt == nil {
		t.Fatalf("got generation = %+v, want first_token_at and finished_at set", got)
	}
	if got.FinishReason != "stop" || got.UsageJSON != `{"total_tokens":42}` {
		t.Fatalf("got generation = %+v", got)
	}

	list, err := st.ListGenerationsForMessage(ctx, msg.ID)
	if err != nil {
		t.Fatalf("list for message: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
}

func TestGenerations_SetModelReported(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)

	conv, err := st.CreateConversation(ctx, "Chat", nil, nil, "", "{}")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	msg, err := st.CreateMessage(ctx, conv.ID, nil, "assistant", "", "", "streaming")
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	gen, err := st.CreateGeneration(ctx, msg.ID, nil, "requested-model", "{}")
	if err != nil {
		t.Fatalf("create generation: %v", err)
	}
	if err := st.SetModelReported(ctx, gen.ID, "actually-this-model"); err != nil {
		t.Fatalf("set model reported: %v", err)
	}
	got, err := st.GetGeneration(ctx, gen.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ModelReported != "actually-this-model" {
		t.Fatalf("model_reported = %q", got.ModelReported)
	}
}

func TestListRecentGenerations(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)

	conv, err := st.CreateConversation(ctx, "Chat", nil, nil, "", "{}")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	for i := 0; i < 3; i++ {
		msg, err := st.CreateMessage(ctx, conv.ID, nil, "assistant", "", "", "streaming")
		if err != nil {
			t.Fatalf("create message: %v", err)
		}
		if _, err := st.CreateGeneration(ctx, msg.ID, nil, "m", "{}"); err != nil {
			t.Fatalf("create generation: %v", err)
		}
	}

	list, err := st.ListRecentGenerations(ctx, 2)
	if err != nil {
		t.Fatalf("list recent: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2 (limit applied)", len(list))
	}
}

// A-C8: an unknown/renamed model ID still records the reported model,
// distinct from the one requested.
func TestGenerations_ReportedModelDiffersFromRequested(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)

	conv, err := st.CreateConversation(ctx, "Chat", nil, nil, "", "{}")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	msg, err := st.CreateMessage(ctx, conv.ID, nil, "assistant", "", "", "streaming")
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	gen, err := st.CreateGeneration(ctx, msg.ID, nil, "requested-model", "{}")
	if err != nil {
		t.Fatalf("create generation: %v", err)
	}
	if err := st.RecordFirstToken(ctx, gen.ID, "actually-this-model"); err != nil {
		t.Fatalf("record first token: %v", err)
	}
	got, err := st.GetGeneration(ctx, gen.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ModelRequested != "requested-model" || got.ModelReported != "actually-this-model" {
		t.Fatalf("got = %+v", got)
	}
}
