package openai

import (
	"context"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/egress"
	"github.com/Labyricorn/Labbonet/internal/providers/fakeprovider"
)

// A-E6/A-C8-adjacent: the Strata shape from Strata_Protocol.md section
// 5.2, including the reasoning field and context length it implies.
func TestListModels_Strata(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{
		ModelsPath: "/v1/models",
		ModelsBody: `{"object":"list","data":[{"id":"qwen3.8-flash-next-unsloth-ud-iq4_xs",
			"object":"model","status":{"value":"loaded"},"meta":{"n_ctx":32768},
			"architecture":{"input_modalities":["text"],"output_modalities":["text"]}}]}`,
	})
	defer srv.Close()

	a := New(egress.NewClient(), targetFor(t, srv.URL))
	a.profile = ProfileStrata
	models, err := a.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("len(models) = %d, want 1", len(models))
	}
	m := models[0]
	if m.ID != "qwen3.8-flash-next-unsloth-ud-iq4_xs" || m.ContextLength != 32768 ||
		m.ReasoningField != "reasoning_content" || m.State != "loaded" || m.Vision || !m.Chat || !m.Streaming || m.Embeddings {
		t.Fatalf("model = %+v", m)
	}
}

// PROVIDER-SPEC.md section 5.1: LM Studio's shape, including an
// embeddings model that must never be marked chat-capable, and
// tool_use capability.
func TestListModels_LMStudio(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{
		ModelsPath: "/api/v0/models",
		ModelsBody: `{"data":[
			{"id":"llama-3","type":"llm","state":"not-loaded","max_context_length":8192,"capabilities":["tool_use"]},
			{"id":"text-embedding-nomic","type":"embeddings","state":"not-loaded","max_context_length":2048}
		]}`,
	})
	defer srv.Close()

	a := New(egress.NewClient(), targetFor(t, srv.URL))
	a.profile = ProfileLMStudio
	models, err := a.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}

	llm := models[0]
	if llm.ID != "llama-3" || !llm.Chat || !llm.Tools || llm.Embeddings || llm.ContextLength != 8192 || llm.State != "not-loaded" {
		t.Fatalf("llm model = %+v", llm)
	}
	emb := models[1]
	if emb.ID != "text-embedding-nomic" || emb.Chat || emb.Streaming || !emb.Embeddings {
		t.Fatalf("embeddings model = %+v, want chat-incapable", emb)
	}
}

func TestListModels_Generic(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{
		ModelsPath: "/v1/models",
		ModelsBody: `{"data":[{"id":"gpt-oss"}]}`,
	})
	defer srv.Close()

	a := New(egress.NewClient(), targetFor(t, srv.URL))
	a.profile = ProfileGeneric
	models, err := a.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "gpt-oss" || !models[0].Chat {
		t.Fatalf("models = %+v", models)
	}
}

// ListModels runs Detect itself when no profile is set yet.
func TestListModels_DetectsWhenProfileUnset(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{
		HealthStatus: 200,
		HealthBody:   `{"service":"strata"}`,
		ModelsPath:   "/v1/models",
		ModelsBody:   `{"data":[{"id":"m1","status":{"value":"loaded"}}]}`,
	})
	defer srv.Close()

	a := New(egress.NewClient(), targetFor(t, srv.URL))
	models, err := a.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if a.Profile() != ProfileStrata {
		t.Fatalf("Profile() = %q, want strata (ListModels should detect first)", a.Profile())
	}
	if len(models) != 1 || models[0].ID != "m1" {
		t.Fatalf("models = %+v", models)
	}
}
