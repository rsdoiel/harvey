
# Action Items

## Update next


## Bugs

- [ ] Benchmark per-chunk timing across candidate models, now that GPULayers
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

- [x] Added `/read-chunks PATH [--chunk-size N] [--max-chunks N] [--overlap
  paragraph|sentence|none] [INSTRUCTION...]` — runs the chunked map-reduce
  pipeline directly, with no overflow-threshold check, and lets chunk-size/
  overlap/max-chunks be swept per-invocation independent of harvey.yaml.
  See DECISIONS.md 2026-07-05 entry. Tests in `read_chunks_cmd_test.go`.

- [ ] Known remaining gap: `startAndUseLlamafile` (`backend_startup.go`) adopts
  an already-running server under a detected model name without registering/
  probing a matching `LlamafileEntry` when that name differs from the
  configured active entry — same class of bug as the fixed `adoptExternalServer`
  case, narrower scope. See DECISIONS.md 2026-07-05 entry.

- [ ] `/read-chunks` doesn't fail fast when the llamafile backend is
  unreachable (e.g. the server died after cancelling a prior prompt). Found
  2026-07-06 via `agents/logs/harvey-20260706-172458.jsonl`: every chunk in
  the map phase fired its own "connection refused" to `localhost:8080` and
  was recorded as a per-chunk failure (by design — a chunk failure doesn't
  abort the map phase), then the run only actually errored out at the
  synthesis call. On a multi-chunk document this burns through the whole
  file before surfacing what is really a single root-cause problem. Add a
  cheap preflight reachability probe (e.g. `ProbeLlamafile`/equivalent for
  the active backend) at the top of `cmdReadChunks`/`RunChunkedAnalysis` so
  an unreachable backend fails immediately with one clear message instead of
  N per-chunk ones.

- [ ] Debug log records each chunk's LLM call twice during `/read-chunks`.
  `RunChunkedAnalysis` (`chunk_analyzer.go:100,116-118,141-163`) calls
  `dbg.LogLLMRequest`/`LogLLMResponse` itself around every `client.Chat`
  call, but `AnyLLMClient.chatInternal` (`anyllm_client.go:162,202`) logs the
  same request/response a second time internally — so
  `agents/logs/*.jsonl` gets two near-identical `llm_request` lines (and two
  `llm_response`/error lines) per actual HTTP call, not two real requests.
  Cosmetic, but it'll skew any tooling built on top of the debug log,
  including the per-chunk timing benchmark tracked above. Fix: only one of
  the two call sites should log — likely drop the caller-side logging in
  `chunk_analyzer.go` since `chatInternal` already logs unconditionally for
  every `Chat`/`ChatWithTools` call.

- [ ] `Qwen3.5-4B-Q5_K_S`'s `/read-chunks` chunk 1 ran 54+ minutes (interrupted
  by user 2026-07-06, still pegged at ~389% CPU when stopped — genuinely
  computing, not hung) versus the ~10 min/chunk baseline already measured for
  `gemma-4-E4B-it-Q5_K_M`. The likely difference: this entry's
  `harvey.yaml` `context_length` is 180224, vs. 16384–65536 for the other
  registered models — a much larger configured context can inflate CPU-only
  KV-cache setup/compute cost regardless of actual chunk size. Unconfirmed;
  needs a controlled retest with the Qwen entry's context_length temporarily
  lowered (e.g. to 16384) to see if chunk time drops accordingly before
  folding Qwen into the per-model timing table above.


