// Package harvey — config_yaml.go defines the on-disk YAML adapter types
// that map between harvey.yaml and the in-memory Config struct.
// These types are only used by LoadHarveyYAML and the Save* functions in
// config.go; they are never exposed to callers outside this package.
package harvey

import "gopkg.in/yaml.v3"

// modelAliasYAML accepts both string form ("granite3.3:8b") and struct form
// ({model: granite3.3:8b, tags: [code, instruct]}) in harvey.yaml.
// When marshalled, it emits a plain string when Tags is empty, otherwise a struct.
type modelAliasYAML struct {
	Model  string
	Engine string
	Tags   []string
}

func (m *modelAliasYAML) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		// Old string form: granite: granite3.3:8b
		m.Model = value.Value
		return nil
	}
	type plain struct {
		Model  string   `yaml:"model"`
		Engine string   `yaml:"engine,omitempty"`
		Tags   []string `yaml:"tags,omitempty"`
	}
	var p plain
	if err := value.Decode(&p); err != nil {
		return err
	}
	m.Model = p.Model
	m.Engine = p.Engine
	m.Tags = p.Tags
	return nil
}

func (m modelAliasYAML) MarshalYAML() (interface{}, error) {
	if m.Engine == "" && len(m.Tags) == 0 {
		return m.Model, nil // emit as plain string for readability (backward compat)
	}
	return struct {
		Model  string   `yaml:"model"`
		Engine string   `yaml:"engine,omitempty"`
		Tags   []string `yaml:"tags,omitempty"`
	}{m.Model, m.Engine, m.Tags}, nil
}

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
	Active         string            `yaml:"active,omitempty"`
	Stores         []ragStoreYAML    `yaml:"stores,omitempty"`
	Enabled        bool              `yaml:"enabled"`
	DBPath         string            `yaml:"db_path,omitempty"`
	EmbeddingModel string            `yaml:"embedding_model,omitempty"`
	ModelMap       map[string]string `yaml:"model_map,omitempty"`
}

// ollamaYAML is the on-disk representation of the ollama: section in harvey.yaml.
type ollamaYAML struct {
	URL           string `yaml:"url,omitempty"`
	Model         string `yaml:"model,omitempty"`
	ContextLength int    `yaml:"context_length,omitempty"`
	Timeout       string `yaml:"timeout,omitempty"` // e.g. "0", "10m"; 0 or empty = no timeout
}

// securityYAML is the on-disk representation of the security: section in harvey.yaml.
type securityYAML struct {
	SafeMode        *bool               `yaml:"safe_mode,omitempty"`
	AllowedCommands []string            `yaml:"allowed_commands,omitempty"`
	Permissions     map[string][]string `yaml:"permissions,omitempty"`
	RunTimeout      string              `yaml:"run_timeout,omitempty"` // e.g. "5m", "300s"
}

// sessionYAML is the on-disk representation of the session: section in harvey.yaml.
type sessionYAML struct {
	AutoRecord       *bool  `yaml:"auto_record,omitempty"`
	RecordPath       string `yaml:"record_path,omitempty"`
	ContinuePath     string `yaml:"continue_path,omitempty"`
	ResumeLatest     *bool  `yaml:"resume_latest,omitempty"`
	ReplayPath       string `yaml:"replay_path,omitempty"`
	ReplayOutputPath string `yaml:"replay_output_path,omitempty"`
	ReplayContinue   *bool  `yaml:"replay_continue,omitempty"`
}

// harveyYAML is the on-disk representation of harvey/harvey.yaml.
type harveyYAML struct {
	SessionsDir     string                    `yaml:"sessions_dir"`
	AgentsDir       string                    `yaml:"agents_dir"`
	ModelCacheDB    string                    `yaml:"model_cache_db"`
	SyntaxHighlight *bool                     `yaml:"syntax_highlight,omitempty"`
	AutoFormat      *bool                     `yaml:"auto_format,omitempty"`
	Tools           toolsYAML                 `yaml:"tools,omitempty"`
	ModelAliases    map[string]modelAliasYAML `yaml:"model_aliases,omitempty"`
	Ollama          ollamaYAML                `yaml:"ollama,omitempty"`
	Security        securityYAML              `yaml:"security,omitempty"`
	Session         sessionYAML               `yaml:"session,omitempty"`
	Memory          memoryYAML                `yaml:"memory,omitempty"`
	Llamafile       llamafileYAML             `yaml:"llamafile,omitempty"`
	LlamaCpp        llamacppYAML              `yaml:"llamacpp,omitempty"`
	Chunking        chunkingYAML              `yaml:"chunking,omitempty"`
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

type llamacppYAML struct {
	ServerBin    string `yaml:"server_bin,omitempty"`    // path to llama-server; "" = PATH lookup
	ModelsDir    string `yaml:"models_dir,omitempty"`    // directory for *.gguf files
	URL          string `yaml:"url,omitempty"`           // API base URL; default http://127.0.0.1:8081
	CtxSize      int    `yaml:"ctx_size,omitempty"`      // --ctx-size; 0 = server default
	Threads      int    `yaml:"threads,omitempty"`       // --threads; 0 = server default
	GPULayers    *int   `yaml:"gpu_layers,omitempty"`    // --n-gpu-layers; nil = not set (0 = CPU-only)
	StartTimeout string `yaml:"start_timeout,omitempty"` // e.g. "120s", "2m"
	PinCPU       bool   `yaml:"pin_cpu,omitempty"`       // taskset -c 0-(threads-1); default false
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
	Enabled        *bool              `yaml:"enabled,omitempty"`
	TopK           int                `yaml:"top_k,omitempty"`
	InjectOnStart  *bool              `yaml:"inject_on_start,omitempty"`
	BudgetPct      float64            `yaml:"budget_pct,omitempty"`
	RollingSummary rollingSummaryYAML `yaml:"rolling_summary,omitempty"`
	RAG            ragYAML            `yaml:"rag,omitempty"`
	KnowledgeBase  knowledgeBaseYAML  `yaml:"knowledge_base,omitempty"`
}
