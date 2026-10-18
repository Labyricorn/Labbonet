# Project Decision Register

**Project:** Secure Multi-Provider AI Chat Interface  
**Reference project:** [ElodineOfficial/GobboNet](https://github.com/ElodineOfficial/GobboNet)  
**Document:** `decision_register.md`  
**Version:** 0.2  
**Created:** October 17, 2026  
**Status:** Active. 15 of 16 decisions accepted, DR-014 deferred; Gate 1 cleared (2026-10-18)

---

## 1. Purpose

This decision register documents the architectural, functional, security, licensing, and implementation choices that remain unresolved for our new AI chat application.

The application is being designed using GobboNet as its primary functional reference, while replacing GobboNet's inference-management approach with a secure, extensible architecture supporting multiple independently managed AI providers.

Our intended providers include Strata (local host, currently serving Qwen3.8-Flash-Next), LM Studio, Ollama, llama.cpp, WebLLM, and other compatible services.

The decision register serves several purposes:

- Prevent previously resolved architectural questions from being repeatedly reopened.
- Clearly distinguish agreed requirements from implementation recommendations.
- Document the reasoning behind consequential technical choices.
- Identify dependencies between decisions.
- Record when a decision becomes necessary for implementation.
- Provide a durable history of changes in project direction.
- Make it easier to transfer the project between development sessions and coding assistants.

**A proposal becomes an approved decision only when explicitly accepted by the project owner.**

---

## 2. Decision Management

### Decision ownership

**Project owner:** User  
**Technical analysis and recommendations:** AI development assistants  
**Implementation authority:** The project owner, with implementation delegated as appropriate

Technical assistants may research options, develop recommendations, and identify risks, but should not silently convert recommendations into accepted architectural decisions.

### Decision statuses

| Status | Meaning |
|---|---|
| OPEN | Requires a choice; no accepted decision exists |
| PROPOSED | A preferred choice has been identified but not approved |
| ACCEPTED | Choice has been approved and is part of the project baseline |
| DEFERRED | Decision intentionally postponed until a defined milestone |
| SUPERSEDED | Replaced by a subsequent decision |

### Priorities

| Priority | Meaning |
|---|---|
| P0 | Blocks foundational architecture or initial implementation |
| P1 | Must be resolved before its associated feature is developed |
| P2 | Can be deferred without undermining the foundation |

### Decision rules

1. Every accepted decision should include its rationale and consequences.
2. Dependencies should be resolved before dependent decisions are finalized.
3. Decisions should favor security, portability, maintainability, and user control.
4. Changes to accepted decisions should be documented instead of silently overwriting their history.
5. A proof of concept may test an option without committing the project to that architecture.
6. Decisions should be revisited when new evidence invalidates their original assumptions.

---

# 3. Master Decision Register

| ID | Decision | Priority | Status | Required by |
|---|---|---|---|---|
| DR-001 | Application identity and name | P2 | ACCEPTED | Public release |
| DR-002 | Application architecture and deployment model | P0 | ACCEPTED | Phase 1 |
| DR-003 | GobboNet code reuse versus independent implementation | P0 | ACCEPTED | Before implementation |
| DR-004 | Project license and distribution model | P1 | ACCEPTED | Before distribution |
| DR-005 | Strata connection protocol and provider adapter | P0 | ACCEPTED | Phase 1 |
| DR-006 | Persistent storage architecture | P0 | ACCEPTED | Phase 1 |
| DR-007 | Long-term memory ownership and scope | P1 | ACCEPTED | Phase 4 |
| DR-008 | Automatic memory update permissions | P1 | ACCEPTED | Phase 4 |
| DR-009 | Multi-model workflow orchestration scope | P1 | ACCEPTED | Phase 5 |
| DR-010 | LAN and remote-access strategy | P1 | ACCEPTED | Before LAN support |
| DR-011 | WebLLM model acquisition and runtime integration | P1 | ACCEPTED | Phase 2 |
| DR-012 | Credential storage and endpoint trust policies | P0 | ACCEPTED | Phase 1 |
| DR-013 | Embedding provider and retrieval storage | P1 | ACCEPTED | Phase 4 |
| DR-014 | Automatic model routing and failover policies | P2 | DEFERRED | Phase 5 or later |
| DR-015 | Minimum viable product feature boundary | P0 | ACCEPTED | Phase 1 |
| DR-016 | Data portability, backups, and migration strategy | P1 | ACCEPTED | Phase 1–3 |

The first ten entries formalize questions already raised in our project brief, with related choices separated where necessary. DR-011 through DR-016 make additional unresolved implementation decisions explicit.

---

# 4. Individual Decision Records

## DR-001: Application Identity and Name

**Priority:** P2  
**Status:** ACCEPTED (2026-10-18, project owner): the name is **Labbonet**  
**Required by:** Public release  
**Dependencies:** None

### Decision required

Determine the application's name, terminology, visual identity, and relationship to GobboNet in public documentation.

### Alternatives

**A. GobboNet-derived name**

Choose a name that visibly associates the application with its design inspiration.

**B. Completely independent identity**

Create a new name that reflects our application's multi-provider and model-independent architecture.

**C. Working codename followed by a final name**

Use a temporary identifier during development and settle on a public name after the interface takes shape.

### Recommendation

**Option C, followed by Option B.**

A working codename allows development to begin without spending time on branding. An independent identity would better reflect the application's substantial architectural differences from GobboNet.

We should describe GobboNet as a reference and inspiration rather than suggest official affiliation.

### Acceptance criteria

- Working project name selected.
- Naming convention established for source repositories and configuration files.
- Final public identity chosen before release.

### Accepted decision

The application is named **Labbonet** (owner choice, 2026-10-18). The name echoes GobboNet, so public documentation must describe GobboNet as a design reference and inspiration only, with no suggestion of affiliation or endorsement. The owner may still revise the name before public release; a rename would be logged here.

### Consequences

The name may influence repository paths, configuration filenames, documentation, and user-data directories.

Renaming late in development could require migration support.

---

## DR-002: Application Architecture and Deployment Model

**Priority:** P0  
**Status:** ACCEPTED (2026-10-18, project owner): Option B, browser frontend with a minimal Go backend; optional standalone HTML proof of concept  
**Required by:** Phase 1  
**Dependencies:** DR-005, DR-006, DR-012

### Decision required

Choose the primary runtime architecture for the complete application.

### Alternatives

**A. Standalone single-file HTML application**

All interface logic runs inside the browser. Remote inference APIs are contacted directly when browser security policies permit.

Advantages include portability, ease of distribution, and minimal installation.

Limitations include CORS, credential handling, persistent background tasks, and server-side networking restrictions.

**B. Browser frontend with a minimal Go backend**

A Go service hosts the interface, maintains endpoint profiles, securely proxies inference requests, manages durable storage, and coordinates server-side jobs.

WebLLM executes directly in the browser.

**C. Packaged desktop application**

Use Tauri, Electron, or a similar wrapper to provide a traditional desktop application with access to native operating-system capabilities.

### Recommendation

**Option B for the full application, with an optional standalone HTML proof of concept.**

This best balances security, extensibility, and the application's multi-provider requirements.

The Go backend should remain intentionally small. It should not download inference executables, manage system services, or control unrelated processes.

### Acceptance criteria

- Frontend/backend boundaries documented.
- Provider API boundary defined.
- WebLLM execution location defined.
- Local startup and shutdown behavior demonstrated.
- Application operates without elevated permissions.

Progress 2026-10-18: boundaries, provider API boundary, WebLLM location and startup/shutdown behavior are documented in `docs/ARCHITECTURE.md`; demonstration is Phase 1 tests A-B1 to A-B4 (`docs/PHASE1-ACCEPTANCE.md`).

### Consequences

The application will require a small companion service for its complete feature set.

A standalone HTML implementation may remain useful for prototypes or a reduced-function portable edition.

---

## DR-003: GobboNet Code Reuse Strategy

**Priority:** P0  
**Status:** ACCEPTED (2026-10-18, project owner): Option B, selective source reuse  
**Required by:** Before implementation  
**Dependencies:** DR-004

### Decision required

Determine whether our application will fork GobboNet, reuse selected source files, or independently implement comparable functionality.

### Alternatives

**A. Direct fork**

Modify the existing repository, preserving its architecture where practical.

**B. Selective source reuse**

Build a new architecture while incorporating selected GobboNet components.

**C. Independent implementation**

Use GobboNet as a reference for behavior, workflows, interface features, and requirements, but implement our own source.

### Recommendation

**Option C.**

Our major changes concern the application's foundational architecture, not just its user interface. Independently implementing the core may be simpler than disentangling GobboNet's assumptions about local server management.

The original repository should remain a reference for feature behavior and interoperability requirements.

### Acceptance criteria

- [x] Code reuse policy approved (Option B, 2026-10-18).
- [x] GobboNet reference commit recorded (`314bdc9`, `docs/GOBBONET-REFERENCE.md`, 2026-10-18).
- [x] Licensing implications documented (AGPLv3 applies to reused code; see DR-004).
- [x] Any copied source tracked explicitly (`docs/REUSE-LOG.md` created 2026-10-18; no source reused yet).
- [x] Feature-parity inventory maintained separately from implementation details (`Initial_Project_Proposal.md` section 19).

### Accepted decision

The owner chose **Option B** (2026-10-18), overriding the Option C recommendation above. The architecture is new, but selected GobboNet components may be incorporated.

Conditions that follow from this choice:

- GobboNet is AGPLv3-or-later. Any copied or adapted GobboNet source is covered by the AGPL, and the combined work must comply (source availability, preserved notices). See DR-004.
- **License (DR-004: AGPLv3-or-later):** copied GobboNet source is compatible with the project license. Keep upstream notices, mark changes, and log each reused file.
- Reuse is selective and deliberate: each reused file or component is recorded before it is merged.
- Reuse must not bring back the behaviors the project excludes: PowerShell or installer logic, firewall changes, `0.0.0.0` listening, recursive-delete uninstall paths, unrestricted JavaScript hooks and remote script loading, and local inference-engine management.
- Behavior and interface ideas may still be reimplemented independently where that is simpler.

### Consequences

Selective reuse can save effort on well-understood pieces (for example card parsing or retrieval logic) but ties any combined work to AGPLv3 and adds review work to confirm that reused code is safe and fits the new architecture.

---

## DR-004: License and Distribution

**Priority:** P1  
**Status:** ACCEPTED (2026-10-18, project owner): **GNU AGPLv3-or-later**   
**Required by:** Before distribution  
**Dependencies:** DR-003

### Decision required

Choose the license for our application and how finished versions will be distributed.

GobboNet itself is licensed under GNU AGPLv3-or-later.

### Alternatives

**A. AGPLv3**

Appropriate if we intentionally build upon AGPL-covered GobboNet source, subject to the license's conditions.

**B. Permissive open-source license**

An independently implemented application could use an appropriate permissive license, such as MIT or Apache-2.0.

**C. Private initial development, licensing decision before release**

Keep the licensing decision open until the source provenance and distribution objectives are established.

### Recommendation

**Option C initially, with a permissive license worth considering if we independently implement the application.**

Any reuse of AGPL-covered source must be reviewed before choosing an incompatible license.

**Update 2026-10-18:** DR-003 was accepted as Option B (selective source reuse). Any combined work that includes GobboNet source must be AGPLv3-or-later, so a permissive license is only possible if no GobboNet source ends up in the shipped code. This decision stays OPEN until the actual amount of reuse is known. *(Superseded the same day: the owner accepted AGPLv3-or-later, below.)*

### Accepted decision

Labbonet is licensed under the **GNU AGPLv3-or-later** (owner choice, 2026-10-18), the same license as GobboNet.

This is compatible with DR-003 Option B (selective source reuse), which applies in full.

Consequences:

- Reused GobboNet source is allowed, with its copyright and license notices preserved and changes marked.
- The whole distributed work is AGPLv3-or-later, including when users interact with it over a network: the complete corresponding source must be offered to them.
- Dependencies must be AGPLv3-compatible (MIT, BSD, ISC and Apache-2.0 are; check others individually, and record dependency licenses).
- Contributions are accepted under the same license. Only the copyright holders can relicense later, and only if every contributor and every copied component allows it.

### Acceptance criteria

- [ ] Source ownership and reuse policy established.
- [x] License selected (AGPLv3-or-later; `LICENSE` file added 2026-10-18).
- [ ] Dependency licenses reviewed (inventory started in `docs/ARCHITECTURE.md` section 8; confirm each license when added).
- Attribution and notice requirements documented.
- Distribution format identified.

### Consequences

Licensing determines redistribution obligations, permissible code reuse, and how other developers may contribute.

---

## DR-005: Strata Connection Protocol

**Priority:** P0  
**Status:** ACCEPTED (2026-10-18, project owner): Option A, standard OpenAI-compatible adapter  
**Required by:** Phase 1  
**Dependencies:** DR-002

### Decision required

Determine the actual interface exposed by Strata, our existing inference host.

### Verified findings (2026-10-18)

Full detail in `Strata_Protocol.md`.

- Strata is the open-source MIT project `Niko1221/Strata`. A live instance at `http://127.0.0.1:8080` identified itself via `/health` (`"service": "strata"`).
- It exposes an **OpenAI-compatible API** (`/v1/models`, `/v1/chat/completions` with SSE streaming, `usage` and `timings`), plus Anthropic Messages and Responses APIs. Auth is an optional bearer token. Option A is sufficient for chat.
- It has **no `/v1/embeddings`** (404) and runs **one request at a time** by default.
- **CORS is off**, and foreign-origin POSTs get 403, so browsers cannot call it directly. A backend proxy is required.
- The served model was `qwen3.8-flash-next-unsloth-ud-iq4_xs` (context 32768), **not a "Qwen 111B"**. The "111" in Strata's docs is a download size in GB.
- The owner confirmed (2026-10-18) that the local instance at `http://127.0.0.1:8080` is the intended Strata host. The "Qwen 111B" label in the brief is superseded by the server-reported model, currently `qwen3.8-flash-next-unsloth-ud-iq4_xs`.
- Still untested: cancellation and cold-load timeout behavior, and authenticated mode (the instance has no API key).

### Accepted decision

Use the standard OpenAI-compatible adapter for Strata (Option A). Keep Option B (a dedicated adapter) as an extension path for Strata-only features (`timings`, `/status`, `reasoning_effort`). Strata is registered as an ordinary endpoint with `/health` `service: "strata"` as a detection hint, and is reached through the Go backend because CORS is off.

### Alternatives

**A. Standard OpenAI-compatible adapter**

Use endpoints such as model discovery and chat completions when supported.

**B. Dedicated Strata adapter**

Implement a specific integration if Strata exposes a distinct API.

**C. Existing gateway adapter**

Connect through OmniRoute or another compatible gateway if it already provides access to the model.

### Recommendation

**Prefer Option A when Strata supports it, while preserving Option B as an extension path.**

A standard provider interface minimizes special cases and makes future endpoints easier to add.

### Acceptance criteria

- [x] Strata software identity confirmed.
- [x] API documentation or observed protocol recorded (`Strata_Protocol.md`).
- [x] Connection URL and authentication method established (`http://127.0.0.1:8080`, no key; bearer token if one is later set).
- [x] Model discovery tested.
- [x] Streaming generation tested.
- [ ] Cancellation and timeout behavior tested.
- [x] Strata model successfully accessible through the application (`docs/evidence/step6-chat.png`, `docs/evidence/step7-live.json`; served model: Qwen3.8-Flash-Next).

### Consequences

This decision influences the initial provider interface and therefore blocks reliable implementation of the model-routing layer.

---

## DR-006: Persistent Storage Architecture

**Priority:** P0  
**Status:** ACCEPTED (2026-10-18, project owner): Option D, SQLite (Go) as the authoritative database; WebLLM weights and runtime caches stay in browser storage  
**Required by:** Phase 1  
**Dependencies:** DR-002

### Decision required

Choose where conversations, characters, settings, memories, and related data will be stored.

### Alternatives

**A. Browser IndexedDB**

Simple, browser-native persistence without a backend.

**B. SQLite managed by Go**

Structured storage outside browser-origin limitations, supporting backups, migrations, and durable application data.

**C. Flat JSON documents**

Human-readable files with straightforward export, but more complicated indexing and concurrent updates.

**D. Hybrid storage**

Use SQLite for durable application records and IndexedDB or browser-managed storage for WebLLM-specific assets.

### Recommendation

**Option D, with SQLite as the authoritative application database.**

Conversations, character configurations, summaries, and endpoint metadata should be retained by the backend.

WebLLM weights, browser-specific compilation artifacts, and other runtime caches should remain under the browser's storage model.

### Acceptance criteria

- Data ownership defined.
- Database schema versioning established.
- Conversation persistence demonstrated.
- Browser cache versus durable record boundaries documented.
- Backup and restoration tested.
- Database migrations demonstrated.

Progress: data ownership, schema versioning and cache-versus-durable boundaries are documented in `docs/ARCHITECTURE.md` section 5. A-B5 to A-B8 pass in storage tests; live cross-provider persistence and clean Ctrl+C shutdown are recorded in `docs/evidence/step7-live.json`. Final owner review remains pending.

### Consequences

Moving away from purely browser-local storage improves portability across browsers and devices but introduces a backend persistence layer.

---

## DR-007: Long-Term Memory Scope and Ownership

**Priority:** P1  
**Status:** ACCEPTED (2026-10-18, project owner): Option D, hierarchical memory scopes, implemented incrementally (conversation memory and character lorebooks first)  
**Required by:** Phase 4  
**Dependencies:** DR-006, DR-013

### Decision required

Define how persistent memory is associated with conversations, characters, personas, and potentially the overall application.

### Alternatives

**A. Conversation-only memory**

Every thread maintains its own summary and memory.

**B. Character-specific memory**

Characters retain knowledge across multiple conversations.

**C. Persona-specific memory**

Memory is associated with a user persona, independent of any particular character.

**D. Hierarchical memory**

Support multiple memory scopes with explicit retrieval and sharing rules.

### Recommendation

**Option D, implemented incrementally.**

A useful hierarchy is:

1. Conversation memory.
2. Character memory.
3. Persona memory.
4. Explicitly shared memory.

The initial implementation should prioritize conversation memory and user-authored character lorebooks.

Cross-scope sharing should require explicit configuration.

### Acceptance criteria

- Memory scopes defined.
- Ownership and sharing policies documented.
- Retrieval precedence established.
- Memory-editing interface designed.
- Export and deletion semantics defined.

### Consequences

A hierarchical system provides flexibility but requires careful isolation to avoid one character or conversation unexpectedly accessing another's information.

---

## DR-008: Automatic Memory Update Permissions

**Priority:** P1  
**Status:** ACCEPTED (2026-10-18, project owner): Option B, AI proposes memory updates and the user approves; automatic updates only as a later opt-in  
**Required by:** Phase 4  
**Dependencies:** DR-007, DR-009

### Decision required

Determine whether AI models may autonomously create or modify persistent memories.

### Alternatives

**A. Manual updates only**

The user explicitly edits every memory entry.

**B. AI proposals requiring approval**

Models suggest memory updates that the user can accept, modify, or reject.

**C. Automatic updates with configurable restrictions**

Approved models may modify certain memory scopes automatically.

### Recommendation

**Option B as the initial default.**

A model should be able to propose relevant memory updates without silently rewriting persistent character lore or shared knowledge.

Automatic updates could be introduced later as an opt-in capability, with change history and rollback.

### Acceptance criteria

- Memory mutation policy selected.
- User-review workflow defined.
- Memory provenance recorded.
- Update history and rollback specified.
- Rules for conflicting memories established.

### Consequences

Review-based memory updates require more user interaction but reduce the risk of permanently incorporating hallucinations or incorrect information.

---

## DR-009: Multi-Model Workflow Orchestration

**Priority:** P1  
**Status:** ACCEPTED (2026-10-18, project owner): Option A, role-based assignment first, extended to Option B (sequential and parallel pipelines)  
**Required by:** Phase 5  
**Dependencies:** DR-005, DR-012, DR-014

### Decision required

Choose the complexity and configuration model for coordinating multiple inference providers.

### Alternatives

**A. Role-based assignment**

Users assign models to predefined roles such as chat, summarization, embedding, and evaluation.

**B. Configurable sequential and parallel pipelines**

Users construct workflows that call multiple models in a defined order or concurrently.

**C. General-purpose workflow graph**

A graphical or declarative system supports branching, loops, conditions, and arbitrary workflow components.

### Recommendation

**Start with Option A, then extend to Option B.**

Role assignments provide immediate value without requiring a complex orchestration framework.

The underlying execution model should nevertheless be capable of running independent requests concurrently.

A general-purpose workflow designer can be considered after actual use cases demonstrate the need.

### Acceptance criteria

- Initial model roles documented.
- Role-assignment persistence defined.
- Concurrency and cancellation policies implemented.
- Sequential workflow demonstrated.
- Parallel workflow demonstrated.
- Per-task model provenance recorded.
- Workflow failures handled without corrupting conversations.

### Consequences

The selected scope will determine how much workflow infrastructure must be built before user-facing features can be completed.

---

## DR-010: LAN and Remote Access

**Priority:** P1  
**Status:** ACCEPTED (2026-10-18, project owner): Option A, localhost only initially; Option B (authenticated HTTPS LAN) planned later  
**Required by:** Before LAN support  
**Dependencies:** DR-002, DR-012

### Decision required

Determine whether the application should support access from other devices at initial release and how that access is secured.

### Alternatives

**A. Localhost only**

The application is accessible exclusively from the machine running the backend.

**B. Trusted LAN access**

Other devices may connect over the local network using authenticated HTTPS.

**C. General remote access**

Support access from outside the local network through explicitly configured secure infrastructure.

### Recommendation

**Option A for the initial implementation, with Option B as a planned enhancement.**

Remote access should never become active merely because the application was installed or launched.

The application should not automatically create firewall rules.

### Acceptance criteria

- Default network binding defined.
- Authentication requirements defined.
- HTTPS deployment approach selected.
- Host validation and session security tested.
- LAN activation requires explicit user action.
- External exposure is documented and controlled.

### Consequences

Localhost-only operation minimizes early security complexity.

Adding mobile and LAN access later will require secure origin handling, authentication, and reliable access to shared application data.

---

## DR-011: WebLLM Integration and Model Management

**Priority:** P1  
**Status:** ACCEPTED (2026-10-18, project owner): Option A, curated WebLLM catalogue, evolving toward Option C  
**Required by:** Phase 2  
**Dependencies:** DR-002, DR-012

### Decision required

Define how browser-hosted models are acquired, cached, loaded, and managed.

### Alternatives

**A. Curated catalogue**

Support a small list of verified WebLLM-compatible models.

**B. Open custom model registration**

Allow users to register arbitrary compatible MLC artifacts.

**C. Curated catalogue plus advanced custom registration**

Use verified defaults while permitting controlled advanced customization.

### Recommendation

**Option A initially, evolving toward Option C.**

The application should use pinned WebLLM dependencies, explicit model downloads, integrity checks, and worker-based inference.

WebLLM should share the common provider interface with server-hosted models.

### Acceptance criteria

- Browser compatibility policy documented.
- Initial model catalogue established.
- Download and caching strategy approved.
- Worker integration demonstrated.
- Streaming and cancellation tested.
- GPU-memory failure behavior defined.
- Model unloading tested.
- Offline behavior documented.

### Consequences

Browser storage and GPU limitations make WebLLM substantially different operationally from remote inference servers.

These differences belong in the provider adapter rather than the conversation system.

---

## DR-012: Credentials, Authentication, and Endpoint Trust

**Priority:** P0  
**Status:** ACCEPTED (2026-10-18, project owner): Option B, backend-managed credentials and endpoint allowlist with protected storage  
**Required by:** Phase 1  
**Dependencies:** DR-002

### Decision required

Specify where provider credentials are stored and how authorized inference destinations are enforced.

### Alternatives

**A. Browser-managed credentials**

Store credentials in browser storage and contact endpoints directly.

**B. Backend-managed credentials**

Store credentials on the host and relay approved requests through the backend.

**C. External secret-management integration**

Use an existing operating-system or dedicated secret store for credentials.

### Recommendation

**Use backend-managed endpoint configuration with protected credential storage.**

Where appropriate, use operating-system credential protection rather than storing plaintext API keys in configuration files.

The browser should never receive provider secrets merely to render the interface.

### Required security controls

- Explicit endpoint registration.
- Server-side endpoint allowlisting.
- Credential scoping to individual providers.
- Protection against server-side request forgery.
- No automatic credential forwarding between providers.
- Authentication and authorization for privileged backend operations.
- Secure local and remote session handling.
- Explicit approval before sensitive data crosses trust boundaries.

### Acceptance criteria

- Credential storage mechanism selected.
- Endpoint validation rules documented.
- Threat model completed.
- Authentication design reviewed.
- Secrets excluded from normal exports and logs.
- Negative security tests implemented.

Progress: Windows DPAPI current-user credentials and endpoint validation are implemented. S1-S13/S15 are covered by Go tests, S14 by the browser render page, and S16/S17 by live inspection (`docs/evidence/step7-live.json`). The threat model and authentication design still await owner review; live provider authentication tests were explicitly deferred by the owner.

### Consequences

This decision affects nearly every provider request and must be resolved before the initial connection manager is considered complete.

---

## DR-013: Embedding Provider and RAG Storage

**Priority:** P1  
**Status:** ACCEPTED (2026-10-18, project owner): Option D, provider-independent embedding interface with model-tagged caches  
**Required by:** Phase 4  
**Dependencies:** DR-006, DR-007

### Decision required

Choose how embedding models are accessed and how vectors are retained and searched.

### Alternatives

**A. Dedicated embedding endpoint**

Use an independently managed local embedding server.

**B. Shared inference provider**

Use embedding support offered by LM Studio, Ollama, or another configured endpoint.

**C. Browser-hosted embeddings**

Generate embeddings locally in the browser through a suitable runtime.

**D. Provider-independent embedding interface**

Allow any supported embedding source and maintain compatible caches.

### Recommendation

**Option D.**

Our architecture should not permanently bind RAG to GobboNet's `nomic-embed-text-v1.5` implementation.

The initial RAG system can use an existing embedding endpoint. The provider interface should remain extensible.

The storage system should record the embedding model identity, vector dimensions, and relevant configuration to prevent incompatible vectors from being mixed.

### Acceptance criteria

- Embedding API defined.
- Initial provider chosen.
- Cache format selected.
- Vector search strategy selected.
- Cache invalidation rules defined.
- Tag-only fallback demonstrated.
- Retrieval diagnostics available.

### Consequences

A model-independent embedding layer supports better future experimentation but requires explicit cache compatibility and migration rules.

---

## DR-014: Automatic Routing and Failover

**Priority:** P2  
**Status:** DEFERRED  
**Required by:** Phase 5 or later  
**Dependencies:** DR-009, DR-012

### Decision required

Determine whether the application should automatically choose among providers based on task complexity, endpoint availability, or other criteria.

### Alternatives

**A. Manual selection only**

The user always selects the inference model.

**B. Rules-based routing**

Deterministic policies select models according to the request or workflow.

**C. Model-assisted routing**

A small classifier determines which model should process the request.

**D. Automatic failover**

The application selects a configured alternate provider when the primary endpoint fails.

### Recommendation

**Manual selection first. Add explicit, policy-based routing later.**

Automatic routing should never override the user's privacy preferences or silently send data to an external endpoint.

Automatic failover should be independently configurable and disabled by default when it could change the data's trust boundary.

### Acceptance criteria

- Routing policy model defined.
- Consent and privacy behavior documented.
- Fallback eligibility rules established.
- Routing decisions logged.
- User override supported.
- Failure behavior tested.

### Consequences

Automatic routing can improve efficiency but introduces additional complexity and privacy risks.

---

## DR-015: Initial Feature Scope

**Priority:** P0  
**Status:** ACCEPTED (2026-10-18, project owner): Option B, minimal inference-client foundation first  
**Required by:** Phase 1  
**Dependencies:** DR-002, DR-005, DR-006, DR-012

### Decision required

Define what must exist in the first working release and which GobboNet-inspired features are introduced later.

### Alternatives

**A. Full GobboNet feature parity immediately**

Reproduce the complete character, lorebook, memory, chat, and utility experience before releasing a working version.

**B. Minimal inference-client foundation**

Build the multi-provider engine, interface, streaming chat, and persistence first.

**C. Character-centered initial release**

Prioritize character cards and lorebooks even before all provider infrastructure is complete.

### Recommendation

**Option B.**

The most consequential difference from GobboNet is the new inference architecture. We should prove that architecture before expanding the user interface.

### Proposed first milestone

- Browser-based chat interface.
- Multiple endpoint registration.
- Strata and LM Studio connections.
- Model discovery.
- Streaming and cancellation.
- Per-conversation model selection.
- Durable conversation storage.
- Connection diagnostics.
- Secure credential handling.
- No elevated operating-system permissions.

WebLLM would follow immediately as the next milestone.

Characters, hybrid RAG, and more advanced orchestration would build upon the established provider system.

### Acceptance criteria

- Scope approved.
- Feature acceptance tests documented.
- Non-goals explicitly listed.
- End-to-end scenario demonstrated on two independent endpoints.

Progress: acceptance tests and non-goals are in `docs/PHASE1-ACCEPTANCE.md` (awaiting owner approval). The two-endpoint exit scenario passed with retained history and provenance (`docs/evidence/step7-live.json`), using Strata and the owner-loaded Gemma model on LM Studio. Visual resemblance to GobboNet was rejected by the owner and needs a frontend revision before UI acceptance.

### Consequences

A narrow initial release accelerates architecture validation but postpones some of the functionality that makes GobboNet distinctive.

---

## DR-016: Data Portability, Backup, and Migration

**Priority:** P1  
**Status:** ACCEPTED (2026-10-18, project owner): Option C, combined backup and portable export system  
**Required by:** Phase 1–3  
**Dependencies:** DR-006, DR-007

### Decision required

Define how users can export, back up, restore, migrate, and recover application data.

### Alternatives

**A. Database-level backups only**

Copy the application database.

**B. Human-readable exports**

Provide structured JSON or similar formats for conversations, characters, and configuration.

**C. Combined backup and export system**

Support both reliable complete backups and independent portable exports.

### Recommendation

**Option C.**

The authoritative database should have a reliable backup mechanism, while user-authored content should remain exportable independently of the application.

The format should distinguish conversation content, character definitions, model assignments, and derived artifacts such as embedding caches.

### Acceptance criteria

- Backup format defined.
- Export format documented.
- Import validation implemented.
- Restore tested.
- Database migration tested.
- Destructive import behavior requires confirmation.
- Credentials excluded unless separately and securely handled.
- Conversation integrity verified after restoration.

### Consequences

Early attention to data portability reduces the cost of future architectural changes and helps prevent vendor or implementation lock-in.

---

# 5. Existing Architectural Commitments

The following items are not listed as open decisions because they were already established as project direction during our discussion.

| Requirement | Baseline |
|---|---|
| GobboNet reference | Use the repository as the primary feature and design model |
| Inference architecture | Multiple independently configured providers |
| Strata | Support the existing Strata host (`127.0.0.1:8080`, model as reported by the server) |
| LM Studio | First-class provider integration |
| WebLLM | First-class browser inference provider |
| Other engines | Extensible adapters for Ollama, llama.cpp, and compatible APIs |
| Model inventory | Discover models from connected providers |
| Inference roles | Support task-specific model assignments |
| Conversation portability | Conversations must not depend on a particular model |
| Character system | Preserve character and persona functionality |
| RAG | Retain hybrid semantic and weighted-tag retrieval |
| Memory | Preserve conversation summarization and long-term context |
| Security | Least privilege and explicit network access |
| Installation | No automatic inference-engine installation |
| System modifications | No automatic firewall changes or antivirus exclusions |
| Extensions | No unrestricted arbitrary JavaScript execution by default |

These commitments should be reflected in the requirements and architecture documents.

Changing them should be treated as a scope or architectural change rather than an undocumented implementation detail.

---

# 6. Decision Dependencies

Certain decisions must be resolved in a particular order.

```text id="m91epk"
DR-003 Code Reuse
    |
    +----> DR-004 License


DR-002 Application Architecture
    |
    +----> DR-006 Storage
    |
    +----> DR-012 Credentials / Security
    |
    +----> DR-010 LAN Access
    |
    +----> DR-011 WebLLM Integration


DR-005 Strata Protocol
    |
    +----> DR-015 MVP Scope


DR-006 Storage
    |
    +----> DR-007 Memory Ownership
                 |
                 +----> DR-008 Memory Updates


DR-013 Embedding / RAG
    |
    +----> DR-007 Memory Design


DR-009 Workflow Orchestration
    |
    +----> DR-014 Automatic Routing
```

This is an initial dependency map. It should be updated as decisions are resolved.

---

# 7. Recommended Decision Sequence

The most useful order is based on implementation dependencies rather than numerical ID.

| Order | Decision | Reason |
|---|---|---|
| 1 | DR-003: Code reuse | Establishes implementation provenance |
| 2 | DR-002: Architecture | Determines the application structure |
| 3 | DR-005: Strata protocol | Confirms the first critical provider |
| 4 | DR-012: Credential and endpoint security | Establishes safe networking |
| 5 | DR-006: Storage | Establishes durable application data |
| 6 | DR-015: MVP scope | Establishes the first acceptance milestone |
| 7 | DR-011: WebLLM | Enables second-stage browser inference |
| 8 | DR-013: Embedding and RAG | Enables retrieval architecture |
| 9 | DR-007: Memory ownership | Defines persistent knowledge boundaries |
| 10 | DR-008: Memory updates | Defines autonomous write permissions |
| 11 | DR-009: Orchestration | Defines multi-model coordination |
| 12 | DR-014: Routing and failover | Extends orchestration after validation |
| 13 | DR-010: LAN access | Required before multi-device deployment |
| 14 | DR-016: Portability | Should begin with storage and mature over time |
| 15 | DR-004: Licensing | Must be finalized before distribution |
| 16 | DR-001: Naming | May remain provisional during development |

Some work can proceed in parallel.

For example, interface prototyping does not need to wait for the final application name, and provider discovery can be tested before every memory-management policy is settled.

---

# 8. Decision Log

This section records changes to decision status over the life of the project.

| Date | Decision ID | Event | Outcome |
|---|---|---|---|
| 2026-10-17 | DR-001 through DR-016 | Initial register created | Open, proposed, and deferred decisions documented |
| 2026-10-18 | DR-005 | Protocol verified from Strata docs and a live local instance (`Strata_Protocol.md`) | OPEN to PROPOSED. Option A recommended. Model is Qwen3.8-Flash-Next, not 111B. |
| 2026-10-18 | DR-003 | Owner accepted Option B (selective source reuse), overriding the Option C recommendation | PROPOSED to ACCEPTED. Reused code brings AGPLv3 obligations (DR-004). Reference commit and reuse log still to be created. |
| 2026-10-18 | DR-005 | Owner accepted Option A and confirmed the local instance is the Strata host | PROPOSED to ACCEPTED. Remaining tests (cancellation, cold-load timeout, auth) move to Phase 1 acceptance. |
| 2026-10-18 | DR-001 | Owner named the application **Labbonet** | OPEN to ACCEPTED. Docs must state no affiliation with GobboNet. |
| 2026-10-18 | DR-004 | Owner chose GNU AGPLv3-or-later | OPEN to ACCEPTED. Compatible with DR-003 Option B; reused GobboNet source needs notices preserved and a reuse log. |
| 2026-10-18 | DR-002, DR-006, DR-007, DR-008, DR-009, DR-010, DR-011, DR-012, DR-013, DR-015, DR-016 | Owner accepted all remaining proposals at their recommended options | PROPOSED to ACCEPTED. DR-001 and DR-004 remain OPEN; DR-014 remains DEFERRED. Each record's acceptance criteria are still to be met during implementation. |
| 2026-10-18 | DR-002, 003, 004, 006, 012, 015 | Phase 1 prerequisite docs added in `docs/` (architecture, security model, provider spec, acceptance tests, GobboNet reference, reuse log); stale text in the register, proposal and Strata protocol corrected; LM Studio host `10.138.11.11:1234` verified by read-only probes | No status changes. DR-003 criteria ticked; progress notes added to the others. Implementation defaults (no standalone PoC, `modernc.org/sqlite`, DPAPI, `127.0.0.1:4815`) approved by the owner as defaults, not as DRs. |

Fifteen decisions are accepted. Only DR-014 (routing and failover) is deferred.

Implementation evidence, 2026-10-17 America/Los_Angeles: step 6 committed as `2024e27`; step 7 cross-provider streaming, concurrency, cancellation and shutdown evidence added under `docs/evidence/`. No decision statuses changed. Owner deferred provider auth/cold-start windows and requested closer visual resemblance to GobboNet. Final Phase 1 sign-off remains open.

When an option is approved, an entry should be added here with the date, approved choice, and any relevant conditions.

A decision record should retain its original rationale even if subsequently superseded.

---

# 9. Next Decision Gate

## Gate 1: Foundational Architecture

Before beginning the full implementation, the project should establish accepted decisions for:

- DR-002: Application architecture.
- DR-003: GobboNet code reuse.
- DR-005: Strata integration protocol.
- DR-006: Persistent storage.
- DR-012: Credential and endpoint security.
- DR-015: MVP scope.

The remaining decisions may be developed as their associated implementation phases approach.

**Gate 1 exit condition:** The application has a documented architecture, a verified primary provider protocol, an agreed storage and security design, and a testable first milestone.

At that point, implementation can proceed without reopening fundamental architectural choices.

**Gate 1 status: cleared 2026-10-18.** All six decisions are accepted. The supporting design is in `docs/ARCHITECTURE.md`, `docs/SECURITY-MODEL.md`, `docs/PROVIDER-SPEC.md` and `docs/PHASE1-ACCEPTANCE.md`.

---

# 10. Register Maintenance

When a decision changes status:

1. Update its status and record the approved option.
2. Record the date and decision authority.
3. Preserve the alternatives and rationale.
4. Record any consequences or implementation conditions.
5. Update dependent records.
6. Record the event in the decision log.
7. Update architecture, requirements, or implementation documentation where affected.

An accepted decision should not be silently changed by an implementation assistant simply because an alternative appears easier.

If implementation reveals a material problem, the assistant should report the new evidence and propose an amendment.

---

## Register Summary

The project currently has **16 formally identified decisions**:

| Status | Count |
|---|---:|
| OPEN | 0 |
| PROPOSED | 0 |
| DEFERRED | 1 |
| ACCEPTED | 15 |
| **Total** | **16** |

The foundation decisions are accepted. The immediate priority is meeting their acceptance criteria during Phase 1 (`docs/PHASE1-ACCEPTANCE.md`).

The remaining decisions establish the planned progression toward WebLLM, hybrid retrieval, configurable memory, and concurrent multi-model workflows.

**This register is the decision-management baseline for the project.**
