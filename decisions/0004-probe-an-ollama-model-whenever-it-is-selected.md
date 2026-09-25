---
id: "0004"
title: "Probe an Ollama model whenever it is selected"
date: "2026-09-25"
status: accepted
kind: decision
trigger: request
project: harvey
phase: ""
supersedes: []
superseded_by: []
relates_to: []
initiative: ""
session: ""
decisions: []
tags: []
uuid: "01a0dabf-47f5-7c2f-aaff-4a1853dbce29"
origin_host: "wren"
---

**Context.**

The 2026-06-30 entry in `DECISIONS.md` removed `/ollama` and moved probing to one place: a
`FastProbeModel` call when `/model use` saves a *new alias* for an Ollama model. It rejected keeping
`/ollama probe` because "auto-probe on alias creation covers the same need". That was not true.
Checked on 2026-09-25:

- Nothing else probes. `ThoroughProbeModel` has no caller; startup does not probe.
- A model chosen without saving an alias (the startup picker with the alias prompt declined, or
  `/model use NAME`, which passes `offerAlias=false`) has no cache entry.
- With no entry, `toolsReliable()` answers false, so Harvey pre-injects file contents instead of
  using tools, and the user is not told.
- The alias-time probe wrote `ModelCapability` with `ToolMode` empty, and `ModelCache.Set` replaces
  the whole row, so it erased a tool mode chosen earlier with `/model mode`. A comment in
  `FastProbeModel` and `MODEL_CACHE.md` said such modes survive re-probing; they did not.
- A model updated in Ollama kept its old entry, and no command could refresh it.

**Decision.**

1. Every path that selects an Ollama model ends in `setOllamaModel`. It now calls
   `probeOllamaModelAndCache`, which runs `FastProbeModel` (one `/api/show` request, 5 second
   limit) and stores the result. Selecting a model again refreshes its entry.
2. The probe carries over the existing entry's `ToolMode`, because the probe itself always reports
   auto. A failed probe (server unreachable, unknown model) changes nothing and does not stop the
   selection.
3. The probe at alias creation is removed; `useSelectedModel` prints the same "Probed:" line from
   the cache after selection.
4. No `/model probe` command is added. `/ollama probe` stays removed, as decided on 2026-06-30.

**Rationale.**

`setOllamaModel` is the one place startup, the picker, `/model use` and `@NAME` all reach, so a
probe there cannot be skipped by choosing a different route. One `/api/show` call per selection is
cheap next to loading a model. Refreshing on every selection replaces a staleness policy (an age
limit, a version check) with a rule that needs no explanation. Carrying the tool mode over makes
the documented behaviour true.

**Rejected alternatives.**

- *Add `/model probe [NAME|--all]`.* Reverses a rejected alternative of the 2026-06-30 decision,
  and does nothing for a model nobody thinks to probe.
- *Both auto-probe and an explicit command.* Two behaviours to document and test for a refresh that
  re-selecting the model already gives.
- *Fix only the tool-mode overwrite and the docs.* Leaves models unprobed, so tools silently
  degrade to file injection.
- *Probe only when no entry exists (the `probeLlamaCppAndCache` rule).* Fixes the missing entry but
  not the stale one.

**Consequences.**

Selecting an Ollama model costs one extra request, and up to 5 seconds when the server hangs. A
probe overwrites `SupportsTools` and `SupportsEmbed` each time, so a value learned some other way
is lost; nothing produces such a value today (the thorough probe is not wired in). llama.cpp and
llamafile keep their own rule (`probeLlamaCppAndCache`: probe only when no entry). Tests:
`ollama_select_probe_test.go`, with the tool-mode carry-over mutation-checked. `MODEL_CACHE.md`
is corrected to match.

Status `proposed`: promotion is the author's call.
