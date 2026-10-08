
## Bugs

- [x] **FIXED 2026-10-08** (`write_offer_input.go`, tests in
  `write_offer_input_test.go`): a line that is a command (`/exit`, `!ls`, any
  registered `/name`) given at a write offer is handed back to the REPL through
  `Agent.pendingInput` and ends all offers; end of input ends them; at the box
  prompt only Enter, y and yes approve (any other text used to count as yes).
  Decided with the author: an absolute path stays re-rooted inside the workspace
  (`resolveWorkspacePath`: `/etc/x` is `<workspace>/etc/x`; `..` escapes and symlinks
  are refused), which already keeps the real `/etc` and the workspace parent out of
  reach, so no extra refusal is needed. Non-terminal sessions keep their write
  offers, which is useful for automated tests; revisit when the Oberon-style
  frontend lands (event-driven REPL brief). Original report: **The write-offer
  prompt takes the next input line as a file path and
  writes it.** Found 2026-10-07 piping `Say hello...` then `/exit` into harvey:
  the reply held a fenced block tagged `bash:testout/hello.bash`; the "Write ...?
  Path (or Enter to skip)" prompt read `/exit` as the path and Harvey printed
  `wrote 0 bytes to /exit`, creating an empty `exit` file in the workspace root
  (an absolute path was mapped inside the workspace). Two faults: a line meant as
  a command answered the prompt, and `/exit` was accepted as a path. Reproduce
  through the real prompt with piped stdin; expect no file written and a
  refusal of an absolute path. Also check the prompt's non-interactive default.
- [x] **FIXED 2026-10-07** (`ErrStreamTruncated`, exit class io; tests in
  `chat_stream_failure_test.go`). **A failed or truncated chat stream shows as an empty reply, not an
  error.** Found on harvey.local 2026-10-06 (kb observation 367): when
  hailo-ollama closes `/api/chat` with no data (llama3.2:3b past its prompt
  limit), harvey prints nothing, records a 0-token reply, and the `--debug` log
  shows `llm_response` with no error. Reproduce with an `httptest` server that
  closes a chunked response early, through the real chat path; expect an error
  the user sees and a recorded failure.
- [x] **FIXED 2026-10-07.** `--ollama` and `-m/--model` were overridden by `agents/harvey.yaml`; the flags now set `Ollama.URLExplicit`/`ModelExplicit`, which `LoadHarveyYAML` honours. Tests: `cli_override_test.go`, `cmd/harvey/cli_override_test.go`. Original report: 
  `flagSpecs` in `cmd/harvey/main.go` sets `cfg.Ollama.URL`/`Model`, then `Run`
  (`terminal.go:407`) calls `LoadHarveyYAML`, which overwrites both when
  `ollama.url`/`ollama.model` are set (`config.go:850-855`). Seen: `harvey
  --ollama http://localhost:8001` still connected to `:8000`, and `harvey -m
  qwen2.5-coder:1.5b` ran llama3.2:3b (kb observations 368, 371). The command
  line should win over the file.
- [x] **FIXED 2026-10-07.** The real cause was not the template markers alone:
  hailo-ollama's `/api/show` returns `"model_info": ""`, which made `ShowModel`
  fail to decode, so the probe never ran and the mode stayed `auto` with tools
  unknown. `ShowModel` now tolerates it, and `details.format == "hef"` probes
  as no tool support (the server 500s on any `tools` key, even `[]`). Tests:
  `hailo_probe_test.go`. Original report: **Tool support is read from the
  template, so hailo-ollama models get a `tools` array they cannot take.** hailo-ollama returns HTTP 500 for any
  `/api/chat` carrying `tools`. With llama3.2:3b in tool mode `auto`, harvey
  sent `tools` and every prompt failed with `provider_error: 500` until
  `/model mode prose` was set by hand (kb observations 365, 371). The likely
  cause is `FastProbeModel` trusting the template's tool markers; confirm that
  first, since the cached row showed `supports_tools` unknown. The probe should
  catch this, either by recognising the server or by a live tool test, and
  fall back to prose.
- [x] **FIXED 2026-10-06.** Switching to an Ollama model mid-session left the
  transcript naming the old model and stopped `--debug` logging LLM requests
  (kb observation 372; seen in `harvey-session-20261006-204135.spmd`). Cause:
  `setOllamaModel` (picker and `/model use NAME`'s fallback match), both `ollama`
  branches of `attemptModelSwitch`, and the Ollama `Restore` after a dispatched
  step each built a new client without the debug log; the picker path and
  `Restore` also wrote no `[[model switch]]` note. All four now go through
  `Agent.useOllamaClient`, which sets the model, wires the debug log and records
  the switch. Tests: `model_switch_ollama_test.go`, red first, through `/model use` and
  `resolveDispatchTarget`. Not covered: `startLlamaCppModelPath` also installs a
  client without the debug log or a switch note; untested and unchanged.

- [x] **FIXED 2026-09-25.** `getting_started.md` was stale (21 registered commands missing, `/llamafile` and `/ollama` documented though no longer registered, `/kb` subcommands short). Command tables now come from the registry; the `/kb` registry entry itself omitted `learn`, `source`, `retract`, `cite`, `show` and `check-retractions` and is fixed (`kb_registry_test.go` keeps it in step with `cmdKB`).
- [x] **FIXED 2026-09-25.** `/model use NAME` only searched the registry and aliases, so an Ollama model could be reached only through the picker. It now falls back to every model `/model list` shows: exact name (case-insensitive), then a unique prefix; an ambiguous prefix lists the candidates. Tests: `model_use_name_test.go`.
- [x] **FIXED 2026-09-25.** (Original report below.) Cause: `nomic-embed-text` was not installed, and `MemoryStore.Save` wrote the memory file before embedding, so each failed save left an orphan `.fountain` file. `Save` now embeds first, writes nothing on failure, restores any prior file if the index write fails, and the error says to run `ollama pull MODEL`. Tests: `memory_save_atomic_test.go`. Two orphans from the report remain on disk in `agents/memories/tool_use/` (`tool_use_b39dd1.fountain`, `tool_use_760a04.fountain`); delete by hand.

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Connected: Apertus (llamafile)
  /help for commands · /exit to quit
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  25 session(s) unmined — /memory mine to extract learnings
  RAG has 514 chunk(s) but is off — /rag on to enable context injection
harvey > /memory list
project_fact_019f37             project_fact      -               0.5  safe_mode default allowed_commands now includes kb and man
workspace_profile_29074f        workspace_profile  -               0.5  Data Scientist — Laboratory
project_fact_84e77b             project_fact      -               0.5  Project: Laboratory
project_fact_4f8e21             project_fact      pattern         1.0  harvey/INSTALL.md is hand-maintained, not cmt-generated; installer.sh/ps1 removed (pre-release, no binary distribution yet)
harvey > /memory mine
Extracting memories from /home/rsdoiel/Laboratory/agents/sessions/harvey-session-20260924-143905.spmd …
LLM proposed 1 memory candidate(s). Starting review…

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Proposed memory 1 of 1
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Type:        tool_use
 Kind:        pitfall
 Description: Never use the '+' character in filenames to avoid shell parameter expansion issues.
 Action:      Use '-' or '_' instead of '+' in filenames.
 Tags:        shell, filenames, parameters
 Summary:     The '+' character in filenames triggers parameter expansion, leading to incorrect file handling by shells. This is a permanent shell behavior.

[a]ccept  [e]dit  [s]how similar  [r]eplace <id>  [f]ull view  [k]skip  [q]uit
> a
Error saving memory: memory store: save: embed: ollama embed: HTTP 404: {"error":"model \"nomic-embed-text:latest\" not found, try pulling it first"}

Done. Accepted: 0  Skipped: 0
harvey > /memory list
project_fact_019f37             project_fact      -               0.5  safe_mode default allowed_commands now includes kb and man
workspace_profile_29074f        workspace_profile  -               0.5  Data Scientist — Laboratory
project_fact_84e77b             project_fact      -               0.5  Project: Laboratory
project_fact_4f8e21             project_fact      pattern         1.0  harvey/INSTALL.md is hand-maintained, not cmt-generated; installer.sh/ps1 removed (pre-release, no binary distribution yet)
harvey > /memory mine
Extracting memories from /home/rsdoiel/Laboratory/agents/sessions/harvey-session-20260713-171056.spmd …
LLM proposed 1 memory candidate(s). Starting review…

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Proposed memory 1 of 1
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 Type:        tool_use
 Kind:        pitfall
 Description: Always use the chunk-size parameter when processing large files with Qwen3.5-4B-Q5_K_S.
 Action:      Increase the chunk-size parameter when processing large files.
 Tags:        model, processing, chunk-size
 Summary:     Files over 800 characters without chunk-size cause errors. This is a permanent model quirk.

[a]ccept  [e]dit  [s]how similar  [r]eplace <id>  [f]ull view  [k]skip  [q]uit
> a
Error saving memory: memory store: save: embed: ollama embed: HTTP 404: {"error":"model \"nomic-embed-text:latest\" not found, try pulling it first"}

Done. Accepted: 0  Skipped: 0
harvey > /memory mine
Extracting memories from /home/rsdoiel/Laboratory/agents/sessions/harvey-session-20260706-172458.spmd …
```

- [x] **FIXED 2026-09-25.** `bufio.NewReaderSize(x, 1)` was used in six places to read a line without reading ahead, with comments claiming a one-byte buffer. `bufio` enforces a 16-byte minimum, so it swallowed up to 15 bytes of following input (verified). `newLineReader` (in `model_picker.go`) reads one byte per call and replaces all six sites. Tests: `line_reader_test.go`, mutation-checked.

- [x] **FIXED 2026-09-25: stale `/ollama` and `/llamafile` references.** Neither command is registered (`/model` replaced them), but about 30 places told users to run them. Now: runtime messages in `commands.go`, `commands_rag.go`, `commands_skill.go`, `plan_cmd.go`, `terminal.go`, `backend_startup.go` point at `/model use` or a shell `ollama` command; `helptext.go` (status, rag, inspect) and the generated pages `harvey-inspect/rag/status.7.md` follow; `harvey-model.7.md`, `-model-alias`, `-routing` hand-patched; `README.md`, `CONFIGURATION.md`, `ARCHITECTURE.md`, `models.md`, `model_guide.md`, `MODEL_CACHE.md`, `ROUTING.md`, `reference.md`, `user_manual.md` corrected. `harvey-llamafile.7` and `harvey-ollama.7` (.md and .html) are deleted: they documented commands that no longer exist. The `/ollama use|probe` tab completion moved to `/model alias add ALIAS MODEL`. `stale_commands_test.go` fails if any non-test Go file names either command again. Left as history: `CHANGES.md`, `DECISIONS.md`, the `*-design.md` and `*-plan.md` files, the release notes in `README.md`/`about.md`.
- [x] **FIXED 2026-09-25: Ollama models were probed only when an alias was saved.** A model chosen any other way (startup picker with the alias prompt declined, `/model use NAME`) had no cache entry, so `toolsReliable()` answered false and Harvey silently injected file contents instead of using tools; the alias-time probe also erased a `/model mode` override (`Set` replaces the row). `setOllamaModel` now probes on every selection through `probeOllamaModelAndCache`, carrying the tool mode over; no `/model probe` command (`ollama_select_probe_test.go`, DR-0004 `proposed`). `MODEL_CACHE.md` rewritten to match. **Update 2026-09-27:** `ThoroughProbeModel` now has one caller — see DR-0006 below.

- [x] **DONE 2026-09-27 (harvey DR-0006): closed out DR-0005's two deferred items, plus two more loose ends.**
  - `termlib` bumped to the `v0.0.9`+`354195d` pseudo-version (no `v0.0.10` tag exists; tagging it is optional, left to RSDOIEL). **Update 2026-09-27: RSDOIEL tagged and released termlib v0.0.10; `go.mod` re-bumped from the pseudo-version to the real tag, `go build`/`go vet`/`go test ./...` clean.**
  - `ThoroughProbeModel` wired into `/rag setup`'s embedder auto-pick (`confirmEmbedder` in `commands_rag.go`): the keyword guess is confirmed or corrected with one live `/api/embed` call before the store commits to it. Not wired into every `setOllamaModel` selection (rejected — extra request on every switch for a signal only RAG setup uses).
  - `--json` (`harvey.PrintJSONError`, `harvey.ExtractJSONFlag` in `exitcode.go`): both binaries accept `-json`/`--json` anywhere on the line and print a failing startup/flag-parsing error (or, for `harvey`, a failed non-interactive session) as `{"error","class","code"}` JSON on stderr, matching `kb`'s shape.
  - Scripted `harvey` exit codes: a non-interactive session (`keepFirstFailure` in `terminal.go`) now exits with the class of its first failed slash command or chat turn instead of always 0. `plan_cmd.go`'s two unclassified errors reclassified (`Unavailablef`, `Negativef`) as the first command family this covers.
  - **Follow-up DONE 2026-10-02:** every slash command handler now returns a classed error (phases 1-5: dispatcher; file/workspace; model/session/settings; memory/KB/RAG/skills/routes; the rest), checked by `exitcode_handlers_test.go` including two guards (a registry walk for unknown subcommands, a source scan for print-and-continue). Upgrade table in `CHANGES.md`. Remote ingestion (`ragIngestHTTP`, `ragIngestRemotePrefix`, `ragIngestS3Prefix`) now returns its first failure too, so `/rag ingest` with any source exits non-zero when something could not be ingested. Nothing from this pass is open.

- [x] **DONE 2026-09-27: adopted the workspace exit-code convention** (workspace DR-0003; the `kb` implementation is knowledge DR-0047 to DR-0049; harvey DR-0005). `cmd/harvey` and `cmd/assay` now exit 0 ok, 1 negative, 2 usage, 65 data, 66 no_input, 69 unavailable, 70 internal, 73 cant_create, 74 io, 75 temp_fail, 77 no_permission, 78 config — `exitcode.go`, `EXIT STATUS` in both manuals, `flags_test.go` enforcement. `--json` mode/error class was scoped out (neither binary has a `--json` mode yet).
  **Survey done 2026-09-25:** `exit-codes-survey.md` (baseline of 41 table rows, 20 harvey and 21 assay, decisions Q1-Q5, plan H0-H6). Plan: `exit-codes-plan.md`. **H0-H5 done 2026-09-25**, committed. **H6 done 2026-09-27**: `scripts/compare-exit-codes.py` + `scripts/README.md` (old-vs-new harness, modelled on `knowledge`'s, closed-port backend, `pgrep ollama` guard); run against the v0.0.16 baseline (`df39cfe`, via a throwaway `git worktree`) — 32 commands changed exit code, every one matching the survey/DR-0005 table exactly, no exit 70, no hang, no stray Ollama process. `harvey/decisions/0005-*.md` written and **accepted** by RSDOIEL. `CHANGES.md` `## Unreleased` has the upgrade table.

## Action Items

- [ ] **Shell completion with an install option, as in antenna.** `harvey completion bash|powershell [-install]`,
  following `antennaApp/completion.go` (shipped 2026-10-05; `InstallCompletion` is the install half).
  This is the *shell* completion for the `harvey` binary's own verbs and flags (`init`, `help`, `-h`, ...
  in `cmd/harvey/main.go`). It is separate from the in-REPL `/command` tab completion in
  `design/tab-completion-design.md`. Harvey follows the exit-code convention, so a bad shell name is 2
  and a failed install write is 73/74. Document it and add a test that keeps the verb list in step with
  `main.go`. Tests first. Same behaviour as antenna: bash goes to the bash-completion user directory and
  never overwrites a file harvey did not write; PowerShell writes beside the profile and adds one
  dot-source line, once.



- [x] **SUPERSEDED 2026-09-28 (harvey DR-0007): not implemented.** This item's own reason for buffering to JSONL — that classifying a scene EXT (LLM-triggered shell) vs INT (REPL-contained) needs a look at the whole session — stopped holding up once picked up: `RecordExteriorTurn` (`recorder.go`) already ships `EXT.` for a different, existing meaning (remote-endpoint routing), and the LLM-triggered-vs-REPL-typed distinction this item wanted is already expressed live, per call, with no buffering: `RecordShellCommand` (the REPL `!` path) emits `INT. SHELL`; LLM-triggered actions emit `INT. AGENT MODE` via `StartAgentScene`/`RecordAgentAction`. See `harvey:DR-0007` for the full reasoning, including the other reasons (crash-safety, decoupling from Fountain) considered and declined.

- [x] **DONE, shipped in v0.0.16.** Fully integrate the updates to the knowledge model, Harvey should support a learning mode that integrates both human, model and hybrid dialogs for evaluation, summarization, concept tagging and re-ingest for the knowledge base.
- [x] **DONE, shipped in v0.0.16.** I've evolved the development methodology since last working on Harvey. The knowledge took kb has been updated to reflect those changes. Harvey repo needs to be brought into alignment with the new practrices around design decision reviews and recording them in a decisions directory that kb can be used to update the agents knowledge base for the active workspace. This could impact how we treat the knowledge base as a memory reservoir for Harvey, it could also shed light of how we handle boundries between memory layers, documents versus querying SQLite3 database representations, TAGS and the workspace knowledge base

  **Status 2026-09-24 (end of day):** both items above are done and scheduled for **v0.0.16**, per `knowledge-learning-mode-design.md` / `-plan.md`. H0-H7 are done and committed: `/kb learn ingest|draft|review|concepts`, `learn_model` in `harvey.yaml`, `go.mod` at knowledge `fd588ef` (unreleased v0.0.13; RSDOIEL chose to ship on it). Harvey `decisions/`: DR-0001 and DR-0002 accepted, DR-0003 (keep termlib) proposed. The termlib gate is cleared and `harvey/CLAUDE.md` is fixed. What remains before the tag is the release process itself (below), which is RSDOIEL's step.

- [x] **FIXED 2026-09-24. `promptAction` treated end of input as "yes".** Found while building `/kb learn concepts`: Ctrl-D or a closed pipe at a "Write: PATH" box wrote the file. `promptAction` now returns `(choice, ended)` and every caller treats `ended` as quit (`/kb learn concepts`, and both write prompts in `autoExecuteReply`); a bare Enter is still yes. The same pass found that the tagged-block path in `autoExecuteReply` never checked `CheckWritePermission` (only the untagged fallback did), so a read-only path could be written after a yes; it is now refused before the prompt. 8 red-first tests in `commands_test.go`; one older test had encoded the bug ("Empty input → Enter → yes") and now sends a real Enter.

- [ ] **Design spike: AI HAT+ 2 (Hailo-10H) as a fourth Harvey backend.**
  Blocked on the hardware arriving — a Raspberry Pi 5 16GB, the AI HAT+ 2
  and a 256GB SD card (OS and models) were ordered 2026-10-02 and have not
  yet arrived (parts list: `../Harvey-Project-Parts-List.txt`). When it
  does, start by testing the models listed in *AI Projects with Raspberry
  Pi* (Hattersley & Jepson, 2026; `../AI_Pi_Projects_Lessons.md`).
  `hailo-ollama` (the HAT's local server) exposes `/api/pull` and
  `/api/chat` on port 8000 with the same streaming JSON shape
  (`done`/`eval_count`/`done_reason`) as Ollama's own API on 11434 — the
  spike is to confirm how close that compatibility actually is and
  whether it can reuse `backend_ollama.go` pointed at a different
  URL/port, or needs its own `backend_hailo.go` alongside
  `backend_llamafile.go`/`backend_llamacpp.go`. Model choice is
  constrained to Hailo's curated set (deepseek_r1_distill_qwen:1.5b,
  llama3.2:3b, qwen2.5-coder:1.5b, qwen2.5-instruct:1.5b, qwen2:1.5b — all
  smaller than Harvey's current CPU-only lineup), so this is an
  offload/speed play, not a capability upgrade. See `agents/knowledge.db`
  project `harvey`, observations 423 (original near-drop-in-backend idea),
  444 (unrelated dual-model arbitration hypothesis, same session), 445
  (book review this spike is drawn from), and 446 (parts list).

  **Status 2026-10-06:** hardware arrived and set up as harvey.local. First
  answer (kb observation 370): for chat, `backend_ollama.go` works unchanged
  against hailo-ollama on `:8000`, so no `backend_hailo.go` is needed to get
  started. The gaps (tools, embeddings, tokenize, prompt size, catalog) are
  capability awareness inside the Ollama path, tracked in the items below and
  under Bugs. DR-0029 stays proposed until those are settled.

- [ ] **Mining on small models.** `/memory mine` now reads a large session in parts (2026-10-08), but the
  instructions (`minerSystemPrompt`, about 2.3 KB) alone exceed the 700 token seed for llama3.2 on Hailo, so that
  model is reported as unable to mine. Write a compact instruction variant for models with a small known limit.
  Also: hailo-ollama wedged (empty replies, even to "Say hi.") after a run of failed requests during the first live
  chunking test and needed `systemctl --user restart hailo-ollama` (kb 378); check whether the miner's failed
  whole-session attempt, or its request rate, triggers it, and consider remembering the size that worked per
  model so the first attempt is not a known failure. `/memory mine` with no path still takes only the newest
  unmined session per call.
- [ ] **Confirm `/model` works end to end against hailo-ollama.** hailo-ollama
  0.5.1 runs as a user systemd unit on `:8000`, and `agents/harvey.yaml` points
  `ollama.url` at it. Chat works; the rest of `/model` has not been checked.
  Walk every `/model` subcommand (`list`, `use`, `show`, `status`, `stop`,
  `clean`, `mode`, `alias` and the picker) and record what works, what fails
  and how. Known so far (kb observations 365, 367, 369):
  - **Listing.** `/model list` shows only pulled models (`/api/tags`); see the
    catalog item below.
  - **Tools.** Any `/api/chat` carrying `tools` gets HTTP 500; see Bugs.
  - **Embeddings, tokenize, ps.** `/api/embed`, `/api/tokenize` and
    `POST /api/ps` return 404. Check what `/model status`, `stop` and
    `CountTokens` do when these endpoints are missing.
  - **Content type.** Every POST must carry `Content-Type: application/json`,
    or oatpp returns 500 "No suitable mapper".
  - **Prompt-size limit.** llama3.2:3b fails past about 3,300 characters of
    system prompt; see Bugs and the prompt-size item below.
  For each of the five curated models (deepseek_r1_distill_qwen:1.5b,
  llama3.2:3b, qwen2.5-coder:1.5b, qwen2.5-instruct:1.5b, qwen2:1.5b), record
  in the model cache whether it handles prose tools, tagged blocks and
  embeddings, and measure its prompt-size limit. Bugs found go under Bugs,
  test-first.

- [x] **DONE 2026-10-07 (DR-0030 step 4).** `/model list` and the pickers show the catalog as `not pulled`; `/model pull [--yes] NAME` pulls from it. Ollama pulls stay with the ollama CLI (DR-0022). Original item: **Show and pull the Hailo catalog from `/model`.** hailo-ollama lists the
  models the HAT can run at `GET /hailo/v1/list` and pulls them with
  `POST /api/pull` (Ollama-style progress stream). Harvey only reads
  `/api/tags`, so a model that is not yet pulled cannot be seen or fetched
  from inside harvey (kb observation 369). Detect a hailo-ollama server (for
  example by `/hailo/v1/list` answering), show unpulled catalog entries in
  `/model list` and the picker marked as such, and decide whether
  `/model pull NAME` belongs in harvey for both Ollama and hailo-ollama.

- [ ] **Per-model prompt-size limits.** Measured 2026-10-07 (kb 377): the cap is
  on the WHOLE prompt (system + history + new message), not the system prompt;
  llama3.2:3b on hailo-ollama passes at 3,000 characters and fails at 3,300 with
  HTTP 500 "read failed". Harvey's own prompt is 6,249. Three layers, in order:
  - [x] **Layer 1, 2026-10-07.** `ModelCapability.MaxPromptTokens` in the model
    cache (chars/4 units, 0 = unknown), seeded at 700 for hailo llama3.2,
    kept across re-probes, set with `/model limit`. Startup warns (exit 65 when
    unattended) and each turn is refused before sending. Tests:
    `prompt_limit_test.go`.
  - [x] **Layer 2, 2026-10-07.** The system prompt shrinks to 60% of the limit,
    least essential layer first: the skills catalog, then the tail of HARVEY.md
    (cut at a paragraph break and marked), then a compact preamble, then all of
    HARVEY.md (`prompt_fit.go`). `refreshSystemPrompt` runs at startup and every
    turn, so any model switch is covered, and `/clear` and the plan/pipeline
    commands use the same fitted text. Before each send, `trimHistoryToBudget`
    drops the oldest whole turns (never the latest, the system message or pinned
    context). Both print what they cut. Tests: `prompt_fit_test.go`. Side effect:
    `/clear` now expands HARVEY.md's dynamic sections as startup does; it used to
    re-inject them unexpanded.
  - [x] **Layer 3, 2026-10-07.** A turn that fails like an oversize prompt (a cut
    stream, a context-length error, a provider HTTP 500; not a refused connection
    or a missing model) and whose prompt is at least 200 tokens is retried on
    smaller copies of the conversation, at 70%, 45% and 25% of the failed size
    (`retryWithSmallerPrompt`, `prompt_fit.go`). The first success replaces the
    history with the smaller copy and records its size as the model's limit; if
    none succeeds nothing changes and the original error is shown. Failures on
    hailo-ollama are fast and only a success is slow, which is why a ladder is
    affordable. Not done: the tool-loop path (`RunToolLoop`) does not retry.
    Tests: `prompt_learn_test.go`. The tests caught a panic: a successful retry
    swaps in a shorter history, so `histLenBeforeChat` had to be re-captured.
  Related: a client that abandons a request wedges hailo-ollama until it is
  restarted (kb 378); a request that outlasts `ollama.timeout` does this.

- [x] **DONE 2026-10-08 (RAG, a profile memory save and `/memory mine` verified).** `ollama.url` is now `:11434`, `hailo.url` is `:8000`, `nomic-embed-text` is pulled; a scratch `/rag new`, `/rag ingest` and `/rag query` embedded through Ollama while chat stayed on `hailo`; `/memory profile update` stored a 768-dim embedding. `/memory mine` verified on Ollama (mined, saved, embedded); see the miner fix below. Original item: **Embeddings on harvey.local.** hailo-ollama has no `/api/embed` and none
  of the curated models embeds, so RAG and memory saves fail against it. Install
  regular Ollama on `:11434` alongside, pull `nomic-embed-text`, and confirm
  that RAG stores and the memory store can embed through it while chat goes to
  the HAT. Today `NewEmbedderForEntry` is handed `cfg.Ollama.URL`, so this may
  need a separate embedder URL in `harvey.yaml` (as `EmbedderURL` already
  exists for encoderfile).

## Update next

- [x] **Blocked on `knowledge`** — **resolved 2026-09-15.** Decided
  2026-09-08 (see `agents/knowledge.db`, project `harvey`, observation id
  398) to hold the release until `kb ingest`'s bugs and `[[wikilink]]`
  tagging settled. Both shipped in `knowledge` v0.0.4–v0.0.6
  (`wikilink-tagging`, `concept-tag-retrieval`, `narrative-documents` — see
  `../knowledge/CHANGES.md`), and `go.mod` had no local `replace` for
  `knowledge` by the time this was checked — it was already a plain tagged
  dependency. Bumped `go.mod` `github.com/rsdoiel/knowledge` v0.0.3 → v0.0.6;
  `go build`/`go test ./...` clean. Also rewired `memory_unified.go`'s
  `recallKB` to try `kb.MatchConceptNames`/`RecallByConceptNames` first
  (concept-tag match, not project-scoped, widened to `records` and
  `reviewed`-status `document` sections), falling back to the original
  project-scoped substring scan over `observations` when no concept matches
  — item 1–2 of `knowledge-learning-mode-feature-request.md`. TDD: 6 new
  tests in `memory_unified_test.go`, confirmed red before implementation.
  **Still open:** items 3–6 of that feature-request doc (the learning-mode
  UX itself, session-to-document ingestion, decisions-directory
  realignment) — untouched by this change.

  **Update 2026-09-17:** re-bumped `go.mod` `github.com/rsdoiel/knowledge`
  v0.0.6 → v0.0.9 (`v0.0.7` ingest/search bug fixes, `v0.0.8` `index --check`
  + `observation update` correction path, `v0.0.9` `project rename`/
  `concept rename` + cross-machine last-writer-wins + `kb index --all` — see
  `../knowledge/CHANGES.md`). `go build ./...` and `go test ./...` clean, no
  API changes touched harvey's four consumers (`harvey.go`,
  `commands_kb.go`, `memory_unified.go`, `terminal.go`); `knowledge.go`/
  `knowledge_merge.go` no longer exist in harvey (fully extracted into the
  module already). `go test -race ./...` could not run on this machine
  (Pi: "ThreadSanitizer: unsupported VMA range, Found 47 - Supported 48") —
  a platform limitation, not something this bump caused; unverified under
  the race detector until run on hardware ThreadSanitizer supports. Items
  3–6 of the feature-request doc remain open and untouched.

  **Update 2026-09-23:** re-bumped v0.0.9 → v0.0.11 (`v0.0.10` `concept
  suggest`/`document tag`/density-linking/`project rename` for record-owning
  projects; `v0.0.11` `document fuzzy-tag`/`frontmatter`, fuzzy clustering,
  `kb search` punctuation fix). `go build`, `go vet`, `go test ./...` clean,
  no code changes needed; race detector still unrunnable on this Pi. Items
  3–6 are now designed: see `knowledge-learning-mode-design.md` /
  `-plan.md` and `decisions/0001`–`0002` (all `proposed`). Track B of that
  plan is gated on `knowledge` v0.0.12 (`../knowledge/library-lift-plan.md`).

- [x] **DONE 2026-09-24: evaluated the `github.com/rsdoiel/termlib` dependency; decision is to keep it for v0.0.16.** Requested 2026-09-15 after the stale `go.mod` `replace` was found. Full facts and options in `termlib-evaluation.md` (Harvey's whole use is 6 lines in `terminal.go`; termlib is the author's own small module with one dependency; Charm would add about 30 packages and a rebuild of the line editor). RSDOIEL chose "keep termlib"; recorded as `decisions/0003-*.md` (accepted). `repl-charm-migration-design.md` had wrongly said the drop was already decided; corrected there. **Update 2026-09-29:** termlib `v0.0.10` (which carries `354195d`, a wide/multi-row prompt fix that does not affect Harvey's one-row prompt) is tagged and `go.mod` requires it. Charm stays a later, separate effort.

- [x] Release readiness — **v0.0.16 is tagged and published (checked 2026-09-25); the tag is left as is and later fixes go forward into the next release. `go.mod` now pins knowledge v0.0.13. The note below is history.** Original: **prep done 2026-09-15, tagging/publishing still
  open, now also gated on the termlib evaluation above.** All three prep
  items from the original note are done: (1) stale
  `replace github.com/rsdoiel/termlib => ../termlib` removed from `go.mod`
  — `go mod tidy` confirmed the build against the real tagged `v0.0.9`, no
  local checkout needed; (2) version bumped `0.0.15a` → `0.0.16`; (3)
  `releaseNotes` written covering everything since `v0.0.15` and propagated
  to `CHANGES.md`/`about.md`/`CITATION.cff`/`version.go` via `cmt`. **Note:**
  `cmt codemeta.json README.md` was run but reverted — it discards
  hand-curated content (Security Note, Features, Quick Start, Documentation
  index) that root `CLAUDE.md`'s "regenerated, never hand-edited"
  categorization doesn't account for; `README.md` was hand-patched instead
  (version/date/notes only). If `cmt codemeta.json README.md` is ever run
  again, diff it before committing — same class of gotcha as the `knowledge`
  repo's Makefile (see root `CLAUDE.md`'s "Note for knowledge"). **Update 2026-09-24:** the termlib gate is cleared (above); release notes now cover the learning mode, the write-prompt and permission fixes and the `/kb search` hyphen fix (hand-patched in `codemeta.json`, `CHANGES.md`, `about.md`, `README.md`; `cmt` was not run). RSDOIEL chose to ship on the knowledge pre-release commit `fd588ef` (v0.0.13 is not tagged), so `go.mod` requires a pseudo-version and the notes say so. **Still
  open:** the actual release process
  (`make release` to cross-compile `dist/*.zip`, then `release.bash` to tag
  `v0.0.16`, push, and create the draft GitHub release) — deliberately not
  run automatically since it pushes commits and creates a public (draft)
  release.

- [x] Cross-machine `knowledge.db` sync — **DONE 2026-07-27**. Steps 1 (UUID migration) and 2 (merge tool) were
  already built; this session applied both, fixed two real bugs discovered along the way
  (`concepts.created_at`, `experiments`→`projects`), and completed a genuine merge, placed on both machines.
  Sequence: pulled both `~/Laboratory` and `~/Laboratory/harvey` up to date on `macmini-rd.local` over SSH (no
  conflicts — checked incoming commits against macmini's one unpushed local commit first); backed up and
  migrated macmini's real live `agents/knowledge.db` (both fixes, verified identical to the earlier copy-based
  test); `scp`'d it to wren; ran `bin/kbmerge -a <wren> -b <macmini> -force` locally; independently verified the
  merged output (`PRAGMA integrity_check` clean, zero dangling join rows, all UUIDs present) before placing it
  anywhere; backed up wren's pre-merge live db; replaced wren's live `agents/knowledge.db` with the merged file;
  `scp`'d that same file to replace macmini's. Confirmed byte-identical (checksum) on both machines afterward.
  Result: 5 projects (`harvey`/`henry`/`antennaApp`/`sparqlset`/`audiobox`), 46 concepts, 181 observations, no
  data lost. See the 2026-07-27 "Cross-machine `knowledge.db` merge completed" entry in `DECISIONS.md`.
  Backups on both machines: `.pre-cleanup-20260727`, `.pre-experiments-migration-20260727` (macmini),
  `.pre-merge-20260727` (wren). **Not done:** macmini's root `Laboratory` repo has an unpushed merge commit
  (`e418c8e`) from the `git pull` step — ask before pushing. **Next, if wanted:** knowledge-base module
  extraction (own repo/`go.mod`, step 3) and JSON-L export (deferred, step 4) — full sequencing in
  `../knowledge_db_merge_design.md`. Full resume context also recorded as a `note` observation in
  `agents/knowledge.db` (project `harvey`, concept `cross-machine-sync`, most recent entry).

## Bugs

- [x] macmini-rd.local's `agents/knowledge.db` was still on the pre-`harvey` `experiments`/`experiment_id` schema
  (documented historically in root `CLAUDE.md`, predating `harvey/knowledge.go`, which has used `projects`/
  `project_id` since its first commit) — 93 real observations and 35 concepts across 4 real projects were
  invisible to the app, and `observations` had no `project_id` column at all. **Fixed 2026-07-27:**
  `migrateExperimentsToProjects` in `knowledge.go`, called from `OpenKnowledgeBase`; no-op if no legacy
  `experiments` table exists (confirmed true for every other reachable `knowledge.db`). See
  `experiments-migration-design.md`/`-plan.md` and `DECISIONS.md` same date. TDD: five new tests in
  `knowledge_test.go`, three confirmed red before implementation. Verified against the real copy of macmini's
  database and via a full successful `bin/kbmerge` run. **Still open:** apply to macmini's actual live database
  (only the copy was migrated/tested).

- [x] `concepts` table missing `created_at` on any `knowledge.db` created before that column was added to the
  base schema DDL (found on macmini-rd.local's real database 2026-07-27, via a genuine cross-machine
  `bin/kbmerge` attempt — `kbAlterStmts` had no `ALTER` for it, unlike every other column added after the
  original `CREATE TABLE`). **Fixed 2026-07-27:** added the missing `ALTER TABLE concepts ADD COLUMN created_at
  DATETIME` (no `DEFAULT CURRENT_TIMESTAMP` — SQLite rejects a non-constant `ADD COLUMN` default on any
  non-empty table, confirmed against real `sqlite3`, not just the Go driver) plus an idempotent one-time
  `UPDATE ... WHERE created_at IS NULL` backfill. TDD: `TestOpenKnowledgeBase_BackfillsLegacyConceptsCreatedAt`
  (`knowledge_test.go`) reproduces macmini's exact legacy shape. See `DECISIONS.md` same date. **Still open:**
  macmini's actual live `agents/knowledge.db` needs this fix applied via its own `git pull` + reopen — only a
  copy was fixed/tested here.

- [x] 24 of 101 `observation_concepts` rows in `agents/knowledge.db` (this machine) reference `observation_id`
  values that no longer exist in `observations` — dangling links, found 2026-07-26 while manually verifying the
  merge tool (see `DECISIONS.md`, same date, "Cross-machine `knowledge.db` merge tool" entry). Likely a historical
  delete that ran on a connection without `PRAGMA foreign_keys=ON` active (SQLite enforces FKs per-connection, not
  persistently in the file) — e.g. a raw `sqlite3` CLI session, since `OpenKnowledgeBase` itself pins a single
  connection (`db.SetMaxOpenConns(1)`) and sets the pragma on every open, so the current Go code path was never
  the source. Not caused by, and didn't affect the correctness of, the UUID migration or merge tool —
  `MergeKnowledgeBases`'s join-based copy already excludes unresolvable links rather than propagating them.
  **Fixed 2026-07-27 (on `wren`):** backed up the live file to
  `agents/knowledge.db.pre-cleanup-20260727`, then ran `DELETE FROM observation_concepts WHERE observation_id NOT
  IN (SELECT id FROM observations)` — removed exactly 24 rows. Also checked the other five parent/join
  combinations (`observation_concepts`→`concepts`, `project_concepts`→`projects`/`concepts`,
  `observation_sources`→`observations`/`sources`) — all were already clean, so no further cleanup was needed.
  This machine's `agents/knowledge.db` only; `macmini-rd.local`'s copy was not touched and should be checked
  separately.

- [x] Remove the prompt to remove previous session at startup (we have a `-resume` and `/resume` option if needed) —
  already fixed in commit `9e3e13b` (2026-07-12, bundled into an earlier "Quick Save" commit, not checked off at
  the time). `pickSession`'s interactive "Resume a prior session? [y/N]" prompt was removed from `Run()`
  (`terminal.go`); `--continue`/`--resume` CLI flags (`cmd/harvey/main.go`) and `/resume`, `/session
  use|continue|replay` (`commands.go`) are the confirmed, working replacement.

- [x] I have both Llamafile and gguf models in ~/Models on my Mac, bit the gguf models are not listed as an option
  (llama.cpp is installed) — fixed 2026-07-13. Root cause: `pickBackend` (`backend_startup.go`), the combined
  startup picker used whenever any llamafile is registered, built its options list from registered llamafiles +
  disk-scanned unregistered llamafiles + live Ollama models — with no code path for `.gguf`/llama.cpp models at
  all. `/model list`/`/model use` (`aggregateModels`) already handled all three backends correctly; the startup
  flow had never been brought in line with that later unification. Fixed by adding a disk-scan branch (mirroring
  the existing llamafile one) plus a `"llamacpp"` option kind that starts the model via the already-existing
  `startLlamaCppModelPath`. See DECISIONS.md 2026-07-13 entry. Test: `TestPickBackend_ListsGGUFModels`.

- [x] Chunk prompt never triggered for Gemma4-E4B — root cause found and fixed
  2026-07-05, see [DECISIONS.md](DECISIONS.md) (2026-07-05 — Chunking guard fix).
  Two bugs: `remainingContext()` returning 0 for "unknown limit" was treated the
  same as "skip the guard" in `builtin_tools.go`'s `read_file` (now falls back
  to a 4096-token budget, matching `file_inject.go`); and `adoptExternalServer`
  never probed context length for llamafile models adopted from an
  already-running server, so `effectiveContextLimit()` stayed 0 for the whole
  session. Tests: `TestReadFile_ChunkingEnabledContextLimitUnknown`,
  `TestAdoptExternalServer_probesContextLength`.

- [x] Llamafile GPULayers defaulted to 99 (maximise GPU) on every platform,
  including Raspberry Pi hardware with no usable GPU-compute backend. This is
  the actual explanation for the `bonsai-8b` (Q1_0) retest below appearing to
  hang for 20+ minutes — the underlying `llama-server` process was still
  running after 2+ hours of CPU time. Fixed 2026-07-05: default changed to 0
  (CPU-only), matching `LlamaCppConfig.GPULayers`'s existing default. See
  DECISIONS.md 2026-07-05 entry. Tests:
  `TestDefaultConfig_LlamafileGPULayersDefaultsToZero`,
  `TestSaveLlamafileConfig_DoesNotPersistDefaultGPULayers`,
  `TestSaveLlamafileConfig_PersistsCustomGPULayers`.

- [x] Chunk-quality retest against the actual Gemma4-E4B model — RESOLVED
  2026-07-05. Downloaded `gemma-4-E4B-it-Q5_K_M.llamafile` (7.4GB) from
  huggingface.co/mozilla-ai/llamafile_0.10 (no longer dependent on the
  `henry` build pipeline). Ran `/read-chunks natural_language_programming.md
  --chunk-size 800 --max-chunks 20 [topic-drift instruction]` — 23 chunks,
  stopped after 4 completed (user time constraints). All 4 chunks were
  coherent, on-topic, and did genuinely useful paragraph-level drift
  analysis — a stark contrast to the original garbled-token bug report.
  **Conclusion: the map-reduce chunking approach itself is sound.** The
  original hallucination was entirely explained by the chunk-prompt guard
  never firing (TODO items above), not model coherence collapse under the
  chunking prompt. Per-chunk pace: ~10 min/chunk at 800-byte chunks,
  CPU-only (`-ngl 0`, confirmed via `ps`), 377–385% CPU utilization — genuinely
  computing, not hung. 23 chunks would extrapolate to ~4 hours total,
  consistent with an overnight/unattended run being the intended use case.
  Full per-chunk output is preserved in
  `agents/sessions/harvey-session-20260705-205110.spmd` (chunks 1-4) even
  though the run was killed before synthesis.

- [x] Benchmark per-chunk timing across candidate models, now that GPULayers
  defaults to 0. No other model has been timed with the GPU-layers fix in
  place — the earlier `bonsai-8b` 20+ min "hang" was confounded by the
  GPULayers=99 bug and is not valid timing data. Use `/read-chunks PATH
  --chunk-size 800 --max-chunks 2` (or 3) on the same test document across
  models to get a fast, comparable per-chunk time without committing to a
  full run. Candidates on disk in `~/Models/` as of 2026-07-05:
  `OpenELM-3B-Instruct-Q4_K_M`, `Qwen3.5-4B-Q5_K_S`, `gemma-4-E2B-it-Q5_K_M`
  (smaller Gemma4 sibling, needs `chmod +x`), `Bonsai-8B-Q1_0` (retest —
  previous timing invalid), `Apertus-8B-Instruct-2509`,
  `granite-4.1-8b-source-Q4_K_M`, plus `gemma-4-E4B-it-Q5_K_M` (~10 min/chunk
  baseline from today). Goal: build a real per-model-per-chunk timing table
  to answer "which model fits a given overnight/unattended time budget on a
  Pi 500."

  **Update 2026-08-08:** Started the real per-model run (Claude Code
  session, not manually at the terminal) — a standalone throwaway Go
  program (`chunkbench`, built against this module via a local `go.mod`
  `replace`, not checked in anywhere) that calls `ChunkDocument` +
  `LlamafileBackend.Start`/`NewClient` + `client.Chat` directly per model,
  sequentially, timing 2 chunks each against `natural_language_programming.md`
  (12711 bytes, `--chunk-size 800` → 23 total chunks, confirms the same
  chunking as the 2026-07-05 run). Stopped by user request partway through
  (3 of 7 models attempted) to free up the Pi; resume by rerunning the
  remaining models below. Confirmed via `ps` that the actual server
  invocation carries `-ngl 0 -c 16384` as configured — the GPULayers fix is
  genuinely in effect for this run, unlike every prior timing attempt.

  Results so far (context_length as configured in `harvey.yaml`):
  - `gemma-4-E4B-it-Q5_K_M` (ctx 16384): chunk 1 = 4m40s, chunk 2 = 4m36s
    (avg ~4m38s/chunk). Notably faster than the "~10 min/chunk" figure
    quoted above — that figure was a rough estimate from the real 23-chunk
    run, not a tight back-to-back 2-chunk measurement; treat ~4.5 min/chunk
    as the more reliable number for this model at this chunk size.
  - `gemma-4-E2B-it-Q5_K_M`: **no valid data** — both chunks errored with
    `connection refused`, and the backend reported "server ready in 0s"
    (immediate, suspicious). Root cause: a genuine race in the benchmark
    script, not a Harvey bug — `LlamafileBackend.Start` adopts an
    already-listening server instead of launching a fresh one (by design,
    for the case of an externally-started server), and the script called
    `Stop()` on the previous model then `Start()` on this one with no gap;
    the prior process's SIGINT hadn't yet released port 8080, so Start
    wrongly "adopted" the dying gemma-4-E4B server, which then actually
    exited moments later. **Needs a Detect()-poll-until-down guard between
    Stop() and the next Start()** before re-running E2B or trusting any
    future back-to-back sequential benchmark script — worth fixing in the
    script (not this package) since real interactive `/llamafile use`
    switches are user-paced, not back-to-back-instant.
  - `Qwen3.5-4B-Q5_K_S` (ctx 16384, the exact model/config this TODO item's
    "Update 2026-07-25" entry above wanted re-verified): chunk 1 = **8m21s**
    — nearly 2x `gemma-4-E4B`'s per-chunk time even though both ran at the
    identical `context_length: 16384`. This is a real, moderately
    surprising data point: it means the earlier "large configured context
    inflates CPU-only KV-cache setup cost" theory is **not** the full
    explanation for Qwen's slowness, since 16384 here is already the
    smallest context of any model tested and it's still the slowest —
    something about this specific model/quant is just inherently heavier
    per token on this CPU. Chunk 2 was in progress (interrupted by the
    stop request) — re-run to get a second data point and confirm chunk 1
    wasn't an outlier (e.g. one-time warmup cost).

  **Not yet run:** `Bonsai-8B-Q1_0` (ctx 65536), `OpenELM-3B-Instruct-Q4_K_M`
  (ctx 16384), `granite-4.1-8b-source-Q4_K_M` (ctx 16384),
  `Apertus-8B-Instruct-2509` (ctx 49152) — all still queued, in that order,
  in the `chunkbench` script's model list.

  **Update 2026-08-08 (completed):** Re-ran `chunkbench` (v2, fixed: a
  `waitForPortFree` poll-until-down guard between each model's `Stop()` and
  the next model's `Start()`, closing the race that invalidated
  `gemma-4-E2B`'s first attempt) for the 6 remaining/retry models. All
  completed cleanly; full table below (2 chunks each, `--chunk-size 800`,
  CPU-only `-ngl 0`, confirmed via `ps` on every model this time):

  | model | context | avg/chunk | extrapolated, full 23-chunk doc |
  |---|---|---|---|
  | `Apertus-8B-Instruct-2509` | 49152 | 1m51s | ~42 min |
  | `granite-4.1-8b-source-Q4_K_M` | 16384 | 2m19s | ~53 min |
  | `gemma-4-E2B-it-Q5_K_M` | 16384 | 2m24s | ~55 min |
  | `Bonsai-8B-Q1_0` | 65536 | 3m36s | ~83 min |
  | `gemma-4-E4B-it-Q5_K_M` | 16384 | 4m38s | ~107 min |
  | `OpenELM-3B-Instruct-Q4_K_M` | 16384 | 6m13s | ~143 min |
  | `Qwen3.5-4B-Q5_K_S` | 16384 | 7m51s | ~180 min |

  **Conclusion: parameter count does not predict per-chunk speed on this
  CPU.** The two fastest models (`Apertus`, `granite`) are both 8B-class;
  the smallest model tested (`OpenELM`, 3B) is the second-slowest, beaten
  only by `Qwen3.5-4B`. Quantization scheme/architecture dominates raw
  size for CPU-only inference here. `Qwen3.5-4B-Q5_K_S` is now confirmed
  slowest across three independent chunk measurements (8m21s, 7m0s,
  8m41s — consistently ~7-8.5 min/chunk), at the *smallest* context length
  of any model tested, which rules out "large configured context inflates
  KV-cache cost" as an explanation for its historical slowness; something
  about this specific model/quant is inherently heavier per token here.
  For an overnight/unattended full-document run on this Pi 500,
  `Apertus-8B-Instruct-2509` is the clear best fit (~42 min vs. up to 3
  hours for `Qwen3.5-4B-Q5_K_S`).

  See `agents/knowledge.db` (Laboratory root), project `harvey`, concept
  `chunking`, for the same summary as a finding observation.

- [x] Added `/read-chunks PATH [--chunk-size N] [--max-chunks N] [--overlap
  paragraph|sentence|none] [INSTRUCTION...]` — runs the chunked map-reduce
  pipeline directly, with no overflow-threshold check, and lets chunk-size/
  overlap/max-chunks be swept per-invocation independent of harvey.yaml.
  See DECISIONS.md 2026-07-05 entry. Tests in `read_chunks_cmd_test.go`.

- [x] Known remaining gap: `startAndUseLlamafile` (`backend_startup.go`) adopts
  an already-running server under a detected model name without registering/
  probing a matching `LlamafileEntry` when that name differs from the
  configured active entry — same class of bug as the fixed `adoptExternalServer`
  case, narrower scope. Fixed 2026-07-13: `useLlamafileEntry`'s result is now
  checked, and when no `LlamafileEntry` exists yet for the adopted name, one is
  registered (empty `Path`, matching `adoptExternalServer`'s own precedent —
  the adopted server's actual model file path is unknown) with a probed
  `ContextLength`. See DECISIONS.md 2026-07-13 entry. Test:
  `TestStartAndUseLlamafile_AdoptedDifferentName_RegistersEntry`.

- [x] `/read-chunks` doesn't fail fast when the llamafile backend is
  unreachable (e.g. the server died after cancelling a prior prompt). Found
  2026-07-06 via `agents/logs/harvey-20260706-172458.jsonl`: every chunk in
  the map phase fired its own "connection refused" to `localhost:8080` and
  was recorded as a per-chunk failure (by design — a chunk failure doesn't
  abort the map phase), then the run only actually errored out at the
  synthesis call. On a multi-chunk document this burns through the whole
  file before surfacing what is really a single root-cause problem. Fixed
  2026-07-13: a new `probeClientReachable` helper (`chunk_analyzer.go`)
  dispatches on the client's `ProviderName()` (ollama/llamafile/llamacpp use
  their existing local health probes; cloud providers and test doubles are
  skipped, `checked=false`) and is called once at the top of
  `RunChunkedAnalysis` — fixing all three chunk-analysis call sites
  (`cmdReadChunks`, `injectOrChunk`, `read_file`'s guard) through their
  existing error-handling paths, no per-call-site change needed. See
  DECISIONS.md 2026-07-13 entry. Test:
  `TestRunChunkedAnalysis_FailsFastWhenBackendUnreachable`.

- [x] Debug log records each chunk's LLM call twice during `/read-chunks` — fixed 2026-07-13.
  Root cause confirmed as described: `RunChunkedAnalysis` (`chunk_analyzer.go`) logged every chunk/synthesis call
  itself while `AnyLLMClient.chatInternal` (`anyllm_client.go`) already logs the same call internally. Fix was not
  a simple "drop the caller-side calls," though: `chatInternal`'s own `DebugLog` field is nil for any freshly-
  constructed client (`resolveDispatchTarget`'s route-registry and local-switch branches both build fresh
  `*AnyLLMClient`s that were never wired), so naively removing the caller-side logging would have silently dropped
  logging entirely for `@mention`-dispatched chunk analysis instead of fixing a duplicate. Fixed in two parts:
  (1) `resolveDispatchTarget` (`dispatch_target.go`) now wires `DebugLog` onto whatever client it resolves, for
  all three of its branches; (2) the now-redundant `dbg *DebugLog` parameter was removed entirely from
  `RunChunkedAnalysis`, along with all its internal logging calls. See DECISIONS.md 2026-07-13 entry. Tests:
  `TestResolveDispatchTarget_RouteEndpoint_WiresDebugLog`, `TestResolveDispatchTarget_LocalSwitch_WiresDebugLog`.
  **Related finding, fixed separately 2026-07-13:** `builtin_tools.go`'s `read_file` pre-read chunking guard had
  the same cosmetic-only `@mention` bug already fixed elsewhere for `/read-chunks`/`injectOrChunk` during Direction
  D (Bug 1 in `subagent-dispatch-design.md`) — it parsed `@mention` to relabel `ChunkAnalysisParams.Model` but
  always dispatched via `a.Client`, never the mentioned model. Missed during that earlier work (only two of the
  three chunk-analysis call sites were found at the time); now fixed the same way, via `resolveDispatchTarget`.
  See DECISIONS.md 2026-07-13 entry. Test: `TestReadFile_MentionDispatchesToNamedModel`.

- [x] `Qwen3.5-4B-Q5_K_S`'s `/read-chunks` chunk 1 ran 54+ minutes (interrupted
  by user 2026-07-06, still pegged at ~389% CPU when stopped — genuinely
  computing, not hung) versus the ~10 min/chunk baseline already measured for
  `gemma-4-E4B-it-Q5_K_M`. Original theory: this entry's `harvey.yaml`
  `context_length` was 180224 at the time, vs. 16384–65536 for the other
  registered models — a much larger configured context can inflate CPU-only
  KV-cache setup/compute cost regardless of actual chunk size.

  **Update 2026-07-25:** confirmed via `gguf-dump` against the GGUF embedded
  in `Qwen3.5-4B-Q5_K_S.llamafile` (extracted with `unzip`, since llamafile is
  a zip-appended APE binary, not a raw GGUF) that the model's own *trained*
  context length is `qwen35.context_length = 262144` — even larger than the
  180224 this item suspected, and far above the 16384–65536 range of every
  other registered model. This makes the KV-cache-allocation theory more
  plausible, not less: llama.cpp/llamafile allocate KV cache proportional to
  the configured `-c` value at server startup (`ActiveLlamafileContextLength()`
  → `StartLlamafileService`, see `backend_llamafile.go`), independent of
  actual prompt/chunk size, so a `-c` anywhere near this model's native max
  would explain slow, CPU-bound startup cost.

  Also confirmed: `agents/harvey.yaml`'s registered entry for
  `Qwen3.5-4B-Q5_K_S` **already reads `context_length: 16384`** — the exact
  value this item proposed testing. No record exists of when or why it was
  changed (no matching git history in this repo — `git log -p -S 180224`
  returns nothing), so this may already be applied but never re-benchmarked.
  Remaining step: run `/read-chunks PATH --chunk-size 800 --max-chunks 2`
  against the current (16384) config to confirm chunk time now falls in line
  with the ~10 min/chunk baseline, then fold into the per-model timing table
  above. Not run yet — this is a multi-minute, CPU-heavy operation and
  deserves an explicit go-ahead rather than running unattended.

  **Resolved 2026-08-08**, by the completed per-model benchmark above: at
  `context_length: 16384` — the exact config this item wanted verified —
  `Qwen3.5-4B-Q5_K_S` measured 8m21s, 7m0s, and 8m41s per chunk across three
  independent runs (~7-8.5 min/chunk, consistent). This confirms the 16384
  setting already fixed the original 54+ minute outlier, but **not** the
  KV-cache-allocation theory itself: 16384 is the smallest context of any
  model benchmarked, yet `Qwen3.5-4B-Q5_K_S` is still the single slowest
  model tested (nearly 2x the next-slowest, `OpenELM-3B`). The remaining
  slowness is model/quant-specific, not context-length-driven — no further
  action planned here.

