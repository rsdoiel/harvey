// Package harvey — config_yaml.go defines the on-disk YAML adapter types
// that map between harvey.yaml and the in-memory Config struct.
// These types are only used by LoadHarveyYAML and the Save* functions in
// config.go; they are never exposed to callers outside this package.
package harvey

// ragStoreYAML is the on-disk representation of one entry under rag.stores.
type ragStoreYAML struct {
	Name           string            `yaml:"name"`
	DBPath         string            `yaml:"db_path"`
	EmbeddingModel string            `yaml:"embedding_model"`
	ModelMap       map[string]string `yaml:"model_map,omitempty"`
	EmbedderKind   string            `yaml:"embedder_kind,omitempty"`
	EmbedderURL    string            `yaml:"embedder_url,omitempty"`
	PerPrompt      *bool             `yaml:"per_prompt,omitempty"` // nil or true = augment; false = skip
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
type harveyYAML struct {
	SessionsDir     string              `yaml:"sessions_dir"`
	AgentsDir       string              `yaml:"agents_dir"`
	AutoRecord      *bool               `yaml:"auto_record"` // nil = not set (keep default)
	ModelCacheDB    string              `yaml:"model_cache_db"`
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
	Chunking        chunkingYAML        `yaml:"chunking,omitempty"`
}

type chunkingYAML struct {
	Enabled        *bool   `yaml:"enabled,omitempty"`
	Threshold      float64 `yaml:"threshold,omitempty"`
	ChunkSizeBytes int     `yaml:"chunk_size_bytes,omitempty"`
	MaxChunks      int     `yaml:"max_chunks,omitempty"`
	Overlap        string  `yaml:"overlap,omitempty"`
	STMWarnPct     float64 `yaml:"stm_warn_pct,omitempty"`
}

type toolsYAML struct {
	Enabled              *bool `yaml:"enabled,omitempty"`
	MaxToolCallsPerTurn  int   `yaml:"max_tool_calls_per_turn,omitempty"`
	MaxOutputBytes       int   `yaml:"max_output_bytes,omitempty"`
	ToolResultCompaction *bool `yaml:"tool_result_compaction,omitempty"`
}

type llamafileEntryYAML struct {
	Name          string `yaml:"name"`
	Path          string `yaml:"path"`
	ContextLength int    `yaml:"context_length,omitempty"`
}

type llamafileYAML struct {
	ModelsDir      string               `yaml:"models_dir,omitempty"`
	Active         string               `yaml:"active,omitempty"`
	URL            string               `yaml:"url,omitempty"`
	StartupTimeout string               `yaml:"startup_timeout,omitempty"` // e.g. "120s", "2m"
	GPULayers      *int                 `yaml:"gpu_layers,omitempty"`      // -ngl value; nil = use default (99)
	MaxTokens      int                  `yaml:"max_tokens,omitempty"`      // cap on tokens per completion; 0 = no limit
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
