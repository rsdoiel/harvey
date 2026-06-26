%assay(1) user manual | version 0.0.14 3708851
% R. S. Doiel
% 2026-06-21

# NAME

assay

# SYNOPSIS

assay [OPTIONS]

# DESCRIPTION

assay is an LLM evaluation harness for Harvey. It runs a corpus of prompts
against one or more Ollama models (or a llamafile binary) and produces a
Markdown report plus a JSON results file for human review and automated
checking.

The prompt corpus is defined in a YAML file (default:
agents/assay/corpus.yaml). Each entry specifies a category, a prompt,
automated checks (contains, not_contains, compiles, go_vet), and
human-review questions. assay sends each prompt to each model, records
the response, runs all automated checks, and writes a summary table plus
per-prompt results to the output directory.

When a RAG store is provided via -rag-db, assay embeds each prompt and
injects the top-k retrieved chunks as context before calling the model.
With -rag-compare, every prompt is run twice (once without RAG, once with)
and a per-check delta table is appended to the report.

# OPTIONS

-corpus PATH
: Path to the corpus YAML file.
  Default: agents/assay/corpus.yaml

-models MODEL[,MODEL...]
: Comma-separated list of Ollama model names to evaluate.
  Default: all models currently available on the Ollama server.

-category NAME
: Run only prompts in the named category. Omit to run all categories.

-llamafile PATH
: Path to a llamafile binary to evaluate. assay starts the llamafile
  server automatically before the run and stops it when finished.
  The model name is derived from the binary filename.
  Cannot be combined with -models.

-ollama URL
: Base URL of the Ollama server.
  Default: http://localhost:11434

-output PATH
: Directory to write the report (report.md) and results (results.json).
  Default: $WORKSPACE/assay-results/assay-TIMESTAMP/ if run inside a
  Harvey workspace, or assay-results/assay-TIMESTAMP/ otherwise.

-rag-db PATH
: Path to a Harvey RAG store (SQLite). When set, assay embeds each
  prompt and prepends retrieved context chunks before calling the model.

-rag-top-k N
: Number of RAG chunks to retrieve per prompt when -rag-db is set.
  Default: 3

-rag-embed-model MODEL
: Ollama embedding model used to embed prompts for RAG retrieval.
  Default: nomic-embed-text

-rag-compare
: Run each prompt twice — once without RAG context and once with — and
  append a per-check delta table to the report. Requires -rag-db.

-h, -help, --help
: Display this help message.

-v, -version, --version
: Display version information.

# OUTPUT

assay writes two files to the output directory:

report.md
: Markdown report containing a summary table (model x prompts x
  auto-pass rate x average tok/s) followed by per-prompt result blocks
  with automated check outcomes and space for human review notes.

results.json
: Machine-readable JSON array of all prompt results, including the full
  model response, individual check outcomes, and timing data.

# EXAMPLES

Run all prompts against all local Ollama models:

~~~
  assay
~~~

Run only the go-crosswalk category against a specific model:

~~~
  assay -models qwen2.5-coder:7b -category go-crosswalk
~~~

Evaluate a llamafile binary:

~~~
  assay -llamafile ~/models/qwen2.5-coder-7b.llamafile
~~~

Run with RAG context injection and comparison:

~~~
  assay -models llama3.1:8b -rag-db agents/rag/harvey.db -rag-compare
~~~

Write results to a custom directory:

~~~
  assay -models granite3-moe:3b -output testout/granite-run/
~~~

# SEE ALSO

harvey(1), harvey-rag(7)
