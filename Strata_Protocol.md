# Strata Protocol Specification

**Project:** Secure Multi-Provider AI Chat Interface (Labbonet)
**Document:** `Strata_Protocol.md` (evidence for DR-005; feeds the provider adapter spec)
**Version:** 0.1
**Verified:** 2026-10-18
**Status:** Verified against public docs and a live local instance. DR-005 Option A accepted by the owner on 2026-10-18; the local instance is the confirmed Strata host.

---

## 1. Summary

- **Strata is an OpenAI-compatible inference server.** The standard OpenAI adapter is sufficient. No dedicated Strata adapter is needed for chat.
- **Source:** https://github.com/Niko1221/Strata (MIT license, Python server in `serve/`, native engine in `src/`). Docs read: `README.md`, `docs/DETAILS.md` (first ~200k of 216k chars), `docs/MODELS.md`, `SECURITY.md`, and the `serve/` file listing.
- **Live instance:** `http://127.0.0.1:8080` on this machine answered as `"service": "strata"` (Python `BaseHTTP/0.6`, no API key set).
- **The model is not "Qwen 111B"** (the owner confirmed this instance is the intended host, so the brief's label is superseded). The instance serves `qwen3.8-flash-next-unsloth-ud-iq4_xs`, context 32768. Strata's docs never give a parameter count. The "111" in the docs is a download size (UD-Q4_K_XL is about 111 GB). The brief's "Qwen 111B" label should be corrected (see section 9).
- **Gaps for the adapter:** no `/v1/embeddings`, no documented `stream_options`, and no CORS by default. The last one means a backend proxy is required (supports DR-002 Option B).

## 2. Verification method

| Source | Evidence |
|---|---|
| Docs | Strata README, DETAILS, MODELS, SECURITY (fetched 2026-10-18) |
| Live, read-only | `GET /health`, `/v1/models`, `/props`, `/status`, `/slots` |
| Live, tiny generations | Streaming `POST /v1/chat/completions` (`max_tokens` 12); non-streaming with an unknown model (`max_tokens` 1); `POST /v1/embeddings`; foreign-`Origin` POST; CORS preflight |

Items marked **[docs only]** below were not exercised live. Everything else was observed.

## 3. Connection

| Property | Value |
|---|---|
| Base URL (OpenAI clients) | `http://127.0.0.1:8080/v1` |
| Root URL (health, metadata) | `http://127.0.0.1:8080` |
| Default port | 8080 (`--port` changes it) |
| Default bind | `127.0.0.1`. LAN needs `--host 0.0.0.0` and an API key. |
| Transport | Plain HTTP/1.0 (`Connection: close`). No TLS. |
| Concurrency | One request at a time by default (`total_slots: 1`). `--batch` or `"parallel"` adds slots. |
| Authentication | None on this instance (`"api_key": false`). When set: bearer token. |

The owner confirmed this local instance is the Strata host. The adapter must still take the base URL from endpoint config, never hardcode it.

## 4. Endpoints

| Purpose | Endpoint | Observed |
|---|---|---|
| Chat completions (stream, non-stream, tools) | `POST /v1/chat/completions` | Yes |
| Anthropic Messages | `POST /v1/messages` | [docs only] |
| OpenAI Responses (stateless) | `POST /v1/responses` | [docs only] |
| Model list | `GET /v1/models`, `GET /models` | Yes |
| Health | `GET /health` | Yes |
| Model properties, chat template | `GET /props` (`?model=<id>`) | Yes |
| Activity | `GET /status`, `GET /slots` | Yes |
| Save or restore a conversation | `POST /slots/0?action=save\|restore` | [docs only] |
| Metrics | `GET /metrics`, `GET /metrics/prometheus` | [docs only] |
| MCP tool servers | `GET /mcp` | [docs only] |
| Unload or load model | `POST /unload`, `POST /load` | [docs only] |
| VRAM tuning | `POST /v1/vram` (opt-in) | [docs only] |
| Embeddings | `POST /v1/embeddings` | **404 `{"error":{"message":"not found"}}`** |

Labbonet must not call `POST /unload`, `POST /load`, `POST /v1/vram`, `/mcp` or `/settings`. They are control-plane operations and the brief forbids managing other services' models without an explicit user command.

## 5. Observed responses

### 5.1 Health (`GET /health`)

```json
{"status": "ok", "max_context": 32768, "model": "qwen3.8-flash-next-unsloth-ud-iq4_xs",
 "images": false, "api_key": false, "loaded": true, "service": "strata"}
```

`"service": "strata"` identifies Strata. Use it to detect the provider type during connection tests.

### 5.2 Model list (`GET /v1/models`)

```json
{"object": "list", "data": [{
  "id": "qwen3.8-flash-next-unsloth-ud-iq4_xs", "object": "model",
  "status": {"value": "loaded"}, "meta": {"n_ctx": 32768},
  "architecture": {"input_modalities": ["text"], "output_modalities": ["text"]}}]}
```

- Only the **loaded** model is listed. Docs: an unknown `model` returns 404 on metadata paths.
- **Chat does not enforce the `model` field.** A chat request with `"model": "nope"` returned HTTP 200 and was answered by the loaded model. The adapter must record the model ID the server reports in the response (`model` field), not the one it requested.
- Capability data available from the list: `meta.n_ctx` (context), `architecture.input_modalities` (text only here, images when enabled at setup).

### 5.3 Streaming chat (`POST /v1/chat/completions`, `"stream": true`)

- `Content-Type: text/event-stream`, with `X-Accel-Buffering: no`.
- Standard OpenAI chunks: `data: {"object":"chat.completion.chunk", ...}`. The first chunk carries `delta.role` and empty `content`.
- **Keep-alives** arrive as SSE comment lines (`: keep-alive`) during prompt reads. The parser must ignore lines starting with `:`.
- The final chunk has `finish_reason: "stop"`, then a `usage` object, then `data: [DONE]`.
- `usage` appeared in the stream **without** requesting it (`stream_options.include_usage` was sent in the probe, so whether it is needed is untested).
- A non-OpenAI **`timings`** object comes with the final chunk:

```json
"usage": {"prompt_tokens": 18, "completion_tokens": 4, "total_tokens": 22,
          "prompt_tokens_details": {"cached_tokens": 0}},
"timings": {"prompt_n": 18, "prompt_ms": 630.1, "prompt_per_second": 28.6,
            "predicted_n": 4, "predicted_ms": 393.9, "predicted_per_second": 10.2,
            "cache_n": 0, "draft_n": 0, "draft_n_accepted": 0}
```

`timings` gives time-to-first-token and tokens per second for the diagnostics panel without client-side measurement. It is optional. Fall back to client timing when absent.

### 5.4 Non-streaming and reasoning

- Response is a standard `chat.completion` with `choices[0].message` and `usage`.
- Reasoning arrives as **`reasoning_content`** (separate from `content`). In the non-streaming probe, `content` was `null` and `reasoning_content` held the text because `max_tokens` was exhausted during thinking.
- `finish_reason: "length"` means truncation. The adapter must not treat a null `content` as an error.

## 6. Request parameters

Observed to work: `model`, `messages`, `stream`, `max_tokens`, `reasoning_effort` (`"none"` suppressed thinking), `stream_options`.

**[docs only]** Strata-specific or notable parameters:

| Parameter | Meaning |
|---|---|
| `reasoning_effort` | `none`, `low`, `medium`, `high` (default `high`). Also `reasoning: {effort}` and `chat_template_kwargs: {enable_thinking: false}`. |
| `reasoning_budget_tokens` | Caps thinking tokens. `0` means no cap. |
| `answer_room` | Default true. Reserves room for the answer when `max_tokens` is 1024 or more. |
| `fit_max_tokens` | Shrinks `max_tokens` to fit the context instead of returning 400. |
| `return_progress` | Adds `prompt_progress` to empty-delta chunks (prefill progress). |
| `strata_checkpoint`, `strata_prefix` | Prompt-cache hints. Optional. |
| `repeat_stop_tokens` | Stops repeated-token loops. |
| `strata_mcp` | Opts an API client into Strata's own MCP tools. **Labbonet must never send this.** |

Structured output: Responses API `json_schema`/`json_object` are "checked, not constrained". The chat-path behavior was not read. Treat constrained JSON as unsupported until tested.

Tool calling is supported on all three chat APIs, but the model cannot be forced to call a tool (`tool_choice` other than `none` lets the model choose).

## 7. Limits, errors and cancellation

- **Context overflow** is refused with **HTTP 400**, not truncated. `max_tokens` that overruns the context is also refused unless `fit_max_tokens` is set. The adapter should use `meta.n_ctx` to budget before sending.
- **Cancellation:** close the connection. Docs say generation stops and the next request starts at once. Not tested live. The adapter's `cancelGeneration()` maps to aborting the fetch.
- **Busy:** requests queue behind the single slot. `/status` returns `{busy, queued, phase, prompt_tokens, generated, max_tokens}` and can feed the diagnostics panel. Observed at probe time: `busy: false`, with `prompt_tokens: 6655` from an earlier client, so another client shares this server.
- **Error shape** (observed): `{"error": {"type": "forbidden", "message": "..."}}`. Other statuses from docs: 404 unknown model on metadata paths, 409 unload while busy, 503 model not loaded or insufficient RAM.
- **Slow start:** first model load takes 1 to 3 minutes. Connection tests need a long timeout, or should call `/health` and read `loaded`.
- **Usage / token counts:** `prompt_tokens` and `completion_tokens` are returned. No tokenizer endpoint was found, so pre-send counting must be estimated (the proposal's section 13.1 caveat applies).

## 8. Security behavior (relevant to DR-012 and DR-010)

Observed on the live instance (no API key set):

| Test | Result |
|---|---|
| POST with `Origin: https://evil.example` | **403** `forbidden`: "web pages of ... may not use this server without an API key" |
| CORS preflight from `http://localhost:3000` | 204 with **no `Access-Control-Allow-*` headers** |

Meaning:
- **A browser page cannot call Strata directly.** Preflight succeeds but without CORS headers, so the browser blocks the request. Labbonet must reach Strata through its Go backend (requests without an `Origin` header are not affected). This confirms DR-002 Option B for Strata. A standalone HTML proof of concept would need `cors_origins` set in Strata's config.
- Docs: without an API key, requests whose `Host` is not a known name get 403. A key turns the host check off and governs access.
- Docs: key set via `"api_key"` in `strata-<model>.json` or `STRATA_API_KEY`. The client sends `Authorization: Bearer <key>`. Several keys are allowed. Metadata endpoints and `/slots` also require the key.
- No TLS. A LAN deployment sends the key and conversations in clear text, so a Strata host outside the loopback interface should be classified "trusted LAN" at most, and reached over a trusted network or tunnel.
- The Strata project states it has had no external security audit.

## 9. Impact on Labbonet documents

1. **Provider type:** register Strata as `openai-compatible` with a Strata detection hint (`/health` returns `service: "strata"`). This is DR-005 Option A.
2. **Model label:** the proposal calls the model "Qwen 111B". The server reports `qwen3.8-flash-next-unsloth-ud-iq4_xs`. The adapter should display the server-reported ID. The project owner should confirm whether the intended host is a different Strata instance (for example a LAN machine with a larger model) than the local one probed here.
3. **Capability profile for Strata:** chat yes; streaming yes; reasoning field `reasoning_content`; tools yes; vision only if enabled at setup; **embeddings no**; context from `/v1/models` `meta.n_ctx`; concurrency 1.
4. **RAG (DR-013):** Strata cannot be the embedding provider. Use another endpoint (LM Studio, Ollama, or a dedicated embedder).
5. **Summarization workflows (DR-009):** with concurrency 1, a summarization job sent to the same Strata instance queues behind chat. This supports the brief's plan of sending summaries to a separate endpoint.
6. **Provenance (section 10 of the proposal):** `timings` and `usage` give response duration and token usage when present.

## 10. Adapter test checklist (DR-005 acceptance criteria)

| Criterion | Status |
|---|---|
| Software identity confirmed | Done (Strata, MIT, Niko1221/Strata) |
| Observed protocol recorded | Done (this document) |
| URL and auth recorded | Done (`http://127.0.0.1:8080`, no key) |
| Model discovery tested | Done (`/v1/models`) |
| Streaming tested | Done (SSE, usage, timings) |
| Cancellation tested | Not done. Needs a test that aborts a long generation and checks `/status`. |
| Timeout behavior tested | Not done. Needs a cold-load test. |
| Strata model accessible through the app | Pending app implementation. Served model is Qwen3.8-Flash-Next ("Qwen 111B" dropped). |
| Authenticated mode tested | Not done. Needs a Strata instance with `api_key` set. |
