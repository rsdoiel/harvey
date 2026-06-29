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
 *   SkipPerPrompt  (bool)              — when true, ragAugment skips this store; the model uses
 *                                        retrieve_memory instead. Set via per_prompt: false in YAML.
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
	SkipPerPrompt  bool   // when true, ragAugment skips this store (per_prompt: false in YAML)
}

/** OllamaConfig holds all Ollama-specific connection and model settings.
 *
 * Fields:
 *   URL           (string)        — Ollama base URL; default "http://localhost:11434".
 *   Model         (string)        — currently selected Ollama model.
 *   ContextLength (int)           — context window size in tokens; 0 = unknown.
 *   Timeout       (time.Duration) — HTTP timeout for Ollama requests; 0 = no timeout.
 *
 * Example:
 *   cfg.Ollama = OllamaConfig{URL: "http://localhost:11434", Model: "llama3.1:8b"}
 */
type OllamaConfig struct {
	URL           string
	Model         string
	ContextLength int
	Timeout       time.Duration
}

/** LlamafileConfig holds settings for the llamafile inference backend.
 *
 * Fields:
 *   URL            (string)           — API base URL; default "http://localhost:8080".
 *   ModelsDir      (string)           — discovery directory; default "$HOME/Models".
 *   StartupTimeout (time.Duration)    — how long to wait for server response; default 120s.
 *   GPULayers      (int)              — -ngl value; 99 = maximise GPU.
 *   MaxTokens      (int)              — max tokens per completion; 0 = no limit.
 *   Models         ([]LlamafileEntry) — registered llamafile models.
 *   Active         (string)           — name of the active model; "" = none.
 *
 * Example:
 *   cfg.Llamafile = LlamafileConfig{URL: "http://localhost:8080", GPULayers: 99}
 */
type LlamafileConfig struct {
	URL            string
	ModelsDir      string
	StartupTimeout time.Duration
	GPULayers      int
	MaxTokens      int
	Models         []LlamafileEntry
	Active         string
}

/** SecurityConfig holds safe-mode and command permission settings.
 *
 * Fields:
 *   SafeMode        (bool)                — when true, only AllowedCommands can be executed via ! or /run.
 *   AllowedCommands ([]string)            — command names permitted when SafeMode is enabled.
 *   Permissions     (map[string][]string) — path prefix → allowed actions (read, write, exec, delete).
 *   RunTimeout      (time.Duration)       — timeout for shell commands; 0 = no timeout.
 *
 * Example:
 *   cfg.Security = SecurityConfig{SafeMode: true, AllowedCommands: []string{"ls", "cat"}}
 */
type SecurityConfig struct {
	SafeMode        bool
	AllowedCommands []string
	Permissions     map[string][]string
	RunTimeout      time.Duration
}

/** SessionConfig holds session recording and replay settings.
 *
 * Fields:
 *   AutoRecord       (bool)   — record every session to a .spmd file automatically.
 *   RecordPath       (string) — file path for auto-recording; empty = auto-generated timestamped name.
 *   ContinuePath     (string) — session file to load as pre-history when starting the REPL.
 *   ResumeLatest     (bool)   — auto-select most recent session file.
 *   ReplayPath       (string) — session file to replay instead of entering the REPL.
 *   ReplayOutputPath (string) — output path for replay recording; empty = auto-generated.
 *   ReplayContinue   (bool)   — drop into the REPL after replay finishes.
 *
 * Example:
 *   cfg.Session = SessionConfig{AutoRecord: true}
 */
type SessionConfig struct {
	AutoRecord       bool
	RecordPath       string
	ContinuePath     string
	ResumeLatest     bool
	ReplayPath       string
	ReplayOutputPath string
	ReplayContinue   bool
}

/** Config holds Harvey's runtime configuration.
 *
 * Fields:
 *   WorkDir      (string)         — root directory Harvey is allowed to read/write; defaults to ".".
 *   SessionsDir  (string)         — directory for session .spmd files; empty = agents/sessions/.
 *   AgentsDir    (string)         — base directory for the agents/skills tree; empty = agents/.
 *   SystemPrompt (string)         — contents of HARVEY.md, injected as the system prompt.
 *   Ollama       (OllamaConfig)   — Ollama connection and model settings.
 *   Llamafile    (LlamafileConfig) — llamafile backend settings.
 *   Security     (SecurityConfig) — safe-mode and permission settings.
 *   Session      (SessionConfig)  — recording and replay settings.
 *   Memory       (MemoryConfig)   — unified memory system settings, including RAG and knowledge base.
 *
 * Example:
 *   cfg := DefaultConfig()
 *   cfg.WorkDir = "/home/user/myproject"
 */
type Config struct {
	WorkDir      string          // workspace root; all file I/O is constrained to this tree
	SessionsDir  string          // directory for .spmd session files; empty = agents/sessions/
	AgentsDir    string          // agents/skills tree root; empty = agents/
	SystemPrompt string          // contents of HARVEY.md, injected as the system prompt
	Routes       []RouteEndpoint // registered remote endpoints; persisted across sessions
	RoutingEnabled bool          // when false, @mentions are rejected with a warning
	ModelCacheDB string          // path to model_cache.db; empty = harvey/model_cache.db
	// Grouped settings
	Ollama   OllamaConfig
	Llamafile LlamafileConfig
	Security  SecurityConfig
	Session   SessionConfig
	// LlamaCpp backend settings
	LlamaCpp LlamaCppConfig
	// Model aliases: short name → ModelAlias (model ID + optional purpose tags)
	ModelAliases map[string]ModelAlias
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
	// Chunking: context-overflow detection and chunked document analysis settings.
	Chunking ChunkConfig
}

/** DefaultConfig returns a Config populated with sensible defaults. WorkDir
 * defaults to "." (the process working directory at startup). Session.AutoRecord
 * defaults to true so every session is saved to agents/sessions/.
 *
 * Returns:
 *   *Config — configuration with default values pre-filled.
 *
 * Example:
 *   cfg := DefaultConfig()
 *   fmt.Println(cfg.Ollama.URL) // "http://localhost:11434"
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
	defaultPerms := map[string][]string{
		".": {"read", "write", "exec", "delete"},
	}
	return &Config{
		WorkDir:      ".",
		ModelAliases: make(map[string]ModelAlias),
		Ollama: OllamaConfig{
			URL: "http://localhost:11434",
		},
		Llamafile: LlamafileConfig{
			URL:            "http://localhost:8080",
			ModelsDir:      llamafileDefaultModelsDir(),
			StartupTimeout: 120 * time.Second,
			GPULayers:      99,
		},
		Security: SecurityConfig{
			SafeMode:        true,
			AllowedCommands: allowed,
			Permissions:     defaultPerms,
			RunTimeout:      5 * time.Minute,
		},
		Session: SessionConfig{
			AutoRecord: true,
		},
		LlamaCpp: LlamaCppConfig{
			URL:          "http://127.0.0.1:8081",
			StartTimeout: 120 * time.Second,
		},
		SyntaxHighlight:      true,
		AutoFormat:           true,
		ToolsEnabled:         true,
		MaxToolCallsPerTurn:  defaultMaxToolCallsPerTurn,
		MaxOutputBytes:       defaultMaxOutputBytes,
		ToolResultCompaction: true,
		Memory: MemoryConfig{
			Enabled:       true,
			TopK:          5,
			InjectOnStart: false,
			BudgetPct:     0.25,
			RollingSummary: RollingSummaryConfig{
				Enabled:   true,
				WarnAtPct: 0.80,
				KeepTurns: 6,
			},
		},
		Chunking: DefaultChunkConfig(),
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
	if entry, ok := c.ModelAliases[strings.ToLower(alias)]; ok {
		return entry.Model
	}
	return alias
}

/** AliasesByTag returns the alias names whose Tags slice contains tag
 * (case-insensitive). Returns nil when no aliases carry that tag.
 *
 * Parameters:
 *   tag (string) — purpose label to search for, e.g. "code".
 *
 * Returns:
 *   []string — sorted list of matching alias names.
 *
 * Example:
 *   names := cfg.AliasesByTag("code") // → ["granite", "qwen-coder"]
 */
func (c *Config) AliasesByTag(tag string) []string {
	if c.ModelAliases == nil {
		return nil
	}
	tag = strings.ToLower(tag)
	var out []string
	for name, entry := range c.ModelAliases {
		for _, t := range entry.Tags {
			if strings.ToLower(t) == tag {
				out = append(out, name)
				break
			}
		}
	}
	sortStrings(out)
	return out
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
	if !c.Security.SafeMode {
		return true
	}
	for _, allowed := range c.Security.AllowedCommands {
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
	for _, existing := range c.Security.AllowedCommands {
		if existing == cmd {
			return
		}
	}
	c.Security.AllowedCommands = append(c.Security.AllowedCommands, cmd)
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
	out := c.Security.AllowedCommands[:0]
	for _, e := range c.Security.AllowedCommands {
		if e != cmd {
			out = append(out, e)
		}
	}
	c.Security.AllowedCommands = out
}

/** ResetAllowedCommands replaces AllowedCommands with the default list.
 *
 * Example:
 *   cfg.ResetAllowedCommands()
 */
func (c *Config) ResetAllowedCommands() {
	c.Security.AllowedCommands = make([]string, len(DefaultAllowedCommands))
	copy(c.Security.AllowedCommands, DefaultAllowedCommands)
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
	if c.Security.Permissions == nil {
		return true // No permissions configured means all allowed
	}

	// Find the most specific matching prefix
	bestMatch := "."
	bestMatchLen := 0

	for prefix := range c.Security.Permissions {
		if strings.HasPrefix(path, prefix) || path == prefix {
			// Check if this is a better (more specific) match
			if len(prefix) > bestMatchLen {
				bestMatch = prefix
				bestMatchLen = len(prefix)
			}
		}
	}

	// Check if the permission is in the list for the best matching prefix
	for _, p := range c.Security.Permissions[bestMatch] {
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
	if c.Security.Permissions == nil {
		c.Security.Permissions = make(map[string][]string)
	}
	c.Security.Permissions[prefix] = perms
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
	if c.Security.Permissions == nil {
		c.Security.Permissions = make(map[string][]string)
	}
	perms := c.Security.Permissions[prefix]
	// Check if permission already exists
	for _, p := range perms {
		if p == perm {
			return
		}
	}
	c.Security.Permissions[prefix] = append(perms, perm)
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
	if c.Security.Permissions == nil {
		return
	}
	perms, ok := c.Security.Permissions[prefix]
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
		delete(c.Security.Permissions, prefix)
	} else {
		c.Security.Permissions[prefix] = out
	}
}

/** ResetPermissions resets permissions to the default (full access to root).
 *
 * Example:
 *   cfg.ResetPermissions()
 */
func (c *Config) ResetPermissions() {
	c.Security.Permissions = map[string][]string{
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
	if c.Security.Permissions == nil {
		return "none"
	}
	perms, ok := c.Security.Permissions[prefix]
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
	if y.Memory.KnowledgeBase.Path != "" {
		cfg.Memory.KnowledgeDB = y.Memory.KnowledgeBase.Path
	}
	if y.SessionsDir != "" {
		cfg.SessionsDir = y.SessionsDir
	}
	if y.AgentsDir != "" {
		cfg.AgentsDir = y.AgentsDir
	}
	if y.Session.AutoRecord != nil {
		cfg.Session.AutoRecord = *y.Session.AutoRecord
	}
	if y.Session.RecordPath != "" {
		cfg.Session.RecordPath = y.Session.RecordPath
	}
	if y.Session.ContinuePath != "" {
		cfg.Session.ContinuePath = y.Session.ContinuePath
	}
	if y.Session.ResumeLatest != nil {
		cfg.Session.ResumeLatest = *y.Session.ResumeLatest
	}
	if y.Session.ReplayPath != "" {
		cfg.Session.ReplayPath = y.Session.ReplayPath
	}
	if y.Session.ReplayOutputPath != "" {
		cfg.Session.ReplayOutputPath = y.Session.ReplayOutputPath
	}
	if y.Session.ReplayContinue != nil {
		cfg.Session.ReplayContinue = *y.Session.ReplayContinue
	}
	if y.ModelCacheDB != "" {
		cfg.ModelCacheDB = y.ModelCacheDB
	}
	// memory.rag: is the canonical RAG configuration location.
	if len(y.Memory.RAG.Stores) > 0 {
		cfg.Memory.RagStores = make([]RagStoreEntry, len(y.Memory.RAG.Stores))
		for i, s := range y.Memory.RAG.Stores {
			// PerPrompt defaults to true (augment every prompt); false disables it.
			skipPerPrompt := s.PerPrompt != nil && !*s.PerPrompt
			cfg.Memory.RagStores[i] = RagStoreEntry{
				Name:           s.Name,
				DBPath:         s.DBPath,
				EmbeddingModel: s.EmbeddingModel,
				ModelMap:       s.ModelMap,
				EmbedderKind:   s.EmbedderKind,
				EmbedderURL:    s.EmbedderURL,
				SkipPerPrompt:  skipPerPrompt,
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
	// Load security settings — only override defaults when explicitly set in YAML.
	if y.Security.Permissions != nil {
		normalised := make(map[string][]string, len(y.Security.Permissions))
		for k, v := range y.Security.Permissions {
			if k != "." && !strings.HasSuffix(k, "/") {
				k = k + "/"
			}
			normalised[k] = v
		}
		cfg.Security.Permissions = normalised
	}
	if y.Security.SafeMode != nil {
		cfg.Security.SafeMode = *y.Security.SafeMode
	}
	if y.SyntaxHighlight != nil {
		cfg.SyntaxHighlight = *y.SyntaxHighlight
	}
	if y.AutoFormat != nil {
		cfg.AutoFormat = *y.AutoFormat
	}
	if len(y.Security.AllowedCommands) > 0 {
		cfg.Security.AllowedCommands = y.Security.AllowedCommands
	}
	if y.Security.RunTimeout != "" {
		if d, err := parseDurationString(y.Security.RunTimeout); err == nil {
			cfg.Security.RunTimeout = d
		}
	}
	if y.Ollama.URL != "" {
		cfg.Ollama.URL = y.Ollama.URL
	}
	if y.Ollama.Model != "" {
		cfg.Ollama.Model = y.Ollama.Model
	}
	if y.Ollama.ContextLength > 0 {
		cfg.Ollama.ContextLength = y.Ollama.ContextLength
	}
	if y.Ollama.Timeout != "" {
		if d, err := parseDurationString(y.Ollama.Timeout); err == nil {
			cfg.Ollama.Timeout = d
		}
	}
	// Load model aliases (backward-compatible: accepts string or struct form in YAML)
	if len(y.ModelAliases) > 0 {
		if cfg.ModelAliases == nil {
			cfg.ModelAliases = make(map[string]ModelAlias)
		}
		for k, v := range y.ModelAliases {
			cfg.ModelAliases[strings.ToLower(k)] = ModelAlias{Model: v.Model, Tags: v.Tags}
		}
	}
	// Load llamafile settings
	if y.Llamafile.ModelsDir != "" {
		cfg.Llamafile.ModelsDir = expandTilde(y.Llamafile.ModelsDir)
	}
	if y.Llamafile.Active != "" {
		cfg.Llamafile.Active = y.Llamafile.Active
	}
	if y.Llamafile.URL != "" {
		cfg.Llamafile.URL = y.Llamafile.URL
	}
	if y.Llamafile.StartupTimeout != "" {
		if d, err := parseDurationString(y.Llamafile.StartupTimeout); err == nil {
			cfg.Llamafile.StartupTimeout = d
		}
	}
	if y.Llamafile.GPULayers != nil {
		cfg.Llamafile.GPULayers = *y.Llamafile.GPULayers
	}
	if y.Llamafile.MaxTokens > 0 {
		cfg.Llamafile.MaxTokens = y.Llamafile.MaxTokens
	}
	for _, m := range y.Llamafile.Models {
		cfg.Llamafile.Models = append(cfg.Llamafile.Models, LlamafileEntry{
			Name: m.Name, Path: m.Path, ContextLength: m.ContextLength,
		})
	}
	// Load llama.cpp settings
	if y.LlamaCpp.ServerBin != "" {
		cfg.LlamaCpp.ServerBin = y.LlamaCpp.ServerBin
	}
	if y.LlamaCpp.ModelsDir != "" {
		cfg.LlamaCpp.ModelsDir = expandTilde(y.LlamaCpp.ModelsDir)
	}
	if y.LlamaCpp.URL != "" {
		cfg.LlamaCpp.URL = y.LlamaCpp.URL
	}
	if y.LlamaCpp.CtxSize > 0 {
		cfg.LlamaCpp.CtxSize = y.LlamaCpp.CtxSize
	}
	if y.LlamaCpp.Threads > 0 {
		cfg.LlamaCpp.Threads = y.LlamaCpp.Threads
	}
	if y.LlamaCpp.GPULayers != nil {
		cfg.LlamaCpp.GPULayers = *y.LlamaCpp.GPULayers
	}
	if y.LlamaCpp.StartTimeout != "" {
		if d, err := parseDurationString(y.LlamaCpp.StartTimeout); err == nil {
			cfg.LlamaCpp.StartTimeout = d
		}
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
	// Load chunking settings.
	if y.Chunking.Enabled != nil {
		cfg.Chunking.Enabled = *y.Chunking.Enabled
	}
	if y.Chunking.Threshold > 0 {
		cfg.Chunking.Threshold = y.Chunking.Threshold
	}
	if y.Chunking.ChunkSizeBytes > 0 {
		cfg.Chunking.ChunkSizeBytes = y.Chunking.ChunkSizeBytes
	}
	if y.Chunking.MaxChunks > 0 {
		cfg.Chunking.MaxChunks = y.Chunking.MaxChunks
	}
	if y.Chunking.Overlap != "" {
		cfg.Chunking.Overlap = y.Chunking.Overlap
	}
	if y.Chunking.STMWarnPct > 0 {
		cfg.Chunking.STMWarnPct = y.Chunking.STMWarnPct
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
	if cfg.Security.Permissions != nil {
		y.Security.Permissions = cfg.Security.Permissions
	}
	y.Security.SafeMode = &cfg.Security.SafeMode
	y.SyntaxHighlight = &cfg.SyntaxHighlight
	y.AutoFormat = &cfg.AutoFormat
	if !cfg.ToolResultCompaction {
		f := false
		y.Tools.ToolResultCompaction = &f
	}
	if len(cfg.Security.AllowedCommands) > 0 {
		y.Security.AllowedCommands = cfg.Security.AllowedCommands
	}
	if cfg.Security.RunTimeout > 0 {
		y.Security.RunTimeout = cfg.Security.RunTimeout.String()
	}
	if cfg.Ollama.Timeout > 0 {
		y.Ollama.Timeout = cfg.Ollama.Timeout.String()
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

	if len(cfg.ModelAliases) > 0 {
		yamlAliases := make(map[string]modelAliasYAML, len(cfg.ModelAliases))
		for k, v := range cfg.ModelAliases {
			yamlAliases[k] = modelAliasYAML{Model: v.Model, Tags: v.Tags}
		}
		y.ModelAliases = yamlAliases
	}

	out, err := yaml.Marshal(&y)
	if err != nil {
		return err
	}
	return os.WriteFile(yamlPath, out, 0644)
}

// ─── ModelAlias ───────────────────────────────────────────────────────────────

/** ModelAlias maps a short alias name to a full model identifier and optional
 * purpose tags. Tags allow @mention routing to resolve by capability rather
 * than exact name (e.g. @code → the alias tagged "code").
 *
 * Fields:
 *   Model (string)   — full model name or path passed to the backend.
 *   Tags  ([]string) — purpose labels, e.g. ["code", "instruct"].
 *
 * Example:
 *   cfg.ModelAliases["granite"] = ModelAlias{Model: "granite3.3:8b", Tags: []string{"code", "instruct"}}
 */
type ModelAlias struct {
	Model string   // full model name / path passed to the backend
	Tags  []string // purpose labels, e.g. ["code", "instruct"]
}

// ─── LlamaCppConfig ───────────────────────────────────────────────────────────

/** LlamaCppConfig holds workspace-level llama.cpp (llama-server) settings.
 *
 * Fields:
 *   ServerBin    (string)        — path to the llama-server binary; defaults to "llama-server" (PATH lookup).
 *   ModelsDir    (string)        — directory scanned for *.gguf model files; defaults to ~/Models.
 *   URL          (string)        — API base URL; defaults to http://127.0.0.1:8081.
 *   CtxSize      (int)           — --ctx-size passed to llama-server; 0 = server default.
 *   Threads      (int)           — --threads; 0 = server default.
 *   GPULayers    (int)           — --n-gpu-layers; 0 = CPU-only.
 *   StartTimeout (time.Duration) — how long to wait for the server to respond on startup; default 120s.
 *
 * Example:
 *   cfg.LlamaCpp = LlamaCppConfig{URL: "http://127.0.0.1:8081", GPULayers: 35}
 */
type LlamaCppConfig struct {
	ServerBin    string        // path to llama-server binary; "" = "llama-server" (PATH lookup)
	ModelsDir    string        // directory for *.gguf files; "" = llamafileDefaultModelsDir()
	URL          string        // API base URL; default http://127.0.0.1:8081
	CtxSize      int           // --ctx-size; 0 = server default
	Threads      int           // --threads; 0 = server default
	GPULayers    int           // --n-gpu-layers; 0 = CPU-only
	StartTimeout time.Duration // startup probe timeout; default 120s
}

// ─── LlamafileEntry and registry helpers ─────────────────────────────────────

/** LlamafileEntry describes a registered llamafile model.
 *
 * Fields:
 *   Name          (string) — short identifier used with /llamafile use and /llamafile add.
 *   Path          (string) — path to the llamafile binary, relative to workspace root or absolute.
 *   ContextLength (int)    — context window size in tokens; 0 means unknown (probed at startup).
 *
 * Example:
 *   e := LlamafileEntry{Name: "qwen-coding", Path: "~/Models/Qwen2.5-Coder-7B.llamafile", ContextLength: 8192}
 */
type LlamafileEntry struct {
	Name          string `yaml:"name"`
	Path          string `yaml:"path"`
	ContextLength int    `yaml:"context_length,omitempty"`
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
	return c.LlamafileEntryByName(c.Llamafile.Active)
}

/** ActiveLlamafileContextLength returns the configured context window size for
 * the active llamafile model, or 0 when unknown (server default applies).
 *
 * Returns:
 *   int — context window in tokens, or 0.
 *
 * Example:
 *   ctxSize := cfg.ActiveLlamafileContextLength()
 *   // ctxSize == 49152 when set in harvey.yaml; 0 means use the model default
 */
func (c *Config) ActiveLlamafileContextLength() int {
	if e := c.ActiveLlamafileEntry(); e != nil {
		return e.ContextLength
	}
	return 0
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
	for i := range c.Llamafile.Models {
		if c.Llamafile.Models[i].Name == name {
			return &c.Llamafile.Models[i]
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
	for i := range c.Llamafile.Models {
		if c.Llamafile.Models[i].Name == e.Name {
			c.Llamafile.Models[i] = e
			return
		}
	}
	c.Llamafile.Models = append(c.Llamafile.Models, e)
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
	entries := make([]llamafileEntryYAML, len(cfg.Llamafile.Models))
	for i, e := range cfg.Llamafile.Models {
		entries[i] = llamafileEntryYAML{Name: e.Name, Path: e.Path, ContextLength: e.ContextLength}
	}
	startupTO := ""
	if cfg.Llamafile.StartupTimeout > 0 && cfg.Llamafile.StartupTimeout != 120*time.Second {
		startupTO = cfg.Llamafile.StartupTimeout.String()
	}
	var gpuLayers *int
	if cfg.Llamafile.GPULayers != 99 { // only persist when overriding the default
		gpuLayers = &cfg.Llamafile.GPULayers
	}
	y.Llamafile = llamafileYAML{
		ModelsDir:      cfg.Llamafile.ModelsDir,
		Active:         cfg.Llamafile.Active,
		URL:            cfg.Llamafile.URL,
		StartupTimeout: startupTO,
		GPULayers:      gpuLayers,
		MaxTokens:      cfg.Llamafile.MaxTokens,
		Models:         entries,
	}
	out, err := yaml.Marshal(&y)
	if err != nil {
		return err
	}
	return os.WriteFile(yamlPath, out, 0644)
}

/** SaveLlamaCppConfig writes the llama.cpp settings back to agents/harvey.yaml.
 * Only the llamacpp: section is updated; all other sections are preserved.
 *
 * Parameters:
 *   ws  (*Workspace) — workspace whose harvey.yaml is updated.
 *   cfg (*Config)    — source of llama.cpp fields to persist.
 *
 * Returns:
 *   error — on I/O or marshal failure.
 *
 * Example:
 *   if err := SaveLlamaCppConfig(ws, cfg); err != nil {
 *       fmt.Println("could not save llamacpp config:", err)
 *   }
 */
func SaveLlamaCppConfig(ws *Workspace, cfg *Config) error {
	yamlPath, err := ws.AbsPath(filepath.Join(harveySubdir, "harvey.yaml"))
	if err != nil {
		return err
	}
	var y harveyYAML
	if data, err := os.ReadFile(yamlPath); err == nil {
		_ = yaml.Unmarshal(data, &y)
	}
	startTO := ""
	if cfg.LlamaCpp.StartTimeout > 0 && cfg.LlamaCpp.StartTimeout != 120*time.Second {
		startTO = cfg.LlamaCpp.StartTimeout.String()
	}
	var gpuLayers *int
	if cfg.LlamaCpp.GPULayers != 0 {
		gpuLayers = &cfg.LlamaCpp.GPULayers
	}
	y.LlamaCpp = llamacppYAML{
		ServerBin:    cfg.LlamaCpp.ServerBin,
		ModelsDir:    cfg.LlamaCpp.ModelsDir,
		URL:          cfg.LlamaCpp.URL,
		CtxSize:      cfg.LlamaCpp.CtxSize,
		Threads:      cfg.LlamaCpp.Threads,
		GPULayers:    gpuLayers,
		StartTimeout: startTO,
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
