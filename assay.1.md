%assay(1) user manual | version 0.0.20 cd480d8
% R. S. Doiel
% 2026-10-20

# NAME

assay

# SYNOPSIS

assay [OPTIONS]

# DESCRIPTION

assay is an LLM evaluation harness for Harvey. It runs a corpus of prompts
against one or more models on Ollama and hailo-ollama and produces a
Markdown report plus a JSON results file for human review and automated
checking.

The prompt corpus is defined in a YAML file (default:
agents/assay/corpus.yaml). Each entry specifies a category (the capability
it tests, such as go, sql or tool-call), a prompt, an optional tokens: size
hint (the prompt's approximate size, for probes that test a model's window),
automated checks (contains, not_contains, compiles, go_vet), and
human-review questions. An unknown key, a duplicate or empty id, or a
negative tokens: is an error (a misspelt check would otherwise never run). assay sends each prompt to each model, records
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
: Comma-separated list of models to evaluate, each as service/model:
  ollama/qwen2.5-coder:1.5b or hailo/llama3.2:3b. A name with no service
  is looked up on both servers: on one it runs there, on both it is a usage
  error that names the two choices, on neither it is sent to Ollama.
  Default: every model on each server that answers (a server that does not
  answer is skipped with a note on stderr).

-category NAME
: Run only prompts in the named category (capability). Omit to run all
  categories. A name that matches nothing lists the categories the corpus has.

-ollama URL
: Base URL of the Ollama server.
  Default: http://localhost:11434

-hailo URL
: Base URL of the hailo-ollama server.
  Default: http://localhost:8000

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
: Nothing to run: no prompts match -category, or no server has models.

2
: The command line is wrong: an unknown option, an option without its value, a
  surplus argument, a model name that is on both servers, or options that
  conflict (-guide-compare without -guide-file).

65
: A file assay read is wrong: a corpus that will not parse or has no
  prompts.

66
: A named input is missing: the -corpus or -guide-file file.

69
: A service cannot be reached: neither Ollama nor hailo-ollama answered while
  listing models, a service named in -models is down, or a model call
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

harvey(1), harvey-rag(7)
