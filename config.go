package harvey

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

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

### Shell commands
When you want to suggest a shell command, wrap it in a backtick /run hint:

  ` + "`" + `/run chmod +x testout/hello.bash` + "`" + `

The operator can run it manually with /run.

## Slash commands (for reference)

| What needs to happen | Command |
|---|---|
| Create / write a file | tag your code block (auto-applied) |
| Run a shell command | ` + "`" + `/run <command>` + "`" + ` |
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
 *   WorkDir      (string) — root directory Harvey is allowed to read/write; defaults to ".".
 *   SessionsDir  (string) — directory for session .spmd files; empty = harvey/sessions/.
 *   KnowledgeDB  (string) — path to the knowledge base SQLite file; empty = harvey/knowledge.db.
 *   AgentsDir    (string) — base directory for the agents/skills tree; empty = agents/.
 *   SystemPrompt (string) — contents of HARVEY.md, injected as the system prompt.
 *   OllamaURL    (string) — Ollama base URL (default: http://localhost:11434).
 *   OllamaModel  (string) — currently selected Ollama model.
 *   PublicAIURL  (string) — publicai.co base URL.
 *   PublicAIKey  (string) — API key read from PUBLICAI_API_KEY env var.
 *   PublicAIModel (string) — model name (default: abertus).
 *   AutoRecord   (bool)   — record every session to a .spmd file (default true).
 *
 * Example:
 *   cfg := DefaultConfig()
 *   cfg.WorkDir = "/home/user/myproject"
 */
type Config struct {
	WorkDir             string          // workspace root; all file I/O is constrained to this tree
	SessionsDir         string          // directory for .spmd session files; empty = harvey/sessions/
	KnowledgeDB         string          // path to knowledge.db; empty = harvey/knowledge.db
	AgentsDir           string          // agents/skills tree root; empty = agents/
	CurrentProjectID    int64           // ID of the active knowledge-base project (0 = none)
	SystemPrompt        string          // contents of HARVEY.md, injected as the system prompt
	OllamaURL           string          // Ollama base URL (default: http://localhost:11434)
	OllamaModel         string          // currently selected Ollama model
	OllamaContextLength int             // context window size in tokens; 0 = unknown
	Routes              []RouteEndpoint // registered remote endpoints; persisted across sessions
	RoutingEnabled      bool            // when false, @mentions are rejected with a warning
	PublicAIURL         string          // publicai.co base URL (default: https://api.publicai.co/v1)
	PublicAIKey         string          // API key read from PUBLICAI_API_KEY
	PublicAIModel       string          // model name (default: abertus)
	AutoRecord          bool            // record every session to a .spmd file automatically
	RecordPath          string          // file path for auto-recording; empty = auto-generated timestamped name
	ContinuePath        string          // session file to load as pre-history when starting the REPL
	ReplayPath          string          // session file to replay instead of entering the REPL
	ReplayOutputPath    string          // output path for replay recording; empty = auto-generated
	ModelCacheDB        string            // path to model_cache.db; empty = harvey/model_cache.db
	RagDBPath           string            // path to the RAG SQLite database; empty = disabled
	RagEmbedModel       string            // embedding model used to build the RAG database
	RagModelMap         map[string]string // generation model → embedding model name
	RagEnabled          bool              // when true, top-K chunks are injected before each Chat call
}

/** DefaultConfig returns a Config populated with sensible defaults. WorkDir
 * defaults to "." (the process working directory at startup). AutoRecord
 * defaults to true so every session is saved to harvey/sessions/.
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
		AutoRecord:    true,
	}
}

// ragYAML is the on-disk representation of the rag: section in harvey.yaml.
type ragYAML struct {
	DBPath        string            `yaml:"db_path"`
	EmbeddingModel string           `yaml:"embedding_model"`
	ModelMap      map[string]string `yaml:"model_map"`
	Enabled       bool              `yaml:"enabled"`
}

// harveyYAML is the on-disk representation of harvey/harvey.yaml.
type harveyYAML struct {
	KnowledgeDB  string  `yaml:"knowledge_db"`
	SessionsDir  string  `yaml:"sessions_dir"`
	AgentsDir    string  `yaml:"agents_dir"`
	AutoRecord   *bool   `yaml:"auto_record"` // nil = not set (keep default)
	ModelCacheDB string  `yaml:"model_cache_db"`
	RAG          ragYAML `yaml:"rag"`
}

/** LoadHarveyYAML reads agents/harvey.yaml from ws and applies any overrides
 * to cfg. Missing fields are left unchanged. The file is optional — its
 * absence is silently ignored.
 *
 * Parameters:
 *   ws  (*Workspace) — workspace whose agents/ directory is searched.
 *   cfg (*Config)    — config to update in place.
 *
 * Returns:
 *   error — only on YAML parse failure; a missing file returns nil.
 *
 * Example:
 *   if err := LoadHarveyYAML(ws, cfg); err != nil {
 *       log.Fatal(err)
 *   }
 */
func LoadHarveyYAML(ws *Workspace, cfg *Config) error {
	yamlPath, err := ws.AbsPath(filepath.Join(harveySubdir, "harvey.yaml"))
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(yamlPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var y harveyYAML
	if err := yaml.Unmarshal(data, &y); err != nil {
		return err
	}
	if y.KnowledgeDB != "" {
		cfg.KnowledgeDB = y.KnowledgeDB
	}
	if y.SessionsDir != "" {
		cfg.SessionsDir = y.SessionsDir
	}
	if y.AgentsDir != "" {
		cfg.AgentsDir = y.AgentsDir
	}
	if y.AutoRecord != nil {
		cfg.AutoRecord = *y.AutoRecord
	}
	if y.ModelCacheDB != "" {
		cfg.ModelCacheDB = y.ModelCacheDB
	}
	if y.RAG.DBPath != "" {
		cfg.RagDBPath = y.RAG.DBPath
	}
	if y.RAG.EmbeddingModel != "" {
		cfg.RagEmbedModel = y.RAG.EmbeddingModel
	}
	if len(y.RAG.ModelMap) > 0 {
		cfg.RagModelMap = y.RAG.ModelMap
	}
	cfg.RagEnabled = y.RAG.Enabled
	return nil
}

/** SaveRAGConfig writes the RAG-related config fields back to
 * harvey/harvey.yaml, merging with any existing content so that non-RAG keys
 * are preserved.
 *
 * Parameters:
 *   ws  (*Workspace) — workspace whose harvey/ directory is written.
 *   cfg (*Config)    — source of RAG fields to persist.
 *
 * Returns:
 *   error — on path resolution, YAML parse, or file write failure.
 *
 * Example:
 *   if err := SaveRAGConfig(ws, cfg); err != nil {
 *       fmt.Println("could not save RAG config:", err)
 *   }
 */
func SaveRAGConfig(ws *Workspace, cfg *Config) error {
	yamlPath, err := ws.AbsPath(filepath.Join(harveySubdir, "harvey.yaml"))
	if err != nil {
		return err
	}

	// Read existing content to preserve non-RAG keys.
	var y harveyYAML
	if data, err := os.ReadFile(yamlPath); err == nil {
		_ = yaml.Unmarshal(data, &y)
	}

	y.RAG = ragYAML{
		DBPath:        cfg.RagDBPath,
		EmbeddingModel: cfg.RagEmbedModel,
		ModelMap:      cfg.RagModelMap,
		Enabled:       cfg.RagEnabled,
	}

	out, err := yaml.Marshal(&y)
	if err != nil {
		return err
	}
	return os.WriteFile(yamlPath, out, 0644)
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
