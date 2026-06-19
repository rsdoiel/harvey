package harvey

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

**IMPORTANT**: Never emit a tagged code block to reference or display a
file you want to read. Tagged blocks ALWAYS write a file to disk. To
read a file, call the read_file tool instead.

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

/** RagStoreEntry describes one named RAG knowledge store in the registry.
 *
 * Fields:
 *   Name           (string)            — short identifier used with /rag use and /rag new.
 *   DBPath         (string)            — path to the SQLite database, relative to workspace root.
 *   EmbeddingModel (string)            — embedding model name bound to this store.
 *   ModelMap       (map[string]string) — generation model → embedding model overrides.
 *   EmbedderKind   (string)            — "ollama" (default) or "encoderfile".
 *   EmbedderURL    (string)            — base URL for the embedder; used when EmbedderKind is "encoderfile".
 *
 * Example:
 *   e := RagStoreEntry{Name: "golang", DBPath: "agents/rag/golang.db", EmbeddingModel: "nomic-embed-text"}
 *   e2 := RagStoreEntry{Name: "docs", DBPath: "agents/rag/docs.db", EmbeddingModel: "nomic-embed-text-v1_5",
 *                        EmbedderKind: "encoderfile", EmbedderURL: "http://localhost:8080"}
 */
type RagStoreEntry struct {
	Name           string
	DBPath         string
	EmbeddingModel string
	ModelMap       map[string]string
	EmbedderKind   string // "ollama" (default) or "encoderfile"
	EmbedderURL    string // base URL for the embedder when EmbedderKind == "encoderfile"
}

/** Config holds Harvey's runtime configuration.
 *
 * Fields:
 *   WorkDir      (string) — root directory Harvey is allowed to read/write; defaults to ".".
 *   SessionsDir  (string) — directory for session .spmd files; empty = agents/sessions/.
 *   AgentsDir    (string) — base directory for the agents/skills tree; empty = agents/.
 *   SystemPrompt (string) — contents of HARVEY.md, injected as the system prompt.
 *   OllamaURL    (string) — Ollama base URL (default: http://localhost:11434).
 *   OllamaModel  (string) — currently selected Ollama model.
 *   AutoRecord   (bool)   — record every session to a .spmd file (default true).
 *   Memory       (MemoryConfig) — unified memory system settings, including RAG and knowledge base.
 *
 * Example:
 *   cfg := DefaultConfig()
 *   cfg.WorkDir = "/home/user/myproject"
 */
type Config struct {
	WorkDir             string          // workspace root; all file I/O is constrained to this tree
	SessionsDir         string          // directory for .spmd session files; empty = agents/sessions/
	AgentsDir           string          // agents/skills tree root; empty = agents/
	SystemPrompt        string          // contents of HARVEY.md, injected as the system prompt
	OllamaURL           string          // Ollama base URL (default: http://localhost:11434)
	OllamaModel         string          // currently selected Ollama model
	OllamaContextLength int             // context window size in tokens; 0 = unknown
	Routes              []RouteEndpoint // registered remote endpoints; persisted across sessions
	RoutingEnabled      bool            // when false, @mentions are rejected with a warning
	AutoRecord          bool            // record every session to a .spmd file automatically
	RecordPath          string          // file path for auto-recording; empty = auto-generated timestamped name
	ContinuePath        string          // session file to load as pre-history when starting the REPL
	ResumeLatest        bool            // --resume: auto-select most recent session file
	ReplayPath          string          // session file to replay instead of entering the REPL
	ReplayOutputPath    string          // output path for replay recording; empty = auto-generated
	ReplayContinue      bool            // when true, drop into the REPL after replay finishes
	ModelCacheDB        string          // path to model_cache.db; empty = harvey/model_cache.db
	// Security settings
	SafeMode        bool     // when true, only commands in AllowedCommands can be executed via ! or /run
	AllowedCommands []string // list of command names permitted when SafeMode is enabled
	// Permissions: map from path prefix to list of allowed actions (read, write, exec, delete)
	Permissions map[string][]string
	// Timeout settings
	RunTimeout    time.Duration // timeout for shell commands run via ! or /run; 0 means no timeout
	OllamaTimeout time.Duration // HTTP client timeout for local LLM providers; 0 means no timeout
	// Llamafile backend settings
	LlamafileModels    []LlamafileEntry // registered llamafile models
	LlamafileActive    string           // name of the active model; "" = none
	LlamafileURL            string        // API base URL; default "http://localhost:8080"
	LlamafileModelsDir      string        // discovery directory; default "$HOME/Models"
	LlamafileStartupTimeout time.Duration // how long to wait for the server to respond; default 120s
	LlamafileGPULayers      int           // layers to offload to GPU via -ngl; -1 = let llamafile decide (CPU), 99 = maximise GPU
	// Model aliases: short name → full Ollama model identifier
	ModelAliases map[string]string
	// Tool settings
	ToolsEnabled         bool // when true, send tool schemas to models that support it
	MaxToolCallsPerTurn  int  // hard limit on tool call rounds per user turn; 0 = defaultMaxToolCallsPerTurn
	MaxOutputBytes       int  // cap on tool output injected into context; 0 = defaultMaxOutputBytes
	ToolResultCompaction bool // when true, prior tool-call rounds are compacted before each new LLM turn
	// Debug mode: set by --debug at startup; enables JSONL debug log and OLLAMA_DEBUG
	Debug bool
	// SyntaxHighlight enables ANSI colour highlighting of code blocks in responses.
	SyntaxHighlight bool
	// AutoFormat enables automatic code formatting after write_file writes a source file.
	AutoFormat bool
	// Memory system: RAG stores, knowledge base, and retrieval settings
	Memory MemoryConfig
}

/** DefaultConfig returns a Config populated with sensible defaults. WorkDir
 * defaults to "." (the process working directory at startup). AutoRecord
 * defaults to true so every session is saved to agents/sessions/.
 *
 * Returns:
 *   *Config — configuration with default values pre-filled.
 *
 * Example:
 *   cfg := DefaultConfig()
 *   fmt.Println(cfg.OllamaURL) // "http://localhost:11434"
 */
// DefaultAllowedCommands is the default list of commands allowed when SafeMode is enabled.
// These are considered safe read-only or low-risk utilities.
var DefaultAllowedCommands = []string{
	"ls", "cat", "grep", "head", "tail", "wc",
	"find", "stat", "jq", "htmlq", "bat", "batcat",
}

func DefaultConfig() *Config {
	allowed := make([]string, len(DefaultAllowedCommands))
	copy(allowed, DefaultAllowedCommands)
	// Default permissions: full access to workspace root, read-only for subdirectories
	defaultPerms := map[string][]string{
		".": {"read", "write", "exec", "delete"},
	}
	return &Config{
		WorkDir:         ".",
		OllamaURL:       "http://localhost:11434",
		AutoRecord:      true,
		SafeMode:        true,
		AllowedCommands: allowed,
		Permissions:     defaultPerms,
		ModelAliases:          make(map[string]string),
		RunTimeout:            5 * time.Minute,
		OllamaTimeout:         0, // no timeout — local inference can take minutes on slow hardware
		LlamafileURL:            "http://localhost:8080",
		LlamafileModelsDir:      llamafileDefaultModelsDir(),
		LlamafileStartupTimeout: 120 * time.Second,
		LlamafileGPULayers:      99,
		SyntaxHighlight:       true,
		AutoFormat:            true,
		ToolsEnabled:          true,
		MaxToolCallsPerTurn:   defaultMaxToolCallsPerTurn,
		MaxOutputBytes:        defaultMaxOutputBytes,
		ToolResultCompaction:  true,
		Memory: MemoryConfig{
			Enabled:       true,
			TopK:          5,
			InjectOnStart: true,
			BudgetPct:     0.25,
			RollingSummary: RollingSummaryConfig{
				Enabled:   true,
				WarnAtPct: 0.80,
				KeepTurns: 6,
			},
		},
	}
}

/** ResolveModelAlias returns the full Ollama model identifier for alias, or
 * alias itself when no mapping is defined. Lookup is case-insensitive.
 *
 * Parameters:
 *   alias (string) — short name typed by the user (e.g. "qwen-coder").
 *
 * Returns:
 *   string — full model identifier (e.g. "qwen2.5-coder:7b") or alias unchanged.
 *
 * Example:
 *   full := cfg.ResolveModelAlias("qwen-coder") // → "qwen2.5-coder:7b"
 *   same := cfg.ResolveModelAlias("llama3.2:latest") // → "llama3.2:latest"
 */
func (c *Config) ResolveModelAlias(alias string) string {
	if c.ModelAliases == nil {
		return alias
	}
	if full, ok := c.ModelAliases[strings.ToLower(alias)]; ok {
		return full
	}
	return alias
}

/** IsCommandAllowed returns true if cmd is in the AllowedCommands list.
 * When SafeMode is false, all commands are allowed (returns true).
 * When SafeMode is true, only commands in AllowedCommands are permitted.
 *
 * Parameters:
 *   cmd (string) — the command name to check.
 *
 * Returns:
 *   bool — true if the command is allowed.
 *
 * Example:
 *   if !cfg.IsCommandAllowed("git") {
 *       fmt.Println("git is not allowed in safe mode")
 *   }
 */
func (c *Config) IsCommandAllowed(cmd string) bool {
	if !c.SafeMode {
		return true
	}
	for _, allowed := range c.AllowedCommands {
		if cmd == allowed {
			return true
		}
	}
	return false
}

/** AddAllowedCommand adds a command to the AllowedCommands list if not already present.
 *
 * Parameters:
 *   cmd (string) — command name to add.
 *
 * Example:
 *   cfg.AddAllowedCommand("git")
 */
func (c *Config) AddAllowedCommand(cmd string) {
	for _, existing := range c.AllowedCommands {
		if existing == cmd {
			return
		}
	}
	c.AllowedCommands = append(c.AllowedCommands, cmd)
}

/** RemoveAllowedCommand removes a command from the AllowedCommands list.
 * It is a no-op if the command is not present.
 *
 * Parameters:
 *   cmd (string) — command name to remove.
 *
 * Example:
 *   cfg.RemoveAllowedCommand("git")
 */
func (c *Config) RemoveAllowedCommand(cmd string) {
	out := c.AllowedCommands[:0]
	for _, e := range c.AllowedCommands {
		if e != cmd {
			out = append(out, e)
		}
	}
	c.AllowedCommands = out
}

/** ResetAllowedCommands replaces AllowedCommands with the default list.
 *
 * Example:
 *   cfg.ResetAllowedCommands()
 */
func (c *Config) ResetAllowedCommands() {
	c.AllowedCommands = make([]string, len(DefaultAllowedCommands))
	copy(c.AllowedCommands, DefaultAllowedCommands)
}

// Permission types
const (
	PermRead   = "read"
	PermWrite  = "write"
	PermExec   = "exec"
	PermDelete = "delete"
)

// AllPermissions is a slice of all valid permission types.
var AllPermissions = []string{PermRead, PermWrite, PermExec, PermDelete}

/** HasPermission checks if the given permission is allowed for a path.
 * It checks the most specific matching path prefix first.
 *
 * Parameters:
 *   path (string) — the path to check (relative to workspace root).
 *   perm (string) — the permission to check (read, write, exec, delete).
 *
 * Returns:
 *   bool — true if the permission is allowed.
 *
 * Example:
 *   if cfg.HasPermission("src/main.go", "read") {
 *       // read is allowed
 *   }
 */
func (c *Config) HasPermission(path string, perm string) bool {
	if c.Permissions == nil {
		return true // No permissions configured means all allowed
	}

	// Find the most specific matching prefix
	bestMatch := "."
	bestMatchLen := 0

	for prefix := range c.Permissions {
		if strings.HasPrefix(path, prefix) || path == prefix {
			// Check if this is a better (more specific) match
			if len(prefix) > bestMatchLen {
				bestMatch = prefix
				bestMatchLen = len(prefix)
			}
		}
	}

	// Check if the permission is in the list for the best matching prefix
	for _, p := range c.Permissions[bestMatch] {
		if p == perm {
			return true
		}
	}
	return false
}

/** SetPermission sets the permissions for a path prefix.
 * Replaces any existing permissions for that prefix.
 *
 * Parameters:
 *   prefix (string) — the path prefix (e.g., "src/", ".", "docs/"").
 *   perms ([]string) — list of permissions (read, write, exec, delete).
 *
 * Example:
 *   cfg.SetPermission("src/", []string{"read"})
 */
func (c *Config) SetPermission(prefix string, perms []string) {
	if c.Permissions == nil {
		c.Permissions = make(map[string][]string)
	}
	c.Permissions[prefix] = perms
}

/** AddPermission adds a permission to a path prefix.
 * Creates the prefix entry if it doesn't exist.
 *
 * Parameters:
 *   prefix (string) — the path prefix.
 *   perm (string) — the permission to add (read, write, exec, delete).
 *
 * Example:
 *   cfg.AddPermission("src/", "read")
 */
func (c *Config) AddPermission(prefix string, perm string) {
	if c.Permissions == nil {
		c.Permissions = make(map[string][]string)
	}
	perms := c.Permissions[prefix]
	// Check if permission already exists
	for _, p := range perms {
		if p == perm {
			return
		}
	}
	c.Permissions[prefix] = append(perms, perm)
}

/** RemovePermission removes a permission from a path prefix.
 * It is a no-op if the prefix or permission doesn't exist.
 *
 * Parameters:
 *   prefix (string) — the path prefix.
 *   perm (string) — the permission to remove.
 *
 * Example:
 *   cfg.RemovePermission("src/", "write")
 */
func (c *Config) RemovePermission(prefix string, perm string) {
	if c.Permissions == nil {
		return
	}
	perms, ok := c.Permissions[prefix]
	if !ok {
		return
	}
	// Remove the permission
	out := perms[:0]
	for _, p := range perms {
		if p != perm {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		delete(c.Permissions, prefix)
	} else {
		c.Permissions[prefix] = out
	}
}

/** ResetPermissions resets permissions to the default (full access to root).
 *
 * Example:
 *   cfg.ResetPermissions()
 */
func (c *Config) ResetPermissions() {
	c.Permissions = map[string][]string{
		".": {PermRead, PermWrite, PermExec, PermDelete},
	}
}

/** PermissionString returns a comma-separated string of permissions for a prefix.
 *
 * Parameters:
 *   prefix (string) — the path prefix.
 *
 * Returns:
 *   string — comma-separated permissions, or "none" if no permissions.
 */
func (c *Config) PermissionString(prefix string) string {
	if c.Permissions == nil {
		return "none"
	}
	perms, ok := c.Permissions[prefix]
	if !ok || len(perms) == 0 {
		return "none"
	}
	return strings.Join(perms, ", ")
}

/** MemoryConfig controls the behaviour of Harvey's memory system.
 *
 * Fields:
 *   Enabled           (bool)                — when false the entire memory system is skipped; default true.
 *   TopK              (int)                 — number of memories retrieved at session start; default 5.
 *   InjectOnStart     (bool)                — inject memory context block in ClearHistory; default true.
 *   BudgetPct         (float64)             — fraction of model context window allocated to memory
 *                                             injection (default 0.25); falls back to 512 tokens when
 *                                             OllamaContextLength is unknown.
 *   RollingSummary    (RollingSummaryConfig) — working-memory compression settings.
 *   RagStores         ([]RagStoreEntry)      — all registered named RAG stores.
 *   RagActive         (string)              — name of the currently active RAG store; "" = none.
 *   RagEnabled        (bool)               — when true, inject top-K chunks before each Chat call.
 *   KnowledgeDB       (string)             — path to knowledge.db; empty = agents/knowledge.db.
 *   CurrentProjectID  (int64)              — in-session active knowledge-base project ID (0 = none).
 *
 * Example:
 *   cfg.Memory = MemoryConfig{Enabled: true, TopK: 5, InjectOnStart: true, BudgetPct: 0.25}
 */
type MemoryConfig struct {
	Enabled          bool
	TopK             int
	InjectOnStart    bool
	BudgetPct        float64              // fraction of model context window for memory injection
	RollingSummary   RollingSummaryConfig // working-memory compression settings
	RagStores        []RagStoreEntry      // all registered named RAG stores
	RagActive        string               // name of the currently active RAG store; "" = none
	RagEnabled       bool                 // when true, inject top-K chunks before each Chat call
	KnowledgeDB      string               // path to knowledge.db; empty = agents/knowledge.db
	CurrentProjectID int64                // in-session active knowledge-base project ID (0 = none)
}

/** RollingSummaryConfig controls automatic working-memory compression.
 * When history token count exceeds WarnAtPct of the model context window,
 * Harvey warns then compresses all but the last KeepTurns turns into a
 * short summary via a separate low-temperature LLM call.
 *
 * Fields:
 *   Enabled   (bool)    — when false compression never fires; default true.
 *   WarnAtPct (float64) — compress when history exceeds this fraction of the
 *                         model context window; default 0.80.
 *   KeepTurns (int)     — number of recent turns to keep verbatim; default 6.
 *
 * Example:
 *   cfg.Memory.RollingSummary = RollingSummaryConfig{Enabled: true, WarnAtPct: 0.80, KeepTurns: 6}
 */
type RollingSummaryConfig struct {
	Enabled   bool
	WarnAtPct float64
	KeepTurns int
}

/** ActiveRagStore returns a pointer to the active RagStoreEntry in the memory
 * config, or nil when no store is configured.
 *
 * Returns:
 *   *RagStoreEntry — the active entry, or nil.
 *
 * Example:
 *   if e := cfg.Memory.ActiveRagStore(); e != nil {
 *       fmt.Println(e.DBPath)
 *   }
 */
func (m *MemoryConfig) ActiveRagStore() *RagStoreEntry {
	if m.RagActive == "" {
		return nil
	}
	return m.RagStoreByName(m.RagActive)
}

/** RagStoreByName returns a pointer to the named store entry in the memory
 * config, or nil when not found.
 *
 * Parameters:
 *   name (string) — store name to look up.
 *
 * Returns:
 *   *RagStoreEntry — matching entry, or nil.
 *
 * Example:
 *   if e := cfg.Memory.RagStoreByName("golang"); e != nil {
 *       fmt.Println(e.EmbeddingModel)
 *   }
 */
func (m *MemoryConfig) RagStoreByName(name string) *RagStoreEntry {
	for i := range m.RagStores {
		if m.RagStores[i].Name == name {
			return &m.RagStores[i]
		}
	}
	return nil
}

/** AddOrUpdateRagStore inserts e into the memory config registry if its name
 * is new, or replaces the existing entry if one with the same name exists.
 *
 * Parameters:
 *   e (RagStoreEntry) — the entry to add or replace.
 *
 * Example:
 *   cfg.Memory.AddOrUpdateRagStore(RagStoreEntry{Name: "golang", DBPath: "agents/rag/golang.db"})
 */
func (m *MemoryConfig) AddOrUpdateRagStore(e RagStoreEntry) {
	for i := range m.RagStores {
		if m.RagStores[i].Name == e.Name {
			m.RagStores[i] = e
			return
		}
	}
	m.RagStores = append(m.RagStores, e)
}

/** RemoveRagStore removes the store with the given name from the memory config
 * registry. It is a no-op when no store with that name exists.
 *
 * Parameters:
 *   name (string) — name of the store to remove.
 *
 * Example:
 *   cfg.Memory.RemoveRagStore("research-llm")
 */
func (m *MemoryConfig) RemoveRagStore(name string) {
	out := m.RagStores[:0]
	for _, e := range m.RagStores {
		if e.Name != name {
			out = append(out, e)
		}
	}
	m.RagStores = out
}

// ragStoreYAML is the on-disk representation of one entry under rag.stores.
type ragStoreYAML struct {
	Name           string            `yaml:"name"`
	DBPath         string            `yaml:"db_path"`
	EmbeddingModel string            `yaml:"embedding_model"`
	ModelMap       map[string]string `yaml:"model_map,omitempty"`
	EmbedderKind   string            `yaml:"embedder_kind,omitempty"`
	EmbedderURL    string            `yaml:"embedder_url,omitempty"`
}

// ragYAML is the on-disk representation of the rag: section in harvey.yaml.
// The Active/Stores fields are the current format. DBPath, EmbeddingModel,
// and ModelMap are legacy flat fields from before multi-store support; they
// are read for backward-compat migration and never written.
type ragYAML struct {
	Active         string          `yaml:"active,omitempty"`
	Stores         []ragStoreYAML  `yaml:"stores,omitempty"`
	Enabled        bool            `yaml:"enabled"`
	DBPath         string          `yaml:"db_path,omitempty"`
	EmbeddingModel string          `yaml:"embedding_model,omitempty"`
	ModelMap       map[string]string `yaml:"model_map,omitempty"`
}

// harveyYAML is the on-disk representation of harvey/harvey.yaml.
// KnowledgeDB and RAG are kept for backward-compat reading of old files;
// SaveMemoryConfig zeroes them before writing so only memory: is emitted.
type harveyYAML struct {
	KnowledgeDB     string              `yaml:"knowledge_db,omitempty"`
	SessionsDir     string              `yaml:"sessions_dir"`
	AgentsDir       string              `yaml:"agents_dir"`
	AutoRecord      *bool               `yaml:"auto_record"` // nil = not set (keep default)
	ModelCacheDB    string              `yaml:"model_cache_db"`
	RAG             ragYAML             `yaml:"rag,omitempty"`
	Permissions     map[string][]string `yaml:"permissions,omitempty"`
	SafeMode        *bool               `yaml:"safe_mode,omitempty"`        // nil = not set (keep default)
	SyntaxHighlight *bool               `yaml:"syntax_highlight,omitempty"` // nil = not set (keep default)
	AutoFormat      *bool               `yaml:"auto_format,omitempty"`      // nil = not set (keep default)
	AllowedCommands []string            `yaml:"allowed_commands,omitempty"`
	RunTimeout      string              `yaml:"run_timeout,omitempty"`    // e.g. "5m", "300s", "1m 30s", "300"
	OllamaTimeout   string              `yaml:"ollama_timeout,omitempty"` // e.g. "0", "10m"; 0 or empty = no timeout
	Tools           toolsYAML           `yaml:"tools,omitempty"`
	ModelAliases    map[string]string   `yaml:"model_aliases,omitempty"`  // short name → full Ollama model ID
	Memory          memoryYAML          `yaml:"memory,omitempty"`
	Llamafile       llamafileYAML       `yaml:"llamafile,omitempty"`
}

type toolsYAML struct {
	Enabled              *bool `yaml:"enabled,omitempty"`
	MaxToolCallsPerTurn  int   `yaml:"max_tool_calls_per_turn,omitempty"`
	MaxOutputBytes       int   `yaml:"max_output_bytes,omitempty"`
	ToolResultCompaction *bool `yaml:"tool_result_compaction,omitempty"`
}

type llamafileEntryYAML struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

type llamafileYAML struct {
	ModelsDir      string               `yaml:"models_dir,omitempty"`
	Active         string               `yaml:"active,omitempty"`
	URL            string               `yaml:"url,omitempty"`
	StartupTimeout string               `yaml:"startup_timeout,omitempty"` // e.g. "120s", "2m"
	GPULayers      *int                 `yaml:"gpu_layers,omitempty"`      // -ngl value; nil = use default (99)
	Models         []llamafileEntryYAML `yaml:"models,omitempty"`
}

type rollingSummaryYAML struct {
	Enabled   *bool   `yaml:"enabled,omitempty"`
	WarnAtPct float64 `yaml:"warn_at_pct,omitempty"`
	KeepTurns int     `yaml:"keep_turns,omitempty"`
}

type knowledgeBaseYAML struct {
	Path string `yaml:"path,omitempty"`
}

type memoryYAML struct {
	Enabled        *bool               `yaml:"enabled,omitempty"`
	TopK           int                 `yaml:"top_k,omitempty"`
	InjectOnStart  *bool               `yaml:"inject_on_start,omitempty"`
	BudgetPct      float64             `yaml:"budget_pct,omitempty"`
	RollingSummary rollingSummaryYAML  `yaml:"rolling_summary,omitempty"`
	RAG            ragYAML             `yaml:"rag,omitempty"`
	KnowledgeBase  knowledgeBaseYAML   `yaml:"knowledge_base,omitempty"`
}

// parseDurationString parses a duration from a YAML string value. It accepts:
//   - Plain integer: treated as seconds (e.g. "300" → 5 minutes)
//   - Go duration string: "5m", "30s", "1m30s", "1h"
//   - Duration with spaces: "1m 30s" → trimmed to "1m30s" before parsing
//   - Zero or empty: returns 0 (no timeout)
func parseDurationString(s string) (time.Duration, error) {
	s = strings.ReplaceAll(s, " ", "")
	if s == "" || s == "0" {
		return 0, nil
	}
	// Try as plain integer (seconds).
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Duration(n) * time.Second, nil
	}
	// Try as Go duration string.
	return time.ParseDuration(s)
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
	// KnowledgeDB: top-level key for backward compat; memory.knowledge_base.path takes precedence.
	if y.KnowledgeDB != "" {
		cfg.Memory.KnowledgeDB = y.KnowledgeDB
	}
	if y.Memory.KnowledgeBase.Path != "" {
		cfg.Memory.KnowledgeDB = y.Memory.KnowledgeBase.Path
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
	// RAG: top-level rag: key for backward compat; memory.rag: takes precedence when stores are set.
	if len(y.RAG.Stores) > 0 {
		cfg.Memory.RagStores = make([]RagStoreEntry, len(y.RAG.Stores))
		for i, s := range y.RAG.Stores {
			cfg.Memory.RagStores[i] = RagStoreEntry{
				Name:           s.Name,
				DBPath:         s.DBPath,
				EmbeddingModel: s.EmbeddingModel,
				ModelMap:       s.ModelMap,
				EmbedderKind:   s.EmbedderKind,
				EmbedderURL:    s.EmbedderURL,
			}
		}
		cfg.Memory.RagActive = y.RAG.Active
	} else if y.RAG.DBPath != "" {
		// Legacy flat format — migrate to a single "default" entry.
		cfg.Memory.RagStores = []RagStoreEntry{{
			Name:           "default",
			DBPath:         y.RAG.DBPath,
			EmbeddingModel: y.RAG.EmbeddingModel,
			ModelMap:       y.RAG.ModelMap,
		}}
		cfg.Memory.RagActive = "default"
	}
	cfg.Memory.RagEnabled = y.RAG.Enabled
	// memory.rag: takes precedence over top-level rag: when stores are specified there.
	if len(y.Memory.RAG.Stores) > 0 {
		cfg.Memory.RagStores = make([]RagStoreEntry, len(y.Memory.RAG.Stores))
		for i, s := range y.Memory.RAG.Stores {
			cfg.Memory.RagStores[i] = RagStoreEntry{
				Name:           s.Name,
				DBPath:         s.DBPath,
				EmbeddingModel: s.EmbeddingModel,
				ModelMap:       s.ModelMap,
				EmbedderKind:   s.EmbedderKind,
				EmbedderURL:    s.EmbedderURL,
			}
		}
		cfg.Memory.RagActive = y.Memory.RAG.Active
		cfg.Memory.RagEnabled = y.Memory.RAG.Enabled
	} else if y.Memory.RAG.DBPath != "" {
		cfg.Memory.RagStores = []RagStoreEntry{{
			Name:           "default",
			DBPath:         y.Memory.RAG.DBPath,
			EmbeddingModel: y.Memory.RAG.EmbeddingModel,
			ModelMap:       y.Memory.RAG.ModelMap,
		}}
		cfg.Memory.RagActive = "default"
		cfg.Memory.RagEnabled = y.Memory.RAG.Enabled
	}
	// Load permissions if present
	if y.Permissions != nil {
		cfg.Permissions = y.Permissions
	}
	// Load security settings — only override the default when explicitly set in YAML.
	if y.SafeMode != nil {
		cfg.SafeMode = *y.SafeMode
	}
	if y.SyntaxHighlight != nil {
		cfg.SyntaxHighlight = *y.SyntaxHighlight
	}
	if y.AutoFormat != nil {
		cfg.AutoFormat = *y.AutoFormat
	}
	if len(y.AllowedCommands) > 0 {
		cfg.AllowedCommands = y.AllowedCommands
	}
	// Load timeout settings
	if y.RunTimeout != "" {
		if d, err := parseDurationString(y.RunTimeout); err == nil {
			cfg.RunTimeout = d
		}
	}
	if y.OllamaTimeout != "" {
		if d, err := parseDurationString(y.OllamaTimeout); err == nil {
			cfg.OllamaTimeout = d
		}
	}
	// Load model aliases
	if len(y.ModelAliases) > 0 {
		if cfg.ModelAliases == nil {
			cfg.ModelAliases = make(map[string]string)
		}
		for k, v := range y.ModelAliases {
			cfg.ModelAliases[strings.ToLower(k)] = v
		}
	}
	// Load llamafile settings
	if y.Llamafile.ModelsDir != "" {
		cfg.LlamafileModelsDir = expandTilde(y.Llamafile.ModelsDir)
	}
	if y.Llamafile.Active != "" {
		cfg.LlamafileActive = y.Llamafile.Active
	}
	if y.Llamafile.URL != "" {
		cfg.LlamafileURL = y.Llamafile.URL
	}
	if y.Llamafile.StartupTimeout != "" {
		if d, err := parseDurationString(y.Llamafile.StartupTimeout); err == nil {
			cfg.LlamafileStartupTimeout = d
		}
	}
	if y.Llamafile.GPULayers != nil {
		cfg.LlamafileGPULayers = *y.Llamafile.GPULayers
	}
	for _, m := range y.Llamafile.Models {
		cfg.LlamafileModels = append(cfg.LlamafileModels, LlamafileEntry{
			Name: m.Name, Path: m.Path,
		})
	}
	// Load tool settings
	if y.Tools.Enabled != nil {
		cfg.ToolsEnabled = *y.Tools.Enabled
	}
	if y.Tools.MaxToolCallsPerTurn > 0 {
		cfg.MaxToolCallsPerTurn = y.Tools.MaxToolCallsPerTurn
	}
	if y.Tools.MaxOutputBytes > 0 {
		cfg.MaxOutputBytes = y.Tools.MaxOutputBytes
	}
	if y.Tools.ToolResultCompaction != nil {
		cfg.ToolResultCompaction = *y.Tools.ToolResultCompaction
	}
	// Load memory settings
	if y.Memory.Enabled != nil {
		cfg.Memory.Enabled = *y.Memory.Enabled
	}
	if y.Memory.TopK > 0 {
		cfg.Memory.TopK = y.Memory.TopK
	}
	if y.Memory.InjectOnStart != nil {
		cfg.Memory.InjectOnStart = *y.Memory.InjectOnStart
	}
	if y.Memory.BudgetPct > 0 {
		cfg.Memory.BudgetPct = y.Memory.BudgetPct
	}
	if y.Memory.RollingSummary.Enabled != nil {
		cfg.Memory.RollingSummary.Enabled = *y.Memory.RollingSummary.Enabled
	}
	if y.Memory.RollingSummary.WarnAtPct > 0 {
		cfg.Memory.RollingSummary.WarnAtPct = y.Memory.RollingSummary.WarnAtPct
	}
	if y.Memory.RollingSummary.KeepTurns > 0 {
		cfg.Memory.RollingSummary.KeepTurns = y.Memory.RollingSummary.KeepTurns
	}
	return nil
}

/** SaveMemoryConfig writes the memory-related config fields back to
 * agents/harvey.yaml, merging with any existing content so that unrelated keys
 * are preserved. It writes only to the memory: section; any legacy top-level
 * rag: or knowledge_db: keys are cleared from the file on write.
 *
 * Parameters:
 *   ws  (*Workspace) — workspace whose agents/ directory is written.
 *   cfg (*Config)    — source of Memory fields to persist.
 *
 * Returns:
 *   error — on path resolution, YAML parse, or file write failure.
 *
 * Example:
 *   if err := SaveMemoryConfig(ws, cfg); err != nil {
 *       fmt.Println("could not save memory config:", err)
 *   }
 */
func SaveMemoryConfig(ws *Workspace, cfg *Config) error {
	yamlPath, err := ws.AbsPath(filepath.Join(harveySubdir, "harvey.yaml"))
	if err != nil {
		return err
	}

	// Read existing content to preserve unrelated keys.
	var y harveyYAML
	if data, err := os.ReadFile(yamlPath); err == nil {
		_ = yaml.Unmarshal(data, &y)
	}
	// Clear legacy top-level keys; only memory: is written going forward.
	y.RAG = ragYAML{}
	y.KnowledgeDB = ""

	var stores []ragStoreYAML
	if len(cfg.Memory.RagStores) > 0 {
		stores = make([]ragStoreYAML, len(cfg.Memory.RagStores))
		for i, e := range cfg.Memory.RagStores {
			stores[i] = ragStoreYAML{
				Name:           e.Name,
				DBPath:         e.DBPath,
				EmbeddingModel: e.EmbeddingModel,
				ModelMap:       e.ModelMap,
				EmbedderKind:   e.EmbedderKind,
				EmbedderURL:    e.EmbedderURL,
			}
		}
	}
	y.Memory.RAG = ragYAML{
		Active:  cfg.Memory.RagActive,
		Stores:  stores,
		Enabled: cfg.Memory.RagEnabled,
	}
	if cfg.Memory.KnowledgeDB != "" {
		y.Memory.KnowledgeBase.Path = cfg.Memory.KnowledgeDB
	}
	if cfg.Permissions != nil {
		y.Permissions = cfg.Permissions
	}
	y.SafeMode = &cfg.SafeMode
	y.SyntaxHighlight = &cfg.SyntaxHighlight
	y.AutoFormat = &cfg.AutoFormat
	if !cfg.ToolResultCompaction {
		f := false
		y.Tools.ToolResultCompaction = &f
	}
	if len(cfg.AllowedCommands) > 0 {
		y.AllowedCommands = cfg.AllowedCommands
	}
	if cfg.RunTimeout > 0 {
		y.RunTimeout = cfg.RunTimeout.String()
	}
	if cfg.OllamaTimeout > 0 {
		y.OllamaTimeout = cfg.OllamaTimeout.String()
	}

	out, err := yaml.Marshal(&y)
	if err != nil {
		return err
	}
	return os.WriteFile(yamlPath, out, 0644)
}

/** SaveRAGConfig is an alias for SaveMemoryConfig, retained for call-site
 * compatibility. All RAG state now lives in cfg.Memory.
 *
 * Parameters:
 *   ws  (*Workspace) — workspace whose agents/ directory is written.
 *   cfg (*Config)    — source of Memory fields to persist.
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
	return SaveMemoryConfig(ws, cfg)
}

/** SaveModelAliases writes the model_aliases map back to harvey.yaml,
 * merging with any existing content so that unrelated keys are preserved.
 *
 * Parameters:
 *   ws  (*Workspace) — workspace whose agents/ directory is written.
 *   cfg (*Config)    — source of ModelAliases to persist.
 *
 * Returns:
 *   error — on path resolution, YAML parse, or file write failure.
 *
 * Example:
 *   if err := SaveModelAliases(ws, cfg); err != nil {
 *       fmt.Println("could not save aliases:", err)
 *   }
 */
func SaveModelAliases(ws *Workspace, cfg *Config) error {
	yamlPath, err := ws.AbsPath(filepath.Join(harveySubdir, "harvey.yaml"))
	if err != nil {
		return err
	}

	// Read existing content to preserve all other keys.
	var y harveyYAML
	if data, err := os.ReadFile(yamlPath); err == nil {
		_ = yaml.Unmarshal(data, &y)
	}

	y.ModelAliases = cfg.ModelAliases

	out, err := yaml.Marshal(&y)
	if err != nil {
		return err
	}
	return os.WriteFile(yamlPath, out, 0644)
}

// ─── LlamafileEntry and registry helpers ─────────────────────────────────────

/** LlamafileEntry describes one registered llamafile model in Harvey's registry.
 *
 * Fields:
 *   Name (string) — short identifier used with /llamafile use, e.g. "qwen-coding".
 *   Path (string) — path to the binary; absolute or workspace-relative.
 *
 * Example:
 *   e := LlamafileEntry{Name: "qwen", Path: "/home/user/Models/Qwen3.5-4B.llamafile"}
 */
type LlamafileEntry struct {
	Name string
	Path string
}

// llamafileDefaultModelsDir returns the default discovery directory ($HOME/Models).
// Falls back to "." when the home directory cannot be determined.
func llamafileDefaultModelsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Models")
}

// expandTilde replaces a leading "~/" or bare "~" with the user's home directory.
// Returns s unchanged if it does not start with "~" or home lookup fails.
func expandTilde(s string) string {
	if !strings.HasPrefix(s, "~") {
		return s
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return s
	}
	if s == "~" {
		return home
	}
	return filepath.Join(home, s[2:])
}

/** ActiveLlamafileEntry returns a pointer to the active LlamafileEntry in the
 * config, or nil when no active model is set or the name is not found.
 *
 * Returns:
 *   *LlamafileEntry — the active entry, or nil.
 *
 * Example:
 *   if e := cfg.ActiveLlamafileEntry(); e != nil {
 *       fmt.Println(e.Path)
 *   }
 */
func (c *Config) ActiveLlamafileEntry() *LlamafileEntry {
	return c.LlamafileEntryByName(c.LlamafileActive)
}

/** LlamafileEntryByName returns a pointer to the named LlamafileEntry, or nil
 * when not found.
 *
 * Parameters:
 *   name (string) — registry name to look up.
 *
 * Returns:
 *   *LlamafileEntry — matching entry, or nil.
 *
 * Example:
 *   if e := cfg.LlamafileEntryByName("qwen"); e != nil {
 *       fmt.Println(e.Path)
 *   }
 */
func (c *Config) LlamafileEntryByName(name string) *LlamafileEntry {
	if name == "" {
		return nil
	}
	for i := range c.LlamafileModels {
		if c.LlamafileModels[i].Name == name {
			return &c.LlamafileModels[i]
		}
	}
	return nil
}

/** AddOrUpdateLlamafileEntry inserts e into the registry if its name is new,
 * or replaces the existing entry if one with the same name exists.
 *
 * Parameters:
 *   e (LlamafileEntry) — the entry to add or replace.
 *
 * Example:
 *   cfg.AddOrUpdateLlamafileEntry(LlamafileEntry{Name: "qwen", Path: "/home/user/Models/qwen.llamafile"})
 */
func (c *Config) AddOrUpdateLlamafileEntry(e LlamafileEntry) {
	for i := range c.LlamafileModels {
		if c.LlamafileModels[i].Name == e.Name {
			c.LlamafileModels[i] = e
			return
		}
	}
	c.LlamafileModels = append(c.LlamafileModels, e)
}

/** SaveLlamafileConfig writes the llamafile registry back to agents/harvey.yaml,
 * merging with existing content so that unrelated keys are preserved. Only the
 * llamafile: section is updated.
 *
 * Parameters:
 *   ws  (*Workspace) — workspace whose agents/ directory is written.
 *   cfg (*Config)    — source of llamafile fields to persist.
 *
 * Returns:
 *   error — on path resolution, YAML parse, or file write failure.
 *
 * Example:
 *   if err := SaveLlamafileConfig(ws, cfg); err != nil {
 *       fmt.Println("could not save llamafile config:", err)
 *   }
 */
func SaveLlamafileConfig(ws *Workspace, cfg *Config) error {
	yamlPath, err := ws.AbsPath(filepath.Join(harveySubdir, "harvey.yaml"))
	if err != nil {
		return err
	}
	var y harveyYAML
	if data, err := os.ReadFile(yamlPath); err == nil {
		_ = yaml.Unmarshal(data, &y)
	}
	entries := make([]llamafileEntryYAML, len(cfg.LlamafileModels))
	for i, e := range cfg.LlamafileModels {
		entries[i] = llamafileEntryYAML{Name: e.Name, Path: e.Path}
	}
	startupTO := ""
	if cfg.LlamafileStartupTimeout > 0 && cfg.LlamafileStartupTimeout != 120*time.Second {
		startupTO = cfg.LlamafileStartupTimeout.String()
	}
	var gpuLayers *int
	if cfg.LlamafileGPULayers != 99 { // only persist when overriding the default
		gpuLayers = &cfg.LlamafileGPULayers
	}
	y.Llamafile = llamafileYAML{
		ModelsDir:      cfg.LlamafileModelsDir,
		Active:         cfg.LlamafileActive,
		URL:            cfg.LlamafileURL,
		StartupTimeout: startupTO,
		GPULayers:      gpuLayers,
		Models:         entries,
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
