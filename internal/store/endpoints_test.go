package store

import (
	"testing"
	"time"
)

func TestEndpoints_CreateGetListDelete(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)

	ep, err := st.CreateEndpoint(ctx, "Strata", "openai", "http://127.0.0.1:8080", "local", 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ep.ID == 0 || ep.Name != "Strata" || !ep.Enabled || ep.ConcurrencyLimit != 1 {
		t.Fatalf("created endpoint = %+v", ep)
	}

	got, err := st.GetEndpoint(ctx, ep.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.BaseURL != "http://127.0.0.1:8080" || got.PrivacyClass != "local" {
		t.Fatalf("got endpoint = %+v", got)
	}

	if _, err := st.CreateEndpoint(ctx, "LM Studio", "openai", "http://10.138.11.11:1234", "lan", 1); err != nil {
		t.Fatalf("create second: %v", err)
	}

	list, err := st.ListEndpoints(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2", len(list))
	}

	if err := st.DeleteEndpoint(ctx, ep.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, err = st.ListEndpoints(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) after delete = %d, want 1", len(list))
	}
}

func TestEndpoints_Update(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)

	ep, err := st.CreateEndpoint(ctx, "Strata", "openai", "http://127.0.0.1:8080", "local", 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := st.UpdateEndpoint(ctx, ep.ID, "Strata (renamed)", false, 3)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "Strata (renamed)" || updated.Enabled || updated.ConcurrencyLimit != 3 {
		t.Fatalf("updated endpoint = %+v", updated)
	}
	// The base URL and privacy class are not touched by Update.
	if updated.BaseURL != ep.BaseURL || updated.PrivacyClass != ep.PrivacyClass {
		t.Fatalf("UpdateEndpoint changed base_url/privacy_class: %+v", updated)
	}
}

func TestEndpoints_SetDetectedService(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)

	ep, err := st.CreateEndpoint(ctx, "Strata", "openai", "http://127.0.0.1:8080", "local", 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.SetDetectedService(ctx, ep.ID, "strata"); err != nil {
		t.Fatalf("set detected service: %v", err)
	}
	got, err := st.GetEndpoint(ctx, ep.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DetectedService != "strata" {
		t.Fatalf("DetectedService = %q, want strata", got.DetectedService)
	}
}

// A-E6: two endpoints advertising the same model ID are kept as separate
// (endpoint_id, model_id) identities, not merged.
func TestEndpoints_SameModelIDOnDifferentEndpointsStaySeparate(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)

	a, err := st.CreateEndpoint(ctx, "A", "openai", "http://127.0.0.1:8080", "local", 1)
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := st.CreateEndpoint(ctx, "B", "openai", "http://10.138.11.11:1234", "lan", 1)
	if err != nil {
		t.Fatalf("create b: %v", err)
	}

	for _, id := range []int64{a.ID, b.ID} {
		if err := st.UpsertModel(ctx, Model{EndpointID: id, ModelID: "qwen3", Type: "chat", State: "loaded", SeenAt: time.Now()}); err != nil {
			t.Fatalf("upsert model for endpoint %d: %v", id, err)
		}
	}

	aModels, err := st.ListModels(ctx, a.ID)
	if err != nil {
		t.Fatalf("list models a: %v", err)
	}
	bModels, err := st.ListModels(ctx, b.ID)
	if err != nil {
		t.Fatalf("list models b: %v", err)
	}
	if len(aModels) != 1 || len(bModels) != 1 {
		t.Fatalf("expected one model per endpoint, got a=%d b=%d", len(aModels), len(bModels))
	}
}
