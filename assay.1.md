%assay(1) user manual | version 0.0.20 cd480d8
% R. S. Doiel
% 2026-10-20

# NAME

assay

# SYNOPSIS

assay [OPTIONS]

# DESCRIPTION

assay is an LLM evaluation harness for Harvey. It runs a corpus of prompts
against one or more Ollama models and produces a
Markdown report plus a JSON results file for human review and automated
checking.

The prompt corpus is defined in a YAML file (default:
agents/assay/corpus.yaml). Each entry specifies a category, a prompt,
automated checks (contains, not_contains, compiles, go_vet), and
human-review questions. assay sends each prompt to each model, records
the response, runs all automated checks, and writes a summary table plus
per-prompt results to the output directory.

With -guide-compare and -guide-file, every prompt is run twice (once with
no system message, once with the guide file's content as a system message)
and a per-check delta table is appended to the report — useful for testing
whether an always-on guide (a HARVEY.md section, a skill's prose) still
changes model behavior once a computational sensor exists for the same
failure mode.

# OPTIONS

-corpus PATH
: Path to the corpus YAML file.
  Default: agents/assay/corpus.yaml

-models MODEL[,MODEL...]
: Comma-separated list of Ollama model names to evaluate.
  Default: all models currently available on the Ollama server.

-category NAME
: Run only prompts in the named category. Omit to run all categories.

-ollama URL
: Base URL of the Ollama server.
  Default: http://localhost:11434

-output PATH
: Directory to write the report (report.md) and results (results.json).
  Default: $WORKSPACE/assay-results/assay-TIMESTAMP/ if run inside a
  Harvey workspace, or assay-results/assay-TIMESTAMP/ otherwise.

-guide-file PATH
: Path to a plain text file whose content becomes a system message sent
  to the model on the "guide" variant of a -guide-compare run. Ignored
  unless -guide-compare is set.

-guide-compare
: Run each prompt twice — once with no system message (identical to
  assay's default dispatch) and once with -guide-file's content sent
  as a system message — and append a per-check delta table to the report.
  Requires -guide-file.

-h, -help, --help
: Display this help message.

-v, -version, --version
: Display version information.

-json
: On a failing command line or run, print the error as
  {"error","class","code"} JSON on stderr instead of text. Wherever -json
  appears on the line, it still applies to whatever else fails.

# EXIT STATUS

assay does everything it can and then exits with the class of the first
thing that failed, using the workspace's exit codes:

0
: The run finished. Prompts whose automatic checks failed are results, not
  failures, and exit 0.

1
: Nothing to run: no prompts match -category, or the server has no models.

2
: The command line is wrong: an unknown option, an option without its value, a
  surplus argument, or options that conflict (-guide-compare without -guide-file).

65
: A file assay read is wrong: a corpus that will not parse or has no
  prompts.

66
: A named input is missing: the -corpus or -guide-file file.

69
: A service cannot be reached: Ollama while listing models, or a model call
  that failed. The run still
  finishes, writes its report, and says how many calls failed.

70
: An internal error, or one nothing classified. Please report it.

73
: The output directory cannot be created.

74
: report.md or results.json could not be written. The other is still tried.

77
: The operating system refused access to a file or directory.

With -json, an error prints as {"error","class","code"} JSON on stderr
instead of text; class and code are the same class name and number as the
table above.

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

Compare a run with and without a guide file as the system message:

~~~
  assay -models llama3.1:8b -guide-file HARVEY.md -guide-compare
~~~

Write results to a custom directory:

~~~
  assay -models granite3-moe:3b -output testout/granite-run/
~~~

# SEE ALSO

harvey(1)
