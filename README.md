# Labbonet

Secure, local, multi-provider AI chat. One interface for Strata, LM Studio, Ollama, llama.cpp and in-browser WebLLM, with characters, lorebooks and hybrid RAG. Loopback-first, no installers. AGPLv3.

**Status:** planning complete; Phase 1 design documented. No code yet.

Labbonet is an inference client and workflow orchestrator, not an engine installer. It connects to inference services you already run, treats them as interchangeable providers, and keeps conversations independent of the model that answered them.

## Documents

- [`Initial_Project_Proposal.md`](Initial_Project_Proposal.md): project brief, goals, architecture and phases.
- [`decision_register.md`](decision_register.md): architectural decisions and their status (15 of 16 accepted).
- [`Strata_Protocol.md`](Strata_Protocol.md): verified Strata API behavior for the provider adapter.
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md): components, backend API, storage and repository layout.
- [`docs/SECURITY-MODEL.md`](docs/SECURITY-MODEL.md): threat model and security controls.
- [`docs/PROVIDER-SPEC.md`](docs/PROVIDER-SPEC.md): provider adapter contract with Strata and LM Studio profiles.
- [`docs/PHASE1-ACCEPTANCE.md`](docs/PHASE1-ACCEPTANCE.md): Phase 1 scope, non-goals and acceptance tests.
- [`docs/GOBBONET-REFERENCE.md`](docs/GOBBONET-REFERENCE.md) and [`docs/REUSE-LOG.md`](docs/REUSE-LOG.md): pinned design reference and reuse record.

## Direction

- Browser frontend (HTML, CSS, JavaScript) with a small Go backend that holds credentials, proxies approved endpoints and stores data in SQLite. WebLLM runs in the browser.
- Loopback-only by default. No elevated privileges, no firewall or antivirus changes, no automatic executable downloads, no arbitrary JavaScript extensions.

## Design reference

[GobboNet](https://github.com/ElodineOfficial/GobboNet) is the functional and design reference. Labbonet is not affiliated with or endorsed by GobboNet or its authors.

## License

GNU Affero General Public License v3.0 or later. See [`LICENSE`](LICENSE).
