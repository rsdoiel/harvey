# Harvey — Blank-Slate Startup Ignores a Persisted `llamafile.active` — Design & Fix

**Status (2026-07-03):** Fixed. Test-first: `TestLoadHarveyYAML_IgnoresPersistedLlamafileActive`
added in red state, then the fix applied to `LoadHarveyYAML` in `config.go`
and confirmed green. Full suite (`go test ./...`, `go vet ./...`) clean
except one unrelated pre-existing failure (`TestCmdModelList_ShowsLlamafileEntries`,
confirmed to fail identically on `main` without this change).

---

## Background

Originally reported in `TODO.md`: "When I started harvey 0.0.15a... it
jumped straight into a previous model instead of started as blank slate."
The user's own hypothesis was a stale `active:` value in `agents/harvey.yaml`.

A prior "blank slate" fix (see `blank_slate_test.go`) already stopped
`SaveLlamafileConfig` from ever writing `llamafile.active` back to disk on
save — but that only prevents *new* stale values from being created. It
does nothing about `active:` values already present in a file: a legacy
leftover from an older Harvey version, or a hand edit (this workspace's own
`agents/harvey.yaml` had `llamafile.active: bonsai-8b` set by hand earlier
this session). An initial review of the bug concluded it was already fixed
by the save-side change — that was wrong; the load side still had the bug.

## The bug

`LoadHarveyYAML` (`config.go`, ~line 875) copied a YAML file's
`llamafile.active` straight into `cfg.Llamafile.Active`:

```go
if y.Llamafile.Active != "" {
    cfg.Llamafile.Active = y.Llamafile.Active
}
```

`cmd/harvey/main.go` (~line 107) sets the **same field** when the user
passes `--llamafile PATH` on the command line:

```go
case "--llamafile":
    ...
    cfg.Llamafile.Active = harvey.LlamafileModelNameFromPath(p)
```

`terminal.go` (~line 458) then can't distinguish the two sources — it treats
any non-empty `Config.Llamafile.Active` as a deliberate hint and skips the
interactive model picker:

```go
hint := sessionModel
if hint == "" && a.Config.Llamafile.Active != "" {
    hint = a.Config.Llamafile.Active
}
```

So a YAML file with `active:` set — whether from an old Harvey version or a
hand edit — auto-selects that model at startup exactly as if `--llamafile`
had been passed, even though the user never asked for that this run.

## Design intent (confirmed with the user)

Harvey should **always** show the interactive model picker at startup,
letting the user choose which model to use for that session. The only
things that should skip the picker are:

- An explicit `--llamafile PATH` CLI flag for that specific run.
- A `--continue`/`--resume` session hint (`sessionModel`, extracted from the
  actual session file being resumed) — legitimate resumption, not a stale
  config value.

A persisted `llamafile.active` in `harvey.yaml` should never be one of
those triggers.

## Fix

Removed the `LoadHarveyYAML` block that copied `y.Llamafile.Active` into
`cfg.Llamafile.Active`. The CLI flag path in `cmd/harvey/main.go` is
untouched and continues to work as the deliberate override. Session-resume
hints are untouched (separate `sessionModel` mechanism). No test asserted
the old load-side behavior, so nothing legitimate broke.

```go
// Blank slate: a persisted llamafile.active (legacy leftover or hand edit)
// must never auto-select a model at startup. Only an explicit --llamafile
// CLI flag (cmd/harvey/main.go) or a --continue/--resume session hint may.
```

## Test (test-first)

`TestLoadHarveyYAML_IgnoresPersistedLlamafileActive` (`config_test.go`):
writes a `harvey.yaml` with `llamafile.active: bonsai-8b` and a registered
model, calls `LoadHarveyYAML`, and asserts `cfg.Llamafile.Active` is empty
while `cfg.Llamafile.Models` still loads correctly. Confirmed red against
the pre-fix code, green after.

## Consequence for this workspace

`agents/harvey.yaml` still has `llamafile.active: bonsai-8b` left over from
earlier this session — it's now inert (ignored on load) but harmless to
leave in place. `bonsai-8b` remains registered and selectable via the
picker or `/model use bonsai-8b`; it just no longer auto-starts.
