# Harvey — llama.cpp CPU Tuning on ARM (Raspberry Pi) — Design & Findings

**Status (2026-07-03):** Implemented. See `backend_llamacpp.go`
(`buildLaunchPlan`) and `config.go` / `config_yaml.go`
(`LlamaCppConfig.PinCPU`). See also the Bonsai-8B Q1_0 addendum at the end of
this document, which **revises** the thread-count guidance below.

---

## Background

`LlamaCppBackend.Start()` (`backend_llamacpp.go`) launches `llama-server` for
Harvey's llama.cpp backend, passing `--model`, `--port`, and optional
`--ctx-size` / `--threads` / `--n-gpu-layers`. Before this change it did not
control the process environment or CPU affinity at all — whatever thread
count and scheduling the OS defaulted to is what Harvey got.

This workspace runs on a Raspberry Pi 500 (BCM2712 SoC, quad-core Cortex-A76,
16 GB RAM) — the same SoC family as the Raspberry Pi 5 described in the
source article below, which reports meaningful token/sec gains from BLAS
library choice and thread-count tuning on that hardware. Given Harvey runs
here directly, it was worth confirming those claims locally before deciding
what to change in `backend_llamacpp.go`, rather than porting the recipe on
faith.

## Benchmark methodology

Built `llama.cpp` (upstream commit `d4cff11`) three ways from the same source
tree, varying only the BLAS backend:

| Build | BLAS backend | Notes |
|---|---|---|
| `build-noblas` | none (stock `ggml-cpu`) | `-DGGML_BLAS=OFF` |
| `build-openblas` | OpenBLAS 0.3.29 (`apt libopenblas-dev`) | `-DGGML_BLAS_VENDOR=OpenBLAS` |
| `build-blis` | BLIS (built from source) | `CFLAGS="-O3 -mcpu=cortex-a76"`, `-DGGML_BLAS_VENDOR=FLAME` |

All three shared `-DGGML_NATIVE=ON -DGGML_LTO=ON`, confirmed at configure
time to select `-mcpu=cortex-a76+crypto+dotprod`.

Benchmarked with `llama-bench` against `openelm-3b-instruct-Q4_K_M.gguf`
(already present locally under `henry/models-cache/`), at `--threads 3` and
`--threads 4` (this machine has 4 logical cores).

First pass ran with no CPU isolation while a web browser was open in another
session. That run showed one anomalous high-variance result (±2.86 tok/s vs
±0.2–0.4 elsewhere) — plausible scheduler contention on a 4-core box with no
slack. Second pass closed the browser and pinned each run with
`taskset -c 0-2` (3 threads) / `taskset -c 0-3` (4 threads); variance dropped
to ±0.04–0.40 across the board with no change in the qualitative conclusions.

## Findings (pinned, final)

| Backend | threads | pp64 (tok/s) | tg64 (tok/s) |
|---|---|---|---|
| No BLAS | 3 | 19.61 ± 0.37 | 4.69 ± 0.06 |
| No BLAS | 4 | 21.89 ± 0.40 | 4.03 ± 0.04 |
| OpenBLAS | 3 | 20.01 ± 0.14 | 4.68 ± 0.06 |
| OpenBLAS | 4 | 22.17 ± 0.19 | 4.03 ± 0.07 |
| BLIS | 3 | 20.05 ± 0.10 | 4.73 ± 0.05 |
| BLIS | 4 | 22.09 ± 0.21 | 4.06 ± 0.04 |

1. **Thread count = cores − 1 wins for generation, and it's model-agnostic.**
   ~4.7 tok/s at 3 threads vs. ~4.0 tok/s at 4 threads — a consistent ~15%
   gap across every backend. Matches the source article's finding.
2. **BLAS backend choice does not measurably affect generation speed at this
   model scale.** No-BLAS / OpenBLAS / BLIS are statistically indistinguishable
   at 4.69 / 4.68 / 4.73 tok/s (3 threads). This diverges from the source
   article's headline 2.73 vs. 2.34 tok/s (BLIS vs. OpenBLAS) claim. Likely
   explanation: llama.cpp's BLAS path only engages for larger batched matmuls
   (a small ~1–10% pp64 edge is visible for having *any* BLAS backend), not
   single-token decode of a quantized model — which is exactly Harvey's
   dominant interactive workload. The article's test model was a much larger
   25B MoE checkpoint; the gap may reappear at that scale, but that wasn't
   tested here.

## Decision

- **Adopt:** thread-count discipline, nested-thread-pool isolation, and CPU
  pinning in `LlamaCppBackend.Start()` — a real, reproduced ~15% win with
  effectively free implementation cost.
- **Do not adopt:** building and maintaining a custom BLIS dependency for
  Harvey or Henry. No measured benefit for the workload that matters
  (interactive single-token decode), for a real ongoing build/maintenance
  cost (BLIS isn't packaged for Debian/Raspberry Pi OS; it has to be built
  from source and kept in sync with `llama.cpp` releases).
- This finding is scoped to ~3B-class quantized models on 4-core Cortex-A76.
  Revisit if Harvey's typical local model size grows substantially (e.g.
  toward the 25B class the source article used).

## Implementation

- New `LlamaCppConfig.PinCPU bool` field (`config.go`), surfaced in
  `harvey.yaml` as `llamacpp.pin_cpu` (`config_yaml.go`). Defaults to `false`
  — opt-in, since `taskset` is Linux-only and a fixed `0..N-1` core range is
  a reasonable default only on a dedicated single-purpose box like this one.
- `LlamaCppBackend.buildLaunchPlan(model, port, resolvedBin, environ,
  findTaskset)` — a pure helper extracted from `Start()` so the composition
  logic is unit-testable without spawning a real process:
  - **Always** sets `BLIS_NUM_THREADS=1`, `OPENBLAS_NUM_THREADS=1`,
    `OMP_NUM_THREADS=1` in the child environment, so a BLAS library
    `llama-server` happens to be linked against can't spawn a nested thread
    pool underneath llama.cpp's own worker threads and contend for the same
    cores. Harmless when no BLAS backend is linked in at all.
  - When `PinCPU` is true and `Threads > 0`, wraps the launch as
    `taskset -c 0-(Threads-1) <llama-server> <args...>`. Falls back silently
    to unpinned if `taskset` isn't on `PATH` (macOS, minimal containers) or
    `Threads` is 0 (no basis for a core range).
  - `GOMP_CPU_AFFINITY` was considered and rejected, per the source
    article: it only binds GNU OpenMP threads, not the raw pthreads
    llama.cpp's own worker pool uses. `taskset` pins the whole process
    (and all its threads) instead.
- `lookupTaskset` (package-level `var func() (string, error)`) is the
  production resolver (`exec.LookPath("taskset")`); tests substitute stubs
  (`tasksetFound` / `tasksetNotFound`) so they don't depend on the test host
  actually having `taskset` installed.

## Tests added (`backend_llamacpp_test.go`)

Written before the implementation (red state confirmed via
`go test -run TestBuildLaunchPlan`, which failed to compile against the
not-yet-existing `buildLaunchPlan`/`pinCPU`):

- `TestLlamaCppBackend_PinCPU_Default` / `_FromConfig` — config → struct field
  wiring.
- `TestBuildLaunchPlan_EnvIsolation` — the three isolation vars are present
  and pre-existing environment entries are preserved untouched.
- `TestBuildLaunchPlan_NoPinning_WhenDisabled` — `PinCPU=false` leaves
  `bin`/`args` unpinned even when `taskset` is available.
- `TestBuildLaunchPlan_Pinning_TasksetFound` — `PinCPU=true`, `Threads=3`
  wraps as `taskset -c 0-2 <server> --model ... --threads 3`.
- `TestBuildLaunchPlan_Pinning_TasksetNotFound_FallsBackUnpinned` — missing
  `taskset` degrades gracefully instead of failing the launch.
- `TestBuildLaunchPlan_Pinning_NoThreads_IsNoop` — `PinCPU=true` with
  `Threads=0` is a no-op (no core range to compute).

All pre-existing `LlamaCppBackend` tests continue to pass unchanged; the one
unrelated failure in the full suite
(`TestCmdModelList_ShowsLlamafileEntries`) was confirmed pre-existing via
`git stash` — it fails identically on `main` without this change.

## Citation

Paulus, W. (2026, April 27). *A Glimpse of Tomorrow's Local AI — Running
Llama on a Pi*. Wolf Paulus' Journal. https://wolfpaulus.com/local_llama/
(accessed 2026-07-03).

This document's benchmark methodology (BLIS build recipe, thread-tuning
experiment design, `taskset`-over-`GOMP_CPU_AFFINITY` reasoning) follows the
above post. The CPU-pinning and thread-isolation findings were independently
reproduced on different hardware (Raspberry Pi 500 vs. the post's Pi 5) and a
different, smaller model (OpenELM-3B-Instruct Q4_K_M vs. the post's Cerebras
Qwen3-Coder-25B); the BLAS-backend-choice finding did **not** reproduce at
this smaller model scale — see Findings above.

---

## Addendum (2026-07-03, same session): Bonsai-8B Q1_0 — thread guidance revised

Prompted by a question about whether PrismML's 1-bit "Bonsai" quantization
(GGML type `Q1_0` — one sign bit per weight plus a shared FP16 scale per
128-weight block, ~1.125 bits/weight) combined with a BLAS backend could fit
models larger than 8B on this hardware. Built and benchmarked
`prism-ml/Bonsai-8B-gguf` (Bonsai-8B, built from Qwen3-8B) via Henry's
existing `models/bonsai-8b.yaml` — no fork or patch needed; `GGML_TYPE_Q1_0`
already has full CPU (including ARM SIMD), CUDA, Metal, and Vulkan kernels
merged into the exact llama.cpp commit `llamafile` 0.10.3 vendors.

**Q1_0 is architecturally orthogonal to BLAS, not complementary.** BLAS
accelerates dense floating-point GEMM; Q1_0's entire value proposition is to
avoid needing that; its kernel does the matmul-equivalent as bit-packed
sign flips (add/subtract), so BLAS backend choice simply does not apply to
this quant type. Benchmarked with the same stock `ggml-cpu` (`build-noblas`)
binary used for the no-BLAS row earlier in this document.

Results (`llama-bench`, taskset-pinned, same methodology as above):

| Model | threads | pp64 (tok/s) | tg64 (tok/s) |
|---|---|---|---|
| Bonsai-8B Q1_0 | 3 | 2.48 ± 0.00 | 2.02 ± 0.01 |
| Bonsai-8B Q1_0 | 4 | 3.30 ± 0.00 | 2.72 ± 0.00 |

Two findings:

1. **Memory footprint claim holds:** 1.07 GiB on disk for an 8.19B-parameter
   model (vs. 1.76 GiB for the 3B Q4_K_M model above) — the ~14x compression
   PrismML claims checks out. This is the real lever for fitting larger
   models in this Pi's 16GB RAM, not any BLAS backend choice.
2. **The "threads = cores − 1" guidance in this document does NOT
   generalize across model/quantization types.** Bonsai-8B prefers all 4
   cores for both pp and tg — the opposite of the 3B Q4_K_M model's
   preference for 3 threads. And despite the far smaller file size,
   Bonsai-8B's generation speed (2.72 tok/s at best) is markedly *slower*
   than the 3B model's (4.03–4.77 tok/s across backends) — total parameter
   count still dominates per-token compute cost even at ~1 bit/weight.
   **Revised guidance:** `Threads` and `PinCPU` should stay per-model, user-
   tunable settings in `LlamaCppConfig` — which they already are — rather
   than ever assuming a fixed default core count is optimal across model
   classes.

**Practical verdict:** Q1_0 solves the *memory* constraint on this hardware,
not the *speed* constraint. A hypothetical >8B 1-bit model would likely
still fit in RAM, but based on this measurement, expect generation speed to
keep dropping as parameter count rises, not improve. PrismML has not
publicly shipped anything larger than Bonsai-8B as of this writing.
