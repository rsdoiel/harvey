---
id: "0001"
title: "Learning mode composes existing review pieces; harvey consumes the knowledge library"
date: "2026-09-23"
status: proposed
kind: decision
trigger: design
project: harvey
phase: ""
supersedes: []
superseded_by: []
relates_to: []
initiative: ""
session: ""
decisions: []
tags: []
uuid: "01a0d021-1916-7a1a-b12e-df1a13f8d152"
origin_host: "wren"
---

**Context.**

`knowledge-learning-mode-feature-request.md` proposed a learning mode
driving `knowledge`'s review lifecycle. Reading the code found harvey
already has a review loop (`Miner.reviewInteractive`, `editInEditor`,
`promptAction`) and routing (`RouteRegistry`, `DispatchToEndpoint`,
`@mention`). `knowledge` v0.0.10–v0.0.11 added a corpus-improvement toolchain
the request never anticipated. Designed in
`knowledge-learning-mode-design.md`.

**Decision.**

1. Harvey consumes the `knowledge` library for these features; it does not
   shell out to `kb`. Skills still drive `kb` for manual use.
2. Learning mode composes existing pieces and does not unify with the memory
   miner. Only the `$EDITOR` round trip is extracted, as
   `editTextInEditor`.
3. `/kb learn` is line-oriented over a UI-agnostic `LearnSession`; a
   bubbletea menu is deferred until after the termlib-to-Charm decision.
4. Draft and review are separate primitives. Nothing but the accept
   keypath calls `PromoteDocumentSummary`.

**Rationale.**

Unifying two working review loops would refactor code that works to save a
little duplication, and they review objects with different rules. A
UI-agnostic core means the Charm migration changes a renderer, not the
logic.

**Rejected alternatives.**

- New `/learn` top-level namespace: breaks the three-silo `/rag`, `/memory`,
  `/kb` precedent.
- Unify with the memory miner: see Rationale.
- Build the bubbletea menu first: blocked on an unreviewed migration.

**Consequences.**

Items 3 and 4 depend on `knowledge` v0.0.12 (DR-0035 in that repo). Status
`proposed`: promotion is the author's call.
