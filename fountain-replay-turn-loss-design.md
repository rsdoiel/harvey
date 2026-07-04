# Harvey — Fountain Replay/Continue Turn-Loss Bug — Design & Fix

**Status (2026-07-03):** Fixed. Test-first: `replay_test.go` added the
regression test in red state first (confirmed via `go test -run
TestParseFountainSession_TwoExchangesInOneScene`), then the fix below was
applied to `parseFountainSession` in `replay.go` and confirmed green,
alongside two guard tests for the cases that must keep working. Full suite
(`go test ./...`, `go vet ./...`) clean except one unrelated pre-existing
failure (`TestCmdModelList_ShowsLlamafileEntries`, confirmed to fail
identically on `main` without this change).

---

## Background

Discovered while live-testing `bonsai-8b` through Harvey's `--replay` path. A
hand-authored `.spmd` test file with two consecutive `RSDOIEL` dialogue
blocks (no intervening reply) under one scene heading produced only 1
replayed turn instead of 2 — the model answered a follow-up question
("double that number") with no context of what number that was.

Initial read was "malformed test file, not a real bug" — but tracing
`parseFountainSession` against an actual, already-committed session file
showed the same data loss occurs in genuine Harvey-recorded sessions, not
just hand-authored ones. See Evidence below.

## The bug

`parseFountainSession` (`replay.go`) walks a Fountain document's elements and
accumulates one reused `PlaybackTurn` struct, `cur`, as it goes:

```go
case fountain.DialogueType:
    ...
    switch {
    case lastChar == userName:
        cur.UserInput = text
    case lastChar == "HARVEY" && strings.HasPrefix(text, "Forwarding to "):
        ...
    case lastChar != "" && lastChar != "HARVEY" && lastChar != userName:
        cur.ModelReply = text
    }
```

`cur` is pushed onto the `turns` slice in exactly two places:

1. When a **new scene heading** matching `INT. HARVEY AND {USER} TALKING...`
   is encountered (the previous `cur` is pushed before starting a fresh one).
2. At **end of file**.

There is no push when a *second* `{USER}` dialogue block appears **within
the same scene**, after a first exchange has already completed. Because
`cur` is a single reused struct, the second exchange's `UserInput` and
`ModelReply` silently overwrite the first exchange's — no warning, no error,
just quiet data loss. Whatever was in `cur` right before the file ends (or
the next scene heading starts) is the only turn `parseFountainSession`
reports for that scene.

This affects two callers:

- **`ReplayFromFountain`** — turns before the last one in an affected scene
  are silently never resent.
- **`ContinueFromFountain`** — turns before the last one in an affected scene
  are silently missing from the resumed conversation history.

## Evidence this is a real bug, not a malformed-input artifact

`harvey/agents/sessions/harvey-session-20260502-mozilla-ai-integration.spmd`,
lines 191–210, is a genuine, already-recorded session with **one scene
heading followed by two complete exchanges**:

```
191  INT. HARVEY AND RSDOIEL TALKING — 2026-05-02 19:15
...
195  RSDOIEL
196  Let's do phase 2.
198  CLAUDE
199  Phase 2 scope confirmed. Will expand RouteKind to all any-llm-go providers,
     ...
203  RSDOIEL
204  I am going to hit token limits. We'll continue phase 2 later. Please update
     ...
207  CLAUDE
208  Knowledge base initialized and populated.
     ...
```

Tracing the parser against this exact scene: `cur.UserInput` is set to
"Let's do phase 2." then `cur.ModelReply` to "Phase 2 scope confirmed...";
then the second `RSDOIEL` block **overwrites** `cur.UserInput` with "I am
going to hit token limits..." and the second `CLAUDE` block overwrites
`cur.ModelReply` with "Knowledge base initialized...". The first exchange —
"Let's do phase 2." and its reply — is completely lost. `--continue` on this
file today would silently resume history missing that exchange; `--replay`
would silently skip resending it.

## Why interactive (stdin/stdout) mode is unaffected

Interactive REPL use never calls `parseFountainSession` at all. Each line
read from stdin is immediately treated as one complete turn, sent straight
to `a.Client.Chat()`, with the reply appended to history before the next
input is read — the turn boundary is the read-eval-print cycle itself, not
a document structure being reconstructed after the fact. This class of bug
is structurally confined to `--replay` and `--continue`.

## Proposed fix

In the `lastChar == userName` branch, detect that the current turn is
already complete (`cur.UserInput != "" && cur.ModelReply != ""`) before
assigning the new `UserInput`. If so, push the completed `cur` onto `turns`
and start a fresh one — mirroring the exact push-on-boundary logic already
used for scene-heading transitions, just adding a second boundary condition:
"a new user line after a completed exchange, even within the same scene."

```go
case lastChar == userName:
    if cur.UserInput != "" && cur.ModelReply != "" {
        turns = append(turns, cur)
        cur = PlaybackTurn{}
    }
    cur.UserInput = text
```

This is a minimal, localized change — no format change, no new fields, no
new caller-visible behavior beyond correctly recovering turns that were
previously silently dropped.

## Test plan (test-first)

1. Add `replay_test.go`. First test: a regression test built directly from
   the real mozilla-ai-integration scene structure (two exchanges, one scene
   heading) asserting `parseFountainSession` returns **2** turns with the
   correct, unmixed `UserInput`/`ModelReply` pairs. This must fail (red)
   against the current code — confirms the bug is real and the test
   actually exercises it.
2. Apply the fix above.
3. Re-run — test must pass (green). Also add a normal-case regression test
   (one scene heading, one exchange) to guard against the fix breaking the
   common case, and a multi-scene test (today's already-working case) to
   guard the existing scene-heading-transition push path.
