# Project Brief: Secure, Multi-Provider Local AI Chat Interface

**Name:** Labbonet (DR-001, accepted 2026-10-18); **License:** GNU AGPLv3-or-later (DR-004)  
**Project status:** Planning complete (Gate 1 cleared 2026-10-18); Phase 1 prerequisites in `docs/`  
**Document version:** 0.1  
**Date:** October 17, 2026  
**Reference implementation:** [GobboNet by ElodineOfficial](https://github.com/ElodineOfficial/GobboNet)

---

# 1. Executive Summary

We intend to develop a new, locally controlled AI chat application using the open-source **GobboNet** project as our principal functional and user-experience reference.

The application will preserve and improve GobboNet's most useful features, particularly conversational AI, character cards, personas, lorebooks, retrieval-augmented generation (RAG), conversation memory, and history management.

However, rather than reproducing GobboNet's approach to installing and managing local inference engines, our application will be built around a **unified multi-provider inference architecture**.

It will connect to existing inference services, including:

- **Strata**, our existing local inference host (serves Qwen3.8-Flash-Next; see `Strata_Protocol.md`).
- **LM Studio**, providing access to locally hosted models.
- **Ollama**, through its native or compatible API.
- **llama.cpp**, through its HTTP inference server.
- **WebLLM**, providing GPU-accelerated inference directly inside the browser.
- Other OpenAI-compatible endpoints, including local AI gateways such as OmniRoute where compatible.

The application will treat these as interchangeable inference providers while recognizing that they differ in capabilities, authentication, hardware usage, and operational behavior.

An important objective is to move beyond GobboNet's primarily single-model workflow. Our system will support **multiple AI models performing different roles**, potentially running concurrently across independent endpoints.

For example, the Strata model could handle a primary conversation while a smaller WebLLM model performs classification, a dedicated embedding model supports semantic retrieval, and another model handles background memory consolidation.

Security is a central design requirement. We are undertaking this rebuild partly because our review of GobboNet identified unnecessary system-level permissions, potentially destructive behavior, permissive networking, and unsandboxed JavaScript extension capabilities.

We do not have evidence establishing that GobboNet is malware. Our goal is not to make that accusation. Instead, we intend to design a comparable application with a much smaller and more comprehensible attack surface.

**The overarching objective is a secure, extensible, model-independent AI interface that gives the user explicit control over where inference runs, how models cooperate, what data is transmitted, and how conversations and memory are retained.**

---

# 2. Origin of the Project

## 2.1 GobboNet repository evaluation

The project began with a security examination of:

https://github.com/ElodineOfficial/GobboNet

We examined its publicly available source, architecture, installer behavior, model selection, inference workflows, networking, and RAG implementation.

The static examination did not establish the presence of malicious software, but it revealed several security and reliability concerns.

These included:

1. An antivirus detection reported against an older Windows installer, identified as `Trojan:Win32/Wacatac.B!ml`.
2. Documentation and installer guidance recommending Microsoft Defender exclusions.
3. PowerShell execution and scripts capable of downloading and launching executable components.
4. Installation and network setup routines that can modify Windows Firewall behavior.
5. Network configuration permitting listening on all interfaces, including `0.0.0.0`.
6. LAN authentication over unencrypted HTTP.
7. An uninstall path that could recursively delete the directory containing a custom configuration file, potentially affecting unrelated files.
8. Mechanisms for loading arbitrary inline or remote JavaScript into the application.
9. Additional character-card JavaScript hooks running without isolation.
10. Reliance on separately downloaded executables and model assets, with verification practices varying between components.
11. Release binaries that had not been independently verified against the published source during our examination.

These observations motivated the question:

**Could we rebuild an application with GobboNet's functionality while eliminating the concerning installation, execution, networking, and extension behaviors?**

Our answer was yes.

## 2.2 Shift in design philosophy

GobboNet is designed partly as an accessible way for someone to install and operate local AI without already knowing how to manage inference engines.

Our intended application has a different starting point.

We already have substantial local AI infrastructure and do not need another application to install llama.cpp, maintain GGUF inventories, download large models, or manage GPU resources automatically.

Consequently, our application should be **an inference client and workflow orchestrator, not an inference-engine installer**.

This distinction simplifies the architecture and avoids several categories of security concern.

---

# 3. Relationship to GobboNet

## 3.1 GobboNet as the design model

GobboNet will serve as the principal reference for:

- Overall chat workflow and interface organization.
- Character card creation, selection, and editing.
- Character personalities, prompts, and alternate greetings.
- User personas.
- Lorebook authoring and retrieval.
- RAG integration.
- Conversation history and long-term memory.
- Conversation summarization.
- Thread management, branching, and rerolling.
- Streaming text and model reasoning presentation.
- Generation controls.
- Import and export functionality.
- Scheduling, macros, and optional web search.
- General settings, diagnostics, and usability.

We do not need to reproduce its interface pixel-for-pixel or preserve its existing code architecture.

The intention is to use GobboNet as a **functional benchmark** and improve upon it where appropriate.

## 3.2 Existing model catalogue

GobboNet offers a selection of GGUF models including Llama, Gemma, Mistral, Qwen, DeepSeek, gpt-oss, and Command R.

Its catalogue is intended to help users choose and download models suitable for their hardware.

Our application will not require its own fixed model catalogue.

Instead, the primary model inventory should be discovered from registered inference providers.

For example, LM Studio could advertise whichever models it currently exposes, while Strata could advertise its loaded model.

WebLLM would maintain a separate catalogue of browser-compatible models.

A future optional model-download manager might be useful, but it is not a requirement for the initial application.

## 3.3 License considerations

GobboNet is licensed under **GNU AGPLv3-or-later**.

We therefore have two implementation choices.

**Approach A: Fork or reuse code.** We can adapt the original software, but must comply with the applicable AGPL obligations, including source-code availability and preservation of notices where required.

**Approach B: Independent implementation.** We can study GobboNet's functionality and independently implement our own equivalent features and interface. This provides greater freedom in choosing the new application's architecture and license, provided we do not copy protected material.

**Superseded 2026-10-18 (DR-003 accepted as Option B, selective reuse):** the text below records the original preference. Independent implementation, using GobboNet as a design and behavioral reference, while considering selective code reuse only where the benefit justifies the licensing and maintenance implications.

**Resolved 2026-10-18 (DR-004):** Labbonet is licensed GNU AGPLv3-or-later.

---

# 4. Core Project Goals

## 4.1 Functional goals

The application should:

1. Provide a polished, responsive, browser-based AI chat interface.
2. Connect to multiple user-configured inference endpoints.
3. Support in-browser inference through WebLLM.
4. Discover available models and their capabilities.
5. Permit model selection at the conversation and character levels.
6. Support different models for different background tasks.
7. Preserve extensive conversation history.
8. Support configurable character cards and lorebooks.
9. Implement hybrid semantic and rule-based retrieval.
10. Provide persistent conversation memory and summarization.
11. Support simultaneous inference across multiple endpoints where appropriate.
12. Permit comparison of responses from multiple models.
13. Keep inference providers independent of the application.
14. Operate locally without requiring a cloud account, subscription, or central service.
15. Remain extensible without depending on arbitrary JavaScript execution.

## 4.2 Security goals

The application should:

- Run under normal user permissions.
- Avoid automatic executable downloads.
- Avoid PowerShell dependencies and execution-policy bypasses.
- Never modify firewall rules automatically.
- Never terminate unrelated inference processes.
- Avoid deleting files or folders it does not explicitly own.
- Listen only on the loopback interface by default.
- Require explicit configuration for LAN or remote access.
- Protect authentication credentials and API keys.
- Restrict outgoing connections to approved endpoints.
- Make cross-endpoint transmission of conversation data visible and controllable.
- Treat model-generated content, imported character cards, retrieved documents, and extensions as untrusted inputs.
- Provide reliable data export and recovery.
- Maintain a small, auditable dependency and execution footprint.

The goal is to make the system secure by architecture rather than relying on users to trust an installer or disable protective software.

---

# 5. Proposed System Architecture

The application will consist of a unified frontend, a provider-management layer, optional local backend services, and independently managed inference providers.

```text
                  USER INTERFACE
        ┌───────────────────────────────┐
        │ Conversations                 │
        │ Characters and Personas       │
        │ Lorebooks and Memory          │
        │ Model and Endpoint Selection  │
        │ Workflow Controls             │
        │ Diagnostics and Settings      │
        └──────────────┬────────────────┘
                       │
        ┌──────────────▼────────────────┐
        │      APPLICATION CORE         │
        │                               │
        │  Conversation Manager         │
        │  Context and Prompt Builder   │
        │  Model Router                 │
        │  Workflow Orchestrator        │
        │  RAG / Memory Manager         │
        │  Secure Storage               │
        └──────────────┬────────────────┘
                       │
       ┌───────────────┴────────────────┐
       │                                │
┌──────▼────────────┐        ┌──────────▼─────────────┐
│ Browser Provider  │        │ Remote API Providers  │
│                   │        │                        │
│ WebLLM            │        │ Strata                 │
│ WebGPU / Workers  │        │ LM Studio              │
│ Browser Cache     │        │ Ollama                 │
│                   │        │ llama.cpp              │
│                   │        │ OmniRoute / Others     │
└───────────────────┘        └────────────────────────┘
```

Although WebLLM and remote inference servers share a common interface at the application level, they must have different underlying adapters.

WebLLM executes inside the browser process. Remote providers communicate through HTTP APIs.

Our application core should conceal those implementation differences from the conversation and character systems.

---

# 6. Multi-Endpoint Inference System

## 6.1 General requirements

The endpoint manager is a primary component, not an optional feature.

Users should be able to create as many endpoint configurations as they need.

Each endpoint will have a persistent identity, connection settings, and an inventory of available models.

### Proposed endpoint properties

| Property | Purpose |
|---|---|
| Name | Human-readable identification |
| Provider type | OpenAI-compatible, Ollama, WebLLM, etc. |
| Base URL | Network address when applicable |
| Authentication | Optional API key or supported credential method |
| Enabled | Include or exclude from active use |
| Model inventory | Models reported or configured |
| Capability profile | Chat, embedding, streaming, tools, vision, etc. |
| Context information | Advertised or manually configured limits |
| Health state | Connected, unavailable, loading, error |
| Privacy classification | Local machine, trusted LAN, or external |
| Concurrency limit | Maximum active requests allowed |
| Default roles | Chat, summarization, embedding, evaluation |

Endpoint profiles should survive application restarts.

## 6.2 Strata integration

Strata is one of our intended primary inference hosts.

Strata currently serves **Qwen3.8-Flash-Next** (`qwen3.8-flash-next-unsloth-ud-iq4_xs`, 32k context). The earlier "Qwen 111B" label came from a download size in Strata's docs and is superseded (DR-005).

The application should be capable of:

- Connecting to Strata at its configured address.
- Authenticating if the server requires credentials.
- Discovering its available models when the API permits.
- Streaming responses from the Strata model.
- Sending configurable generation parameters.
- Managing request cancellation and timeouts.
- Treating Strata as one of several inference providers rather than a special hardcoded backend.

**Resolved 2026-10-18 (DR-005):** Strata's protocol, authentication and address are verified in `Strata_Protocol.md` (OpenAI-compatible, `http://127.0.0.1:8080`, optional bearer token). The adapter still takes the address from endpoint configuration rather than hardcoding it.

## 6.3 LM Studio integration

LM Studio is a core supported provider.

The application should use its OpenAI-compatible API where appropriate, allowing models currently served through LM Studio to be discovered and selected.

Model loading and unloading should remain under LM Studio's control.

The new application should not automatically start LM Studio, terminate its processes, change its configuration, or load a different model without an explicit user command through a supported API.

This also allows our frontend to coexist with other applications that already use LM Studio.

**Verified 2026-10-18:** the owner's LM Studio host is `http://10.138.11.11:1234` (trusted LAN, plain HTTP). Observed behavior is in `docs/PROVIDER-SPEC.md`.

## 6.4 Ollama integration

Ollama should be supported through either its native API or compatible chat-completions interface.

Native Ollama support is useful for:

- Enumerating locally installed models.
- Distinguishing installed models from currently loaded models.
- Supporting Ollama-specific configuration where appropriate.
- Handling model loading and streaming behavior accurately.

Ollama should appear in the same unified model selector as other providers.

## 6.5 llama.cpp integration

Existing llama.cpp servers should work as external inference endpoints.

The application will not need to download, compile, or manage llama.cpp.

For an initial release, connection through its OpenAI-compatible API should be sufficient for conversational inference.

More advanced server-specific capabilities may be added through a dedicated adapter.

## 6.6 OmniRoute and other compatible gateways

The provider system should also accommodate an existing model-routing gateway such as OmniRoute.

This is useful because the application should not need a separate native integration for every individual backend.

If a gateway exposes an appropriate API, it can be registered as an endpoint and provide access to several models.

The endpoint manager must distinguish between the gateway itself and the individual models it advertises.

---

# 7. WebLLM Integration

## 7.1 Purpose

WebLLM will be a first-class provider.

Unlike Strata or LM Studio, WebLLM runs its inference engine directly inside the browser, using WebGPU.

This provides a useful lightweight inference option without requiring a separate server process.

A WebLLM model could perform:

- Quick conversational responses.
- Prompt classification.
- Task routing.
- Lightweight summarization.
- Metadata extraction.
- Potentially memory-related tasks, depending on model capability.
- Side-by-side comparison against remote models.

## 7.2 Model compatibility

WebLLM does not simply load ordinary llama.cpp GGUF files.

It uses models and compiled artifacts compatible with the MLC/WebLLM runtime.

Consequently, WebLLM will maintain its own model catalogue and cache, separate from LM Studio and Ollama.

The interface should clearly distinguish WebLLM models from models that exist only on an external server.

## 7.3 Operational requirements

WebLLM integration should provide:

1. Browser and WebGPU capability detection.
2. A catalogue of supported browser models.
3. Explicit user approval before model downloads.
4. Download progress and estimated storage requirements.
5. Loading and initialization progress.
6. Local model caching.
7. Streaming generation.
8. Cancellation.
9. Model unloading and memory reclamation where supported.
10. Graceful errors when hardware or memory is insufficient.
11. A dedicated Web Worker where appropriate to preserve UI responsiveness.

WebLLM JavaScript dependencies should be pinned and packaged with the application rather than automatically loaded from arbitrary remote script sources.

Model artifacts should use trusted, approved download sources and integrity verification where available.

## 7.4 Important device distinction

WebLLM runs on the device displaying the webpage.

If our application runs in a browser on the primary desktop, the desktop GPU performs WebLLM inference.

If opened on a phone or tablet, WebLLM inference would run on that phone or tablet, subject to its browser and hardware support.

By contrast, choosing the Strata model would route the request to Strata regardless of which device displays the frontend.

This distinction should be visible to users, particularly when selecting models.

## 7.5 Limitations

Browser inference is affected by GPU memory, browser compatibility, model availability, and browser lifecycle behavior.

A browser-hosted model should not be relied upon for persistent background jobs after its tab is closed or suspended.

Tasks that must survive browser closure should run through a persistent server-based provider and, when necessary, the local backend.

---

# 8. Unified Provider Abstraction

We should implement a common provider interface rather than embedding inference logic directly into the UI.

Conceptually, each provider adapter should expose capabilities such as:

```text
connect()
testConnection()
listModels()
getModelCapabilities()
streamChat()
cancelGeneration()

Optional capabilities:
embedText()
countTokens()
loadModel()
unloadModel()
getStatus()
```

The exact method names are implementation details, but the separation is important.

A conversation should not need to know whether a model is running in WebLLM, LM Studio, Ollama, or on Strata.

It should simply specify a model identity, an endpoint identity, the required capabilities, and the conversation request.

## 8.1 Model identity

Model selection must use a compound identity:

```text
Endpoint ID + Model ID
```

Using only a model name would be insufficient.

For example, two endpoints may both advertise a model called `qwen`.

They may use different quantizations, context windows, versions, or system prompts.

The application must preserve their separate identities.

## 8.2 Model capabilities

The provider layer should eventually understand:

- Text generation.
- Streaming.
- Tool calling.
- Structured output.
- Embeddings.
- Vision and image inputs.
- Model-reported context limits.
- Reasoning output formats.
- Concurrent request support.
- Model loading requirements.

Capabilities should be verified or manually configured rather than inferred solely from a model's name.

## 8.3 Switching models

Model switching will depend on the provider.

For an already-running remote endpoint, switching may simply mean changing the model ID sent in the next request.

For WebLLM, switching may require unloading and loading a different model, potentially involving downloads or GPU-memory changes.

The UI should present one consistent interaction while displaying the actual operational consequences.

---

# 9. Multiple Models Working Together

This is one of the most significant improvements over GobboNet.

## 9.1 What GobboNet currently does

Our source review found three major model-related workflows.

**Primary conversation:** One selected language model generates conversational replies. In the standard local setup, llama.cpp is configured for one parallel generation slot.

**Semantic retrieval:** A separate embedding model, normally `nomic-embed-text-v1.5`, serves embedding requests. It can remain loaded alongside the conversational model, typically on the CPU.

**Lore summarization:** GobboNet can use the chat model or a separately configured summarization model. In the standard local configuration, a different summarization model is loaded temporarily and then replaced by the chat model again.

This means GobboNet can have a conversational model and embedding model available together, but it is not inherently a multi-agent or multi-LLM orchestration system.

Our application will extend this model considerably.

## 9.2 Proposed role-based inference

The application should support logical model roles.

| Role | Responsibility | Illustrative assignment |
|---|---|---|
| Primary chat | Main user-facing response | Strata model |
| Fast assistant | Lightweight requests | WebLLM |
| Summarizer | Compress old conversation history | Small LM Studio model |
| Embedding provider | Generate semantic vectors | Dedicated local embedding model |
| Memory manager | Extract useful persistent knowledge | WebLLM or LM Studio |
| Evaluator | Review or compare responses | Separately configured model |
| Fallback | Handle an unavailable endpoint | User-approved alternative |

These are examples, not fixed assignments.

Users should be able to assign any suitable supported model to any role.

## 9.3 Sequential and parallel workflows

We should support both kinds of execution.

**Sequential workflow:** A smaller model classifies the request, a larger model produces the response, and an evaluator optionally reviews the result.

**Parallel workflow:** Two or more providers answer a request independently, after which their responses can be displayed side-by-side or combined by a designated synthesis model.

Parallel processing is particularly appropriate when models run on independent hardware or endpoints.

The application should honor provider concurrency limits, avoid overloading a shared engine, and permit cancellation of individual branches.

## 9.4 Optional intelligent routing

A future enhancement could provide automatic model selection.

For example:

```text
Incoming request
       |
       v
Optional lightweight classifier
       |
       +-- Simple request --> WebLLM
       |
       +-- General chat ---> LM Studio
       |
       +-- Complex task ---> Strata model
       |
       +-- Comparison -----> Multiple models
```

This is a proposed capability rather than a requirement for the first implementation.

Manual selection must always remain available.

The user should be able to see which model handled a task and override automatic routing.

## 9.5 Privacy-aware routing

Model selection is also a privacy decision.

The application must not silently send a local conversation to a remote provider simply because a preferred model is unavailable.

Automatic fallback should be opt-in.

Each endpoint should have a visible privacy classification, with explicit distinctions between the same computer, a trusted LAN host, and an external service.

For multi-model workflows, the application should disclose which providers will receive the conversation or derived content.

---

# 10. Conversation and Chat System

GobboNet's conversational feature set is a useful baseline.

Our initial functional target should include:

- Multiple saved conversations.
- Conversation titles and timestamps.
- Folder and tag organization.
- Searchable conversation history.
- Per-conversation model assignments.
- Character associations.
- Message streaming.
- Cancel and regenerate controls.
- Message editing.
- Conversation branching.
- Response variants.
- System prompt controls.
- Adjustable sampling parameters.
- Appropriate rendering of model-provided reasoning fields.
- Copy and export operations.
- Token and context usage indicators.

The chat interface should be responsive across desktop screens and smaller devices.

Conversation history should remain independent of any one model.

Changing the selected endpoint must not destroy or rewrite the conversation's existing messages.

Ideally, generated responses would record provenance such as:

```text
Provider
Endpoint
Model
Generation settings
Timestamp
Response duration
Token usage, when available
```

This would be particularly valuable for comparing inference models.

---

# 11. Character Cards and Personas

Characters are a major part of GobboNet's appeal and should be preserved.

## 11.1 Character definitions

A character may include:

- Name and description.
- Personality and behavior guidance.
- Scenario or background information.
- First message and alternate greetings.
- System prompt instructions.
- Avatar and display preferences.
- Preferred conversational model.
- Preferred summarization model.
- Generation settings.
- Associated lorebooks.
- Memory and retrieval settings.

Characters should be portable and independent of any specific endpoint.

## 11.2 Character-card compatibility

We should aim to support established character-card import and export formats, including V2 and V3 where practical.

PNG-embedded character metadata is a useful feature of GobboNet and should be considered for compatibility.

Imported character data must be treated as untrusted.

**We should not reproduce GobboNet's unrestricted JavaScript character-card hooks.**

If scripting or advanced character behavior becomes desirable, it should be implemented through a restricted, explicitly permissioned system.

## 11.3 Personas

Users should be able to define their own personas, including names, preferences, descriptions, and optional avatars.

Persona selection should be independent of character selection and model selection.

---

# 12. Lorebooks and Retrieval-Augmented Generation

GobboNet's hybrid retrieval architecture is one of the most valuable pieces of its design.

It is worth retaining conceptually, while making the implementation more flexible.

## 12.1 Semantic retriever

GobboNet's semantic retriever:

1. Divides lorebook material into entities and text chunks.
2. Generates embedding vectors for relevant text.
3. Caches document embeddings.
4. Embeds recent conversation context.
5. Measures semantic similarity.
6. Identifies likely relevant lore.

Our system should preserve this workflow but allow a user-configured embedding provider.

The embedding model should not be hardcoded to a particular GGUF.

Document embeddings should be cached with information identifying the embedding model and version. This prevents vectors from incompatible models being mixed accidentally.

## 12.2 Weighted-tag retriever

GobboNet also has a non-LLM retrieval path based on weighted tags.

This uses structured keywords, matching thresholds, relationships, and entity-level rules.

For example, a lorebook entry about a particular starship might become eligible when the ship's name, location, crew, or related concepts appear in the conversation.

This is valuable because it is predictable, inexpensive, and available even when the embedding service is offline.

We should preserve:

- Weighted tags.
- Match thresholds.
- Entity and sub-entity support.
- Relationship-based expansion.
- Rules for controlling which entries are eligible.
- Configurable retrieval windows.

## 12.3 Unified ranking

The two retrieval methods should feed a common ranking and budgeting system.

The system should deduplicate results, score their relevance, and include only as much lore as the context budget permits.

Retrieval should be transparent.

An optional inspector should explain:

- Which entries were considered.
- Which entries were selected.
- Why an entry matched.
- Whether it matched semantically or through tags.
- How many tokens it consumed.
- Which entries were excluded by budget limits.

This is particularly useful when diagnosing why a character remembered or overlooked something.

## 12.4 Graceful degradation

If the embedding provider is unavailable, the system should continue using weighted-tag retrieval.

If retrieval fails entirely, normal conversation should remain functional.

An unavailable secondary feature should not unnecessarily prevent the primary AI from answering.

---

# 13. Long-Term Memory and Conversation Summarization

GobboNet compresses older conversation messages into a persistent summary as the active context becomes crowded.

This behavior is worth preserving, but our multi-endpoint architecture allows a better implementation.

## 13.1 Context management

Each conversation needs a context budget that considers:

- The selected model's actual context limit.
- System instructions.
- Character definitions.
- Recent conversation messages.
- Existing summaries.
- Retrieved lore.
- Attachments and tool results.
- Space reserved for the model's response.

The application should avoid relying solely on a fixed word-to-token estimate when a reliable tokenizer or provider token count is available.

## 13.2 Summary generation

When context pressure increases, the system should summarize older messages into a compact memory.

The summary should preserve relevant facts, relationships, commitments, and significant conversation events.

Unlike GobboNet's local swapping mechanism, our application should be able to use a dedicated summarization model on a separate endpoint.

For example:

```text
Conversation: Strata model
                |
                v
      Context nearing capacity
                |
                v
  Summarization: LM Studio model
                |
                v
       Updated conversation lore
                |
                v
   Continue using Strata model
```

There should be no need to unload the Strata model merely to perform summarization.

## 13.3 Memory safety and recoverability

The application should never silently delete original conversation history during summarization.

Summaries are derived information. They should supplement the durable transcript rather than replace it.

A failed summary should not cause irreversible information loss.

The user should be able to inspect and edit summaries and, ideally, see which conversation messages contributed to them.

## 13.4 Optional asynchronous consolidation

A future version could perform memory extraction and consolidation separately from active chat generation.

For example, after the primary model responds, a smaller model might examine the completed exchange and propose memory updates.

These updates should be reviewable or governed by an explicit policy, especially where persistent character lore is being modified.

Browser-based WebLLM tasks would only remain active while the browser permits them to run. Durable background processing should rely on a persistent service.

---

# 14. Web Search and External Information

GobboNet has an optional web-search capability.

Our application should support a comparable feature, but it need not be part of the earliest milestone.

Potential requirements include:

- Explicit enable/disable controls.
- Configurable search provider.
- Credential management.
- Presentation of retrieved sources.
- Inclusion of selected results in model context.
- Clear separation of external content from trusted instructions.
- Request and result visibility.
- Search history and diagnostics where useful.

Search queries may disclose sensitive information to an external provider.

Users should understand when search is invoked and which service receives the query.

Retrieved web content should be treated as untrusted and should never be permitted to redefine application security policies or execute commands.

---

# 15. Security Architecture

Security is a defining feature of this project rather than an afterthought.

## 15.1 No inference-engine installation

Our application will not automatically download or install llama.cpp, Ollama, LM Studio, or any other inference engine.

Those systems will be managed independently.

For WebLLM, the user may explicitly download compatible model assets, but such downloads will not include arbitrary native executables managed by our application.

## 15.2 No elevated privileges

The application should not need administrator rights for ordinary operation.

It should not modify firewall rules, system services, user accounts, or system-wide security settings.

It should not request antivirus exclusions.

## 15.3 Localhost-first networking

The backend, if implemented, should listen on `127.0.0.1` by default.

LAN access must be an explicit configuration choice.

Remote browser access should use authenticated HTTPS rather than transmitting passwords and session credentials over unencrypted HTTP.

The backend should not automatically expose inference endpoints to the network.

## 15.4 Endpoint allowlisting

Only explicitly configured inference destinations should be contacted.

The server must defend against arbitrary URL fetching and server-side request forgery.

Model output, user messages, and imported content must not be able to redirect requests to unrestricted network addresses.

Authentication credentials must be scoped to their configured endpoint.

## 15.5 Browser security

The application should use a restrictive Content Security Policy and avoid dangerous HTML rendering.

Model output and imported content must be sanitized before being inserted into the page.

Remote JavaScript execution should not be part of the normal customization mechanism.

If extensions are supported later, they should run within restricted capabilities and require explicit permissions.

## 15.6 File and data safety

Application-owned directories should be clearly identified.

Deletion should be limited to those owned paths, with confirmation for destructive operations.

Uninstalling the application must not remove user models, unrelated folders, or external inference services.

Import and export should validate file formats and enforce reasonable size limits.

## 15.7 Supply-chain integrity

Dependencies should be version-pinned and auditable.

Where downloads are required, hashes or other appropriate integrity controls should be used.

Build artifacts should have a traceable relationship to the source.

We should maintain an explicit dependency inventory and review security-relevant updates.

---

# 16. Application Stack

We discussed two possible implementations.

## Option A: Single-file HTML application

A single-file HTML application would contain the frontend and interact directly with compatible inference APIs.

Advantages:

- No installation.
- Easy to inspect and distribute.
- Simple to test.
- Well suited to a proof of concept.
- Fits our preference for lightweight, portable applications.

Limitations:

- Browser CORS restrictions.
- Secure handling of credentials.
- Difficulty providing a consistent proxy for unrelated endpoints.
- Reduced access to operating-system services.
- Limitations on persistent background activity.
- More complicated secure LAN deployment.

This remains a useful prototyping option.

## Option B: Browser frontend with a minimal Go backend

A lightweight Go service would:

- Serve the application frontend.
- Maintain endpoint configurations.
- Proxy requests to approved inference services.
- Protect endpoint credentials.
- Provide consistent streaming behavior.
- Manage durable conversations and backups.
- Coordinate persistent inference jobs.
- Enforce security and network policies.
- Support multi-device access when explicitly configured.

The frontend would remain HTML, CSS, and JavaScript.

WebLLM would operate directly inside the browser instead of passing its inference through the Go service.

**Accepted 2026-10-18 (DR-002): Option B.** A standalone HTML proof of concept is optional. The recommended Phase 1 start is the minimal Go backend, because Strata has CORS off and a standalone page cannot reach it (see `docs/ARCHITECTURE.md`).

The backend should remain small and should not take on responsibility for managing inference engines.

A desktop wrapper such as Tauri is not currently required and can be reconsidered later if a particular capability justifies it.

---

# 17. Data Storage and Portability

The application will need durable storage for:

- Endpoint profiles.
- Model inventories and user overrides.
- Characters and personas.
- Conversations and message variants.
- Lorebooks.
- Embedding caches.
- Conversation summaries.
- Application preferences.
- Workflow configurations.
- Generation metadata.
- Scheduled tasks, if implemented.

For a browser-only prototype, IndexedDB is a reasonable starting point.

For a Go-backed application, SQLite is a strong candidate for durable structured storage.

**Accepted 2026-10-18 (DR-006):** SQLite managed by the Go backend is the authoritative store; WebLLM weights and runtime caches stay in browser storage.

The application should provide human-readable or otherwise well-documented export formats.

Backups should be deliberate, portable, and restorable.

API keys and other credentials should not be included in ordinary conversation exports by default.

We should also consider separating user-authored data from generated or reconstructible information such as embedding caches.

---

# 18. User Interface and Experience

GobboNet's feature set can guide the initial interface.

The design should make complex functionality approachable without hiding important information.

## Main interface areas

**Conversations:** Chat history, active thread, message controls, model identification, and streaming output.

**Characters:** Character selection, editing, greeting configuration, lorebooks, and preferred inference assignments.

**Models and endpoints:** Connections, health status, model discovery, model capabilities, and routing choices.

**Memory:** Current summary, retrieval activity, lorebook entries, and memory-management controls.

**Workflows:** Inference roles, comparative generation, automatic routing, and optional background processing.

**Settings:** General preferences, security, storage, backups, web search, and diagnostics.

## Model selector

The model selector should group models by endpoint rather than presenting one undifferentiated list.

For example:

```text
STRATA
  Qwen3.8-Flash-Next (server-reported ID)

LM STUDIO
  [Models currently advertised by LM Studio]

OLLAMA
  [Models currently advertised by Ollama]

WEBLLM
  [Compatible browser models]

OTHER ENDPOINTS
  [Registered and discovered models]
```

These are illustrative entries, not a claim about the currently loaded inventory on any provider.

Each model should display relevant information such as availability, approximate context limit, capabilities, and whether inference is browser-local, LAN-hosted, or external.

## Visibility and diagnostics

A major usability improvement would be showing which models are active and what work they are performing.

An optional diagnostics panel might display:

- Current inference endpoint.
- Active model.
- Request status.
- Time to first token.
- Generation speed.
- Response duration.
- Token usage.
- Retrieval operations.
- Summary operations.
- Errors and fallback decisions.

This would be particularly useful when developing and testing the application with several endpoints.

---

# 19. GobboNet Feature Inventory

The following inventory identifies what we should preserve, improve, or defer.

| Feature | Proposed treatment |
|---|---|
| Streaming chat | Core |
| Conversation history | Core |
| Message editing and regeneration | Core |
| Character cards | Core |
| User personas | Core |
| Lorebooks | Core |
| Hybrid RAG | Core |
| Conversation summarization | Core |
| Model selection | Core, expanded |
| Multiple endpoint connections | Core, new |
| WebLLM | Core, new |
| Model-specific role assignments | Core, new |
| Simultaneous inference across endpoints | Planned |
| Model comparison mode | Planned |
| Automatic routing | Later phase |
| Prompt presets | Planned |
| Generation parameters | Core |
| Conversation branching | Planned |
| Import/export | Core, expanded over time |
| Character-card V2/V3 compatibility | Planned |
| Alternate character greetings | Planned |
| Web search | Optional later phase |
| Macros and auto-continue | Optional later phase |
| Scheduled prompts | Optional later phase |
| Multi-device access | Later phase |
| API-based extensions | Future consideration |
| Arbitrary JavaScript extensions | Excluded by default |
| Native inference-engine installation | Excluded |
| Automatic firewall changes | Excluded |
| Automatic antivirus exclusions | Excluded |
| Unrestricted process management | Excluded |

This is a proposed prioritization, not a commitment to abandon the deferred GobboNet features.

---

# 20. Development Strategy

We should build the application in stages, with working software at the end of each stage.

## Phase 1: Foundation and proof of concept

**Objective:** Demonstrate one chat interface operating with multiple inference providers.

Deliverables:

- Basic responsive browser interface.
- Endpoint configuration.
- OpenAI-compatible provider adapter.
- Connection tests.
- Model discovery.
- Model selection.
- Streaming generation.
- Conversation persistence.
- Basic diagnostics.
- Initial Strata and LM Studio connectivity.

Success criterion: We can create a conversation, choose the Strata model, receive streamed responses, then select a model from LM Studio and continue working without replacing the application.

## Phase 2: WebLLM

**Objective:** Add browser-native inference as an equal provider.

Deliverables:

- WebGPU detection.
- WebLLM integration.
- Supported model catalogue.
- Explicit model download and caching.
- Loading and status display.
- Worker-based inference.
- Streaming and cancellation.
- Integrated model selection.

Success criterion: The user can switch between a WebLLM model and a remote inference model through the same chat interface.

## Phase 3: GobboNet functional foundation

**Objective:** Reintroduce the character-centered experience.

Deliverables:

- Character definitions.
- Personas.
- Character-specific prompts.
- Character-specific model assignments.
- Lorebook editor.
- Expanded conversation controls.
- Improved persistent storage.
- Initial import and export formats.

Success criterion: A character and its conversation can move between inference providers without losing configuration or history.

## Phase 4: RAG and memory

**Objective:** Implement GobboNet-inspired hybrid retrieval and conversation summarization.

Deliverables:

- Embedding provider selection.
- Embedding cache.
- Semantic retrieval.
- Weighted-tag retrieval.
- Unified lore ranking.
- Retrieval-budget management.
- Conversation compression.
- Separate summarization-model assignment.
- Memory and retrieval inspector.

Success criterion: The application uses a dedicated embedding provider, retrieves relevant lore, and can summarize history through a model other than the primary chat model.

## Phase 5: Multi-model workflows

**Objective:** Take advantage of the multi-endpoint architecture.

Deliverables:

- Role-based model assignments.
- Workflow execution coordinator.
- Endpoint concurrency limits.
- Parallel requests.
- Side-by-side model comparisons.
- Optional response synthesis.
- Explicit fallback policies.
- Workflow diagnostics.

Success criterion: The Strata model can handle a conversation while a separate model handles summarization or another permitted workflow without requiring the primary model to be unloaded.

## Phase 6: Advanced functionality

Potential deliverables:

- Automatic model routing.
- Web search.
- Scheduled prompts.
- Advanced macros.
- Memory consolidation.
- Restricted extension APIs.
- Additional model modalities.
- HTTPS-enabled multi-device access.
- Expanded character-card compatibility.
- Additional import/export capabilities.

These features should be prioritized according to actual use rather than implemented automatically.

---

# 21. Testing and Verification

Every phase should include functional and security tests.

## Provider testing

Test against supported API types, including:

- A functioning endpoint.
- An offline endpoint.
- An endpoint with an invalid model name.
- A model that does not support streaming.
- An endpoint that rejects authentication.
- Interrupted connections.
- Slow model startup.
- Model context overflow.
- Concurrent requests.

## WebLLM testing

Test:

- Supported WebGPU hardware.
- Browsers without WebGPU.
- Insufficient GPU memory.
- Interrupted downloads.
- Cached model reuse.
- Model switching.
- Browser reload during initialization.
- Worker termination and recovery.

## Security testing

Verify that:

- The application requires no elevation.
- It does not change firewall rules.
- It does not download executable software.
- It does not execute untrusted character-card scripts.
- It cannot contact arbitrary network addresses through model-controlled content.
- Endpoint credentials are not leaked to another endpoint.
- Conversation content is not silently sent to unauthorized providers.
- Imported content cannot inject executable HTML or JavaScript.
- Uninstall and cleanup operations affect only owned files.
- Backup and restore operate correctly.

## Data-integrity testing

Verify that editing, branching, summarizing, switching models, and interrupted generation cannot silently destroy conversation history.

The application should retain original messages even when they are omitted from a model's active context.

---

# 22. Architectural Principles

The following principles should guide implementation decisions.

### 1. Model independence

The interface must not depend on any specific model family, quantization, inference engine, or vendor.

### 2. Provider independence

Strata, LM Studio, Ollama, llama.cpp, and WebLLM should connect through a shared conceptual interface.

### 3. Local control

The user controls endpoints, models, storage, credentials, and data transmission.

### 4. Least privilege

The application should operate with the minimum permissions required for its functionality.

### 5. No hidden execution

Model output, character cards, web pages, and lorebooks are content, not executable instructions.

### 6. Graceful degradation

Loss of a secondary service should not unnecessarily break the primary chat experience.

### 7. Durable conversations

Original user and assistant messages should remain recoverable even after summarization or model changes.

### 8. Transparent routing

Users should know which inference provider processed a request.

### 9. Explicit resource use

Large downloads, additional GPU usage, parallel requests, and external network access should be visible and controllable.

### 10. Modular growth

Adding a new provider or workflow should not require restructuring the conversation interface.

### 11. Security over convenience shortcuts

We should not reproduce unnecessary PowerShell scripts, firewall manipulation, permissive network defaults, or unsandboxed scripting simply because they exist in the reference project.

### 12. Practical implementation

Features should be introduced when they provide measurable value. We should avoid creating an unnecessarily complicated framework before there is a working application.

---

# 23. Decisions Already Established

The following represents the direction agreed in our discussion.

| Decision | Status |
|---|---|
| Use GobboNet as the design and feature reference | Agreed |
| Rebuild instead of simply trusting the original installer | Agreed |
| Avoid the problematic system-level behaviors | Agreed |
| Keep inference engines externally managed | Agreed |
| Support multiple inference endpoints | Agreed |
| Support Strata (local host, Qwen3.8-Flash-Next) | Agreed |
| Support LM Studio | Agreed |
| Include WebLLM as a provider | Agreed |
| Support existing Ollama and llama.cpp endpoints | Proposed integration |
| Preserve character and lorebook capabilities | Agreed direction |
| Preserve hybrid RAG | Agreed direction |
| Support more sophisticated multi-model workflows | Agreed direction |
| Use HTML, CSS, and JavaScript for the frontend | Preferred |
| Use a small Go backend for the full application | Accepted 2026-10-18 (DR-002 Option B) |
| Start with a functional browser-based proof of concept | Optional (DR-002); recommended start is the minimal Go backend |
| Selective GobboNet source reuse in a new architecture (DR-003 Option B) | Accepted 2026-10-18; supersedes the independent-implementation preference. See `decision_register.md`. |
| Avoid arbitrary JavaScript extensions | Security requirement |
| Require explicit authorization for external inference | Security requirement |

---

# 24. Open Design Decisions

A few subjects should be resolved as the project develops.

**Status 2026-10-18:** all of these are now decided. The original questions are kept below for history; `decision_register.md` is authoritative.

| Subject | Resolution |
|---|---|
| Application identity | Labbonet (DR-001) |
| Initial deployment model | Browser frontend + minimal Go backend (DR-002) |
| Strata protocol | OpenAI-compatible adapter (DR-005, `Strata_Protocol.md`) |
| Storage | SQLite in Go, browser storage for WebLLM caches (DR-006) |
| Memory ownership | Hierarchical scopes, conversation and lorebooks first (DR-007) |
| Automatic memory updates | AI proposes, user approves (DR-008) |
| Workflow complexity | Role assignment, then pipelines (DR-009) |
| Network access | Localhost only first; authenticated HTTPS LAN later (DR-010) |
| Licensing and distribution | AGPLv3-or-later, selective GobboNet reuse (DR-003, DR-004) |

## Application identity

We have not chosen a name.

The application should have its own identity rather than necessarily retaining the GobboNet name.

## Initial deployment model

The current preference is a browser frontend and small Go backend, but we should decide whether the first proof of concept should be a standalone HTML file or the minimal version of the final architecture.

## Strata protocol

We need to confirm the actual protocol and capabilities exposed by the Strata inference service.

This will determine whether the standard OpenAI-compatible adapter is sufficient or a dedicated adapter is required.

## Storage

We should choose the final persistent storage model after establishing the minimum data structures.

IndexedDB and SQLite are both candidates for different deployment modes.

## Memory ownership

We need to decide whether extracted long-term memory is stored primarily per character, per conversation, per persona, or across several scopes.

## Automatic memory updates

We should establish how much authority an AI has to modify persistent memory without user review.

## Workflow complexity

We need to determine whether the first workflow engine should be a simple role-assignment system or support configurable multi-step workflow graphs.

## Network access

We should decide how important access from phones and other computers is for the initial release.

This affects HTTPS, authentication, and storage synchronization.

## Licensing and distribution

We need to select an appropriate license for the new implementation and define whether any GobboNet source code will actually be reused.

---

# 25. Recommended Initial Project Scope

The first complete development milestone should be narrower than the ultimate feature set.

I recommend beginning with:

**A secure, locally hosted, multi-endpoint chat interface with WebLLM support, persistent conversations, and a clean separation between the UI and inference providers.**

**Note 2026-10-18 (DR-015):** the accepted first milestone excludes WebLLM, which follows immediately as Phase 2. Item 4 in the list below belongs to that second milestone. Phase 1 acceptance tests are in `docs/PHASE1-ACCEPTANCE.md`.

That gives us the foundation on which the more specialized GobboNet features can be added.

The initial milestone does not need to include full character-card interoperability, sophisticated RAG, scheduling, web search, or complex agent orchestration.

It does need to establish the correct provider architecture so those capabilities can be introduced without substantial rewrites.

A successful foundation would demonstrate that the application can:

1. Connect to Strata and LM Studio.
2. Enumerate and select models from those services.
3. Execute and stream conversational inference.
4. Run a supported model directly in the browser using WebLLM.
5. Maintain conversations independently of the selected provider.
6. Persist endpoint settings and conversations reliably.
7. Protect credentials and restrict network access.
8. Operate without elevated permissions or system modifications.

Once that works, the next major milestone should be character cards and lorebooks, followed by hybrid RAG and multi-model workflow orchestration.

---

# 26. Long-Term Vision

The intended application is more than a replacement for GobboNet.

GobboNet provides the feature model: a character-centered conversational AI application with persistent history, lorebooks, semantic retrieval, and context management.

Our architecture would extend that design into a **general-purpose, locally controlled AI interaction and orchestration environment**.

A user could keep a large reasoning model loaded on one server, use smaller models on other machines, run browser-local inference with WebLLM, and assign specialized tasks to whichever model is appropriate.

The system would support both simple conversation and advanced workflows without requiring the user to adopt multiple unrelated interfaces.

Characters, lorebooks, conversations, and memories would remain portable between models.

Most importantly, these capabilities would be implemented without relying on broad operating-system permissions, executable-download mechanisms, unsafe extension execution, or hidden network behavior.

**The central design principle is simple: one interface, many inference engines, persistent knowledge, explicit control, and security by design.**

---

# 27. Suggested Project Organization

When establishing the dedicated project, this document should become the initial project brief.

A practical documentation structure would be:

```text
project/
│
├── README.md
│
├── docs/
│   ├── PROJECT-BRIEF.md
│   ├── REQUIREMENTS.md
│   ├── ARCHITECTURE.md
│   ├── SECURITY-MODEL.md
│   ├── PROVIDER-SPEC.md
│   ├── WEBLLM-INTEGRATION.md
│   ├── RAG-AND-MEMORY.md
│   ├── GOBBONET-FEATURE-INVENTORY.md
│   ├── ROADMAP.md
│   └── DECISIONS.md
│
├── frontend/
│
├── backend/
│
├── tests/
│
└── references/
```

The exact source layout is provisional.

The GobboNet repository should be retained as an external design reference rather than automatically copied into this structure.

Architecture decisions should be documented as we make them, particularly where we depart from GobboNet's behavior.

This will help preserve continuity across development sessions and avoid repeatedly reopening decisions that have already been settled.

---

## Project Definition

**We are developing a secure, locally controlled, multi-provider AI chat application inspired by GobboNet. It will preserve GobboNet's character, conversation, lorebook, RAG, and memory functionality while replacing its inference and installation architecture with independent connections to Strata, LM Studio, Ollama, llama.cpp, WebLLM, and other compatible providers. The system will support model-independent conversations, role-based inference, optional concurrent multi-model workflows, and explicit control over security, networking, and data privacy.**

This definition should serve as the baseline for subsequent specifications and implementation work.