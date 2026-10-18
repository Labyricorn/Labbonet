# Labbonet Provider Specification

**Version:** 0.1 (2026-10-18)
**Status:** Phase 1 adapter contract (DR-005 Option A). Evidence labels: **[observed]** = seen in a live probe on 2026-10-18; **[docs]** = read in vendor documentation, not exercised; **[untested]** = needs a Phase 1 test.

---

## 1. Adapter contract

The backend talks to providers only through this interface (Go, conceptual):

```go
type Provider interface {
    Detect(ctx context.Context) (Detection, error)            // provider type hint, health
    ListModels(ctx context.Context) ([]Model, error)
    StreamChat(ctx context.Context, req ChatRequest, emit func(Event) error) (Result, error)
}

// Optional capabilities, discovered by type assertion.
type Embedder interface { Embed(ctx context.Context, req EmbedRequest) (EmbedResult, error) } // Phase 4
type StatusReporter interface { Status(ctx context.Context) (Status, error) }                 // diagnostics
```

- **Cancellation:** cancelling `ctx` aborts the upstream HTTP request (connection close). There is no separate cancel call.
- **Model identity:** always `(endpoint_id, model_id)`. The adapter records the model ID the server reports in its response, not the one requested (Strata ignores unknown IDs; `Strata_Protocol.md` section 5.2).
- **No control-plane methods.** `loadModel` and `unloadModel` from the proposal's section 8 list are not implemented in Phase 1.

### 1.1 Normalized stream events

| Event | Fields | Source |
|---|---|---|
| `start` | generation_id | Adapter, before the first upstream byte |
| `delta` | `content` or `reasoning` text | `choices[0].delta.content`, `delta.reasoning_content` |
| `usage` | prompt, completion, cached tokens | `usage` object |
| `timings` | ttft_ms, tokens_per_second | Strata `timings`, LM Studio `stats`, else client timing |
| `done` | finish_reason, model_reported | Final chunk |
| `error` | kind, message | See 1.3 |

SSE parsing rules: ignore blank lines and lines starting with `:` (keep-alives, **[observed]** on Strata); stop at `data: [DONE]`; tolerate `usage` and `timings` arriving on or after the final chunk; treat unknown fields as ignorable. A `null` content with `finish_reason: "length"` is a truncation, not an error.

### 1.2 Request mapping

`ChatRequest` maps to `POST {base}/v1/chat/completions` with `model`, `messages`, `stream: true`, `stream_options: {"include_usage": true}`, `max_tokens`, `temperature`, `top_p`, `stop`, and provider-specific extras only from an allowlist per provider (Strata: `reasoning_effort`; never `strata_mcp`). For LM Studio Labbonet never sends `ttl`, because it changes server-side model lifetime.

### 1.3 Error taxonomy

| Kind | Typical cause | UI meaning |
|---|---|---|
| `unreachable` | Connection refused, DNS failure, dial guard refusal | Endpoint offline or blocked |
| `timeout` | Connect, first-byte or idle-chunk timeout | Slow start or stalled server |
| `auth` | 401, 403 | Credential missing or wrong |
| `model_not_found` | 404 on a model | Inventory out of date |
| `no_model_loaded` | LM Studio 400 "No models loaded" **[observed]**, Strata 503 **[docs]** | Server has nothing loaded |
| `context_overflow` | Strata 400 on overflow **[docs]** | Shorten history |
| `busy` | 409 or 503 from a busy server | Retry later |
| `upstream_error` | Other 4xx/5xx | Show server message |
| `protocol_error` | Malformed SSE or JSON | Adapter bug or incompatible server |
| `cancelled` | User stop or browser disconnect | Partial text kept |

### 1.4 Timeouts (defaults, configurable per endpoint)

| Timeout | Default | Why |
|---|---|---|
| Connect | 5 s | Local or LAN hosts |
| First byte | 180 s | Strata cold load takes 1 to 3 minutes **[docs]**; LM Studio JIT load |
| Idle between chunks | 120 s | Long prefill emits keep-alives on Strata, which reset the timer |
| Total | none | Long answers are legitimate; the user can stop |

### 1.5 Fixed request paths

These are the only paths the egress client may request (`SECURITY-MODEL.md` section 5).

| Path | Method | Used for |
|---|---|---|
| `/v1/models` | GET | All OpenAI-compatible providers |
| `/v1/chat/completions` | POST | Chat |
| `/health` | GET | Strata detection and load state |
| `/status` | GET | Strata activity (diagnostics) |
| `/api/v0/models` | GET | LM Studio inventory with load state |

`/v1/embeddings` is added in Phase 4. Paths with model IDs in the URL (for example `/api/v0/models/{model}`) are not used.

## 2. Capability profile

Stored per model, filled from discovery and editable by the user.

| Field | Source |
|---|---|
| `type` (`llm`, `vlm`, `embeddings`) | LM Studio `/api/v0/models`; defaults to `llm` |
| `chat`, `streaming` | Assumed true for `llm`/`vlm`; confirmed by first successful stream |
| `embeddings` | LM Studio `type: embeddings`; Strata false **[observed]** |
| `vision` | `vlm` type or `architecture.input_modalities` |
| `tools` | LM Studio `capabilities: ["tool_use"]` |
| `reasoning_field` | `reasoning_content` when seen |
| `context_length` | Strata `meta.n_ctx`; LM Studio `max_context_length` |
| `state` | `loaded` or `not-loaded` |
| `concurrency` | Endpoint setting (Strata default 1) |

Embedding models (`type: embeddings`) are never offered in the chat model selector.

## 3. Provider detection

`POST /api/endpoints/{id}/test` runs these read-only checks and stores the hint:

1. `GET /health`: if the body has `"service": "strata"`, the endpoint is Strata.
2. Else `GET /api/v0/models`: if it returns a `data` list whose entries have `state` and `type`, the endpoint is LM Studio.
3. Else `GET /v1/models`: a generic OpenAI-compatible endpoint.

Detection only selects defaults (paths, timeouts, extras allowlist). Every endpoint still uses the one `openai` adapter.

## 4. Strata profile

Full evidence is in `Strata_Protocol.md`. Summary: `http://127.0.0.1:8080`, no key, one slot, model `qwen3.8-flash-next-unsloth-ud-iq4_xs` (32768 context), SSE with keep-alives, `usage` and `timings`, `reasoning_content`, no embeddings, CORS off. Re-checked 2026-10-18: `/health` unchanged **[observed]**.

## 5. LM Studio profile

**Host:** `http://10.138.11.11:1234` (owner-supplied, 2026-10-18). Privacy class: trusted LAN, plain HTTP.

### 5.1 Observed (read-only probes, 2026-10-18)

| Probe | Result |
|---|---|
| `GET /api/v0/models` | 200, `X-Powered-By: Express`. 28 models. Fields: `id`, `object`, `type` (`llm`/`vlm`/`embeddings`), `publisher`, `arch`, `compatibility_type` (`gguf`), `quantization`, `state`, `max_context_length`, optional `capabilities: ["tool_use"]` |
| Load state | **All 28 models `not-loaded`** at probe time |
| Embedding models present | `text-embedding-nomic-embed-text-v1.5`, `text-embedding-qwen3-embedding-0.6b`, `text-embedding-qwen3-embedding-4b`, `text-embedding-embeddinggemma-300m-qat`, `text-embedding-granite-embedding-278m-multilingual` (candidates for DR-013) |
| Authentication | Not required (`GET /v1/models` returned 200 with no token) |
| CORS | **Enabled**: `Access-Control-Allow-Origin: *`, `Allow-Headers: *`, `Allow-Methods: PUT, POST, PATCH, DELETE, GET`, for any origin including a foreign one |

Not run, on purpose: chat, streaming and embeddings probes. With nothing loaded, any such request would make LM Studio load a model (JIT, section 6), which AGENTS.md forbids for probes. These move to the Phase 1 acceptance tests, run after the owner loads a model.

A second server on `http://127.0.0.1:1234` (Express, same API shape, 28 models all `not-loaded`, no CORS headers) also answered. Its CORS preflight returned `400 "No models loaded. Please load a model in the developer page or use the 'lms load' command."` It is not the registered host; whether it is a separate local LM Studio instance is unconfirmed.

### 5.2 From LM Studio documentation [docs]

Read 2026-10-18 at `lmstudio.ai/docs/developer/...`:

- REST API v0 (LM Studio 0.3.6 or newer): `GET /api/v0/models`, `GET /api/v0/models/{model}`, `POST /api/v0/chat/completions`, `POST /api/v0/completions`, `POST /api/v0/embeddings`. The docs recommend the newer v1 REST API for new projects. Labbonet uses the OpenAI-compatible `/v1/*` paths for inference and `/api/v0/models` only for load state.
- v0 chat responses include `stats` (`tokens_per_second`, `time_to_first_token` in seconds, `generation_time`, `stop_reason`). The OpenAI-compatible stream observation is recorded in 5.3.
- **Authentication:** off by default; "Require Authentication" in Developer > Server Settings; token sent as `Authorization: Bearer <token>`.
- **Server settings:** Server Port, Serve on Local Network, Enable CORS, Just in Time Model Loading, Require Authentication.

### 5.3 Live Phase 1 stream (2026-10-17 America/Los_Angeles) [observed]

After the owner loaded `google/gemma-4-26b-a4b-qat`, `/api/v0/models` confirmed `state: loaded`, `type: vlm`, context 262144. Bounded generations used only this loaded model. Evidence: `evidence/step7-live.json`.

- `/v1/chat/completions` requires `Content-Type: application/json`; omission returned HTTP 415. The egress request builder now supplies it for JSON bodies.
- SSE includes `choices[].delta.reasoning_content`, content deltas in the full application reply, a finish chunk with the reported model, then a separate `usage` chunk with empty `choices`, and `[DONE]`.
- Observed usage keys: `prompt_tokens`, `completion_tokens`, `total_tokens`, `completion_tokens_details` (including `reasoning_tokens`). No `stats` or Strata-style `timings` object appeared in this run; this is a model/host observation, not a universal LM Studio claim.
- When provider timings are absent, the adapter measures TTFT from request start to the first nonempty content/reasoning delta and estimates tokens/sec from completion usage over the observed interval after that delta. Short or buffered streams can make that estimate noisy. Provider timings take precedence when present.
- Cancellation persisted `cancelled`; a subsequent four-token request completed in 0.331 s. No load, unload, settings or TTL operation was used.

## 6. LM Studio JIT loading and auto-evict [docs]

- JIT loading is on by default: the first request to a model that is not loaded loads it.
- JIT-loaded models get an idle TTL of 60 minutes; a request may set `ttl` (seconds).
- Auto-Evict is on by default: before JIT-loading a new model, LM Studio unloads earlier JIT-loaded models, so at most one JIT model stays in memory.

Consequence for Labbonet: sending a chat to a `not-loaded` model is a model-management action. It can take minutes and can unload a model another client is using. Labbonet therefore:

1. Shows each model's `state` in the selector.
2. Asks for explicit confirmation before the first request to a `not-loaded` model, naming the model and warning that another JIT model may be unloaded.
3. Never sends `ttl`, and never calls load or unload endpoints.
4. Refreshes the inventory after the request so the new state shows.

## 7. Open items

| Item | Needed for |
|---|---|
| LM Studio streaming format, `usage`, reasoning field, cancellation | Observed for the loaded Gemma model; section 5.3 and `evidence/step7-live.json` |
| LM Studio authenticated mode | Phase 1 acceptance (needs the owner to enable it) |
| LM Studio embeddings response shape | Phase 4 |
| Strata cancellation | Observed: `/status` idle 116 ms after stop; `evidence/step7-live.json` |
| Strata cold-load timeout | Pending owner window (deferred by owner); `Strata_Protocol.md` section 10 |
| Strata authenticated mode | Phase 1 acceptance (needs the owner to set `api_key`) |
