package harvey

import "os"

// agentPreamble is always prepended to the system prompt so the LLM knows
// how Harvey's auto-execute model works.
const agentPreamble = `You are Harvey, a terminal coding agent running inside an interactive
REPL. Harvey automatically handles certain structured outputs from your
replies — you do not need to tell the operator to run slash commands.

## Auto-execute model

### File writes (always active)
Whenever you produce a fenced code block tagged with a target path,
Harvey writes it to disk immediately after your reply — no /apply needed.

Tag format (two styles are supported):
  ` + "```" + `bash:testout/hello.bash   ← colon-separated lang:path
  ` + "```" + `go cmd/hello/main.go     ← space-separated lang path

Always tag code blocks that are meant to be files. Do NOT say "run
/apply" — Harvey handles it automatically and will confirm with the
operator before writing.

### Shell commands (agent mode only)
When the operator has enabled agent mode (/agent on), wrap suggested
shell commands in backtick /run hints:

  ` + "`" + `/run chmod +x testout/hello.bash` + "`" + `

Harvey will confirm the command with the operator and then run it,
injecting the output into context so you can see the result.

When agent mode is off (the default), you may still suggest commands in
this format — the operator can run them manually with /run.

## Slash commands (for reference)

| What needs to happen | Command |
|---|---|
| Create / write a file | tag your code block (auto-applied) |
| Run a shell command | ` + "`" + `/run <command>` + "`" + ` hint (auto-run in agent mode) |
| Read a file into context | /read <path> |
| Search the workspace | /search <pattern> |
| View git status / diff / log | /git <subcommand> |

## Rules
1. Never show fake command output. If you need execution, emit a
   backtick ` + "`" + `/run ...` + "`" + ` hint.
2. Never claim a file has been written. Tag the code block; Harvey
   will write it and confirm the outcome.
3. Always tag code blocks meant for files — one block per file.

`

/** Config holds Harvey's runtime configuration.
 *
 * Fields:
 *   WorkDir         (string) — root directory Harvey is allowed to read/write; defaults to ".".
 *   SystemPrompt    (string) — contents of HARVEY.md, injected as the system prompt.
 *   OllamaURL       (string) — Ollama base URL (default: http://localhost:11434).
 *   OllamaModel     (string) — currently selected Ollama model.
 *   PublicAIURL     (string) — publicai.co base URL.
 *   PublicAIKey     (string) — API key read from PUBLICAI_API_KEY env var.
 *   PublicAIModel   (string) — model name (default: abertus).
 *   ResumeSessionID (int64)  — session ID to resume on startup; 0 means ask the user.
 *
 * Example:
 *   cfg := DefaultConfig()
 *   cfg.WorkDir = "/home/user/myproject"
 */
type Config struct {
	WorkDir          string // workspace root; all file I/O is constrained to this tree
	CurrentProjectID int64  // ID of the active knowledge-base project (0 = none)
	ResumeSessionID  int64  // session to resume at startup; 0 = prompt the user
	SystemPrompt     string // contents of HARVEY.md, injected as the system prompt
	OllamaURL        string // Ollama base URL (default: http://localhost:11434)
	OllamaModel      string // currently selected Ollama model
	PublicAIURL      string // publicai.co base URL (default: https://api.publicai.co/v1)
	PublicAIKey      string // API key read from PUBLICAI_API_KEY
	PublicAIModel    string // model name (default: abertus)
	AutoRecord       bool   // start a Fountain session recording automatically at startup
	RecordPath       string // file path for auto-recording; empty = auto-generated timestamped name
	ContinuePath     string // Fountain file to load as pre-history when starting the REPL
	ReplayPath       string // Fountain file to replay instead of entering the REPL
	ReplayOutputPath string // output path for replay recording; empty = auto-generated
}

/** DefaultConfig returns a Config populated with sensible defaults. WorkDir
 * defaults to "." (the process working directory at startup).
 *
 * Returns:
 *   *Config — configuration with default values pre-filled.
 *
 * Example:
 *   cfg := DefaultConfig()
 *   fmt.Println(cfg.OllamaURL) // "http://localhost:11434"
 */
func DefaultConfig() *Config {
	return &Config{
		WorkDir:       ".",
		OllamaURL:     "http://localhost:11434",
		PublicAIURL:   "https://api.publicai.co/v1",
		PublicAIKey:   os.Getenv("PUBLICAI_API_KEY"),
		PublicAIModel: "abertus",
	}
}

/** LoadHarveyMD reads HARVEY.md from the current directory and returns the
 * agent preamble followed by the file contents. The preamble is always
 * included so the LLM knows it must use slash commands for real side-effects
 * rather than narrating fake output. Returns only the preamble when HARVEY.md
 * does not exist.
 *
 * Returns:
 *   string — agentPreamble + HARVEY.md contents (or agentPreamble alone).
 *
 * Example:
 *   prompt := LoadHarveyMD()
 *   cfg.SystemPrompt = prompt
 */
func LoadHarveyMD() string {
	data, err := os.ReadFile("HARVEY.md")
	if err != nil {
		return agentPreamble
	}
	return agentPreamble + string(data)
}
