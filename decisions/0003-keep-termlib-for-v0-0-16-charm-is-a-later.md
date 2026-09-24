---
id: "0003"
title: "Keep termlib for v0.0.16; Charm is a later, separate effort"
date: "2026-09-24"
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
uuid: "01a0d506-14f5-700c-94ee-72d6608c09e9"
origin_host: "wren"
---

**Context.**

`TODO.md` gated the v0.0.16 tag on an evaluation of whether `github.com/rsdoiel/termlib`
should be replaced. The trigger was the 2026-09-15 discovery of a stale `replace` directive in
`go.mod`, since fixed. `repl-charm-migration-design.md` had described a decision to drop termlib
for Charm as already made; it had not been. The evaluation is in `termlib-evaluation.md`: Harvey's
whole use is six lines in `terminal.go`, termlib is a small module with one dependency, and moving
to `bubbletea` would add about 30 third-party packages to Harvey's build.

**Decision.**

Harvey v0.0.16 keeps `termlib` unchanged. A move to Charm is a separate, later effort with its own
design and release, not part of this one. `DR-0001` item 3's deferral of a bubbletea menu therefore
continues to hold.

**Rationale.**

The only reason termlib was questioned is fixed. The line editor is the component every keystroke
passes through and the hardest to unit-test, so replacing it belongs in a release of its own rather
than beside the largest feature since v0.0.15.

**Rejected alternatives.**

- Widget-level swap to Charm now (Option A of the design brief): about 30 new packages and a rebuild
  of history, word-completion, multi-line input and the `$EDITOR` handoff, in the same release as
  learning mode.
- Full REPL rearchitecture on Bubble Tea (Option B): a rewrite of most of the interactive surface;
  a separate project.
- Vendoring termlib's line editor into Harvey: no behavior gain, and it leaves termlib without a
  consumer.

**Consequences.**

`termlib` stays at `v0.0.9` until the author tags a release containing `354195d` ("fix prompt and
input bug", which does not affect Harvey's one-row prompt today). The Charm question stays open and
is revisited once learning mode has been in use. Status `proposed`: promotion is the author's call.
