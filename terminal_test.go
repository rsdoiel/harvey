package harvey

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// makeTestTree creates a temporary directory tree for completion tests:
//
//	root/
//	  harvey/
//	    main.go
//	    README.md
//	  docs/
//	    guide.pdf
//	    notes.txt
//	  .hidden/
//	  README.md
//	  report.pdf
func makeTestTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dirs := []string{"harvey", "docs", ".hidden"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"harvey/main.go":   "package main",
		"harvey/README.md": "# harvey",
		"docs/guide.pdf":   "%PDF",
		"docs/notes.txt":   "notes",
		"README.md":        "# root",
		"report.pdf":       "%PDF",
	}
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func sortedMatches(matches []string) []string {
	sort.Strings(matches)
	return matches
}

func TestWorkspacePathCandidates_rootListing(t *testing.T) {
	root := makeTestTree(t)
	got := sortedMatches(workspacePathCandidates(root, "", false, nil))
	// Should see dirs and files at root, no hidden entries.
	want := []string{"README.md", "docs/", "harvey/", "report.pdf"}
	if len(got) != len(want) {
		t.Fatalf("root listing: got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("root listing[%d]: got %q, want %q", i, g, want[i])
		}
	}
}

func TestWorkspacePathCandidates_prefixFilter(t *testing.T) {
	root := makeTestTree(t)
	got := sortedMatches(workspacePathCandidates(root, "h", false, nil))
	want := []string{"harvey/"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("prefix 'h': got %v, want %v", got, want)
	}
}

func TestWorkspacePathCandidates_subdirListing(t *testing.T) {
	root := makeTestTree(t)
	got := sortedMatches(workspacePathCandidates(root, "harvey/", false, nil))
	want := []string{"harvey/README.md", "harvey/main.go"}
	if len(got) != len(want) {
		t.Fatalf("harvey/ listing: got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("harvey/[%d]: got %q, want %q", i, g, want[i])
		}
	}
}

func TestWorkspacePathCandidates_onlyDirs(t *testing.T) {
	root := makeTestTree(t)
	got := sortedMatches(workspacePathCandidates(root, "", true, nil))
	want := []string{"docs/", "harvey/"}
	if len(got) != len(want) {
		t.Fatalf("onlyDirs: got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("onlyDirs[%d]: got %q, want %q", i, g, want[i])
		}
	}
}

func TestWorkspacePathCandidates_extFilter(t *testing.T) {
	root := makeTestTree(t)
	pdfOnly := map[string]bool{".pdf": true}
	got := sortedMatches(workspacePathCandidates(root, "", false, pdfOnly))
	// Directories are always included so the user can navigate into them;
	// regular files are filtered to the given extensions.
	want := []string{"docs/", "harvey/", "report.pdf"}
	if len(got) != len(want) {
		t.Fatalf("extFilter .pdf at root: got %v, want %v", got, want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("extFilter[%d]: got %q, want %q", i, g, want[i])
		}
	}
}

func TestWorkspacePathCandidates_extFilterSubdir(t *testing.T) {
	root := makeTestTree(t)
	pdfOnly := map[string]bool{".pdf": true}
	got := sortedMatches(workspacePathCandidates(root, "docs/", false, pdfOnly))
	want := []string{"docs/guide.pdf"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("extFilter .pdf in docs/: got %v, want %v", got, want)
	}
}

func TestWorkspacePathCandidates_hiddenExcluded(t *testing.T) {
	root := makeTestTree(t)
	got := workspacePathCandidates(root, ".", false, nil)
	for _, m := range got {
		if filepath.Base(m) == ".hidden" || m == ".hidden/" {
			t.Errorf("hidden entry leaked into completions: %q", m)
		}
	}
}

func TestWorkspacePathCandidates_escapeBlocked(t *testing.T) {
	root := makeTestTree(t)
	got := workspacePathCandidates(root, "../../etc/", false, nil)
	if len(got) != 0 {
		t.Errorf("path escape should produce no completions, got %v", got)
	}
}

// ─── activeModelLabel ────────────────────────────────────────────────────────

func TestActiveModelLabel_noBackend(t *testing.T) {
	a := &Agent{Config: DefaultConfig()}
	if got := activeModelLabel(a); got != "none" {
		t.Errorf("got %q want %q", got, "none")
	}
}

func TestActiveModelLabel_ollama(t *testing.T) {
	a := &Agent{Config: DefaultConfig()}
	a.Config.Ollama.Model = "llama3.2:3b"
	if got := activeModelLabel(a); got != "llama3.2:3b (ollama)" {
		t.Errorf("got %q want %q", got, "llama3.2:3b (ollama)")
	}
}

// ─── probeActiveBackend ──────────────────────────────────────────────────────

func TestProbeActiveBackend_noBackendConfigured(t *testing.T) {
	a := &Agent{Config: DefaultConfig()}
	// Neither LlamafileActive nor OllamaModel set — must return false without
	// making any network call.
	if probeActiveBackend(a) {
		t.Error("expected false when no backend is configured")
	}
}

func TestProbeActiveBackend_ollamaNotRunning(t *testing.T) {
	a := &Agent{Config: DefaultConfig()}
	a.Config.Ollama.Model = "llama3.2:3b"
	a.Config.Ollama.URL = "http://127.0.0.1:19992" // nothing listening here
	if probeActiveBackend(a) {
		t.Error("expected false when ollama server is not running")
	}
}

// ─── attemptModelSwitch ──────────────────────────────────────────────────────

func TestAttemptModelSwitch_notFound(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	var buf strings.Builder
	switched, err := attemptModelSwitch(a, "unknown-model", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if switched {
		t.Error("expected switched=false for unknown model name")
	}
}

func TestAttemptModelSwitch_aliasLookupCaseInsensitive(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	// Register an alias. Aliases are stored with lowercase keys.
	cfg.ModelAliases = map[string]ModelAlias{"coder": {Model: "qwen2.5-coder:7b"}}
	cfg.Ollama.URL = "http://localhost:11434" // won't connect, just wiring
	a := NewAgent(cfg, ws)
	var buf strings.Builder
	// "CODER" should match the alias stored as "coder".
	switched, _ := attemptModelSwitch(a, "CODER", &buf)
	if !switched {
		t.Error("expected switched=true for case-insensitive alias match")
	}
}

// ─── @mention local model switch (REPL-level) ─────────────────────────────────

// TestAtMention_tagRouting_resolvesTaggedAlias verifies that @code switches to
// the model whose alias is tagged "code", even though no alias is literally
// named "code". This exercises the Step 3 tag-lookup path in the REPL.
func TestAtMention_tagRouting_resolvesTaggedAlias(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.ModelAliases = map[string]ModelAlias{
		"granite": {Model: "granite3.3:8b", Tags: []string{"code", "instruct"}},
	}
	a := NewAgent(cfg, ws)
	a.Client = newOllamaLLMClient("http://127.0.0.1:1", "granite3.3:8b", 0)

	// @code should not match any alias by name, but should resolve via tag "code"
	// → granite alias → granite3.3:8b → attemptModelSwitch succeeds.
	tagModel, ok := resolveTagAlias(a.Config, "code")
	if !ok {
		t.Fatal("resolveTagAlias(code): expected ok=true")
	}
	if tagModel != "granite3.3:8b" {
		t.Errorf("resolveTagAlias(code) = %q, want granite3.3:8b", tagModel)
	}

	var buf strings.Builder
	switched, err := attemptModelSwitch(a, tagModel, &buf)
	if !switched {
		t.Errorf("attemptModelSwitch(%q): expected switched=true", tagModel)
	}
	if err != nil {
		t.Errorf("attemptModelSwitch: %v", err)
	}
}

// TestAtMention_tagRouting_noTagMatch verifies that resolveTagAlias returns
// ("", false) when no alias carries the requested tag — preserving the
// existing "not found" behaviour.
func TestAtMention_tagRouting_noTagMatch(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.ModelAliases = map[string]ModelAlias{
		"qwen": {Model: "qwen3:8b", Tags: []string{"reasoning"}},
	}
	a := NewAgent(cfg, ws)

	_, ok := resolveTagAlias(a.Config, "code")
	if ok {
		t.Error("expected resolveTagAlias(code) = false when no alias carries 'code' tag")
	}
}

// TestAtMention_tagRouting_exactAliasPrecedence verifies that @granite uses
// the exact alias (Step 2) before tag lookup (Step 3)..
func TestAtMention_tagRouting_exactAliasPrecedence(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	cfg := DefaultConfig()
	cfg.ModelAliases = map[string]ModelAlias{
		"granite": {Model: "granite3.3:8b", Tags: []string{"code", "instruct"}},
	}
	a := NewAgent(cfg, ws)
	a.Client = newOllamaLLMClient("http://127.0.0.1:1", "granite3.3:8b", 0)

	// @granite → exact alias hit via ResolveModelAlias before tag lookup runs.
	resolved := a.Config.ResolveModelAlias("granite")
	if resolved != "granite3.3:8b" {
		t.Errorf("exact alias resolution: got %q, want granite3.3:8b", resolved)
	}

	var buf strings.Builder
	switched, err := attemptModelSwitch(a, "granite", &buf)
	if !switched {
		t.Errorf("attemptModelSwitch(granite): expected switched=true via alias")
	}
	if err != nil {
		t.Errorf("attemptModelSwitch: %v", err)
	}
}

// newTestBufioReader wraps a string in a bufio.Reader for terminal test helpers.
func newTestBufioReader(s string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(s))
}

// ─── routeSpinnerLabel tests ──────────────────────────────────────────────────

func TestRouteSpinnerLabel_includesModel(t *testing.T) {
	ep := &RouteEndpoint{Name: "pi2", Model: "llama3.1:8b"}
	label := routeSpinnerLabel("pi2", ep)
	if !strings.Contains(label, "llama3.1:8b") {
		t.Errorf("expected model name in spinner label, got: %q", label)
	}
	if !strings.Contains(label, "pi2") {
		t.Errorf("expected route name in spinner label, got: %q", label)
	}
}

func TestRouteSpinnerLabel_fallbackWhenNoModel(t *testing.T) {
	ep := &RouteEndpoint{Name: "pi2", Model: ""}
	label := routeSpinnerLabel("pi2", ep)
	if !strings.Contains(label, "pi2") {
		t.Errorf("expected route name in fallback label, got: %q", label)
	}
	if !strings.Contains(label, "working") {
		t.Errorf("expected 'working' in fallback label, got: %q", label)
	}
}

// ─── --continue / --resume model hint extraction ──────────────────────────────

// ─── ragAugment ───────────────────────────────────────────────────────────────

// fakeOllamaEmbedServer starts an httptest server that responds to POST
// /api/embed with a fixed embedding vector, mimicking the Ollama embed API.
func fakeOllamaEmbedServer(t *testing.T, vec []float64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}
		type resp struct {
			Embeddings [][]float64 `json:"embeddings"`
		}
		_ = json.NewEncoder(w).Encode(resp{Embeddings: [][]float64{vec}})
	}))
}

func TestRAGAugment_RagOff(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	a.RagOn = false
	out, info := a.ragAugment("hello", nil)
	if out != "hello" || info != nil {
		t.Errorf("expected (prompt, nil) when RagOn=false, got (%q, %v)", out, info)
	}
}

func TestRAGAugment_NilStore(t *testing.T) {
	ws, _ := NewWorkspace(t.TempDir())
	a := NewAgent(DefaultConfig(), ws)
	a.RagOn = true
	a.Rag = nil
	out, info := a.ragAugment("hello", nil)
	if out != "hello" || info != nil {
		t.Errorf("expected (prompt, nil) when Rag=nil, got (%q, %v)", out, info)
	}
}

func TestRAGAugment_ReturnsInfo(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Build a real RagStore using the stubEmbedder (defined in code_chunkers_test.go).
	store, err := NewRagStore(dbPath, "stub")
	if err != nil {
		t.Fatalf("NewRagStore: %v", err)
	}
	defer store.db.Close()
	if err := store.Ingest("src.go", []string{"package main"}, stubEmbedder{"stub"}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	// Fake Ollama embed endpoint returning the same unit vector as the stub embedder.
	srv := fakeOllamaEmbedServer(t, []float64{1.0, 0.0})
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Ollama.URL = srv.URL
	cfg.Memory.RagStores = []RagStoreEntry{
		{Name: "test", DBPath: dbPath, EmbeddingModel: "stub"},
	}
	cfg.Memory.RagActive = "test"

	ws, _ := NewWorkspace(dir)
	a := NewAgent(cfg, ws)
	a.RagOn = true
	a.Rag = store

	augmented, info := a.ragAugment("package main", nil)
	if info == nil {
		t.Fatal("expected non-nil RAGAugmentInfo when chunks found")
	}
	if info.StoreName != "test" {
		t.Errorf("StoreName: got %q, want %q", info.StoreName, "test")
	}
	if info.Chunks < 1 {
		t.Errorf("Chunks: got %d, want >= 1", info.Chunks)
	}
	if info.TopScore < ragMinScore {
		t.Errorf("TopScore %.2f is below ragMinScore %.2f", info.TopScore, ragMinScore)
	}
	if !strings.Contains(augmented, "package main") {
		t.Errorf("augmented prompt missing original text: %s", augmented)
	}
	if !strings.Contains(augmented, "### Context") {
		t.Errorf("augmented prompt missing context header: %s", augmented)
	}
	// Sources must be populated with at least the one ingested source path.
	if len(info.Sources) == 0 {
		t.Error("expected info.Sources to be non-empty")
	}
	if info.Sources[0].Source != "src.go" {
		t.Errorf("Sources[0].Source = %q, want %q", info.Sources[0].Source, "src.go")
	}
}

func TestRAGAugment_SkipPerPrompt(t *testing.T) {
	// SkipPerPrompt=true must cause ragAugment to return the prompt unchanged
	// without querying the store or calling the embedder.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := NewRagStore(dbPath, "stub")
	if err != nil {
		t.Fatalf("NewRagStore: %v", err)
	}
	defer store.db.Close()
	// Ingest a chunk so the store is non-empty; if SkipPerPrompt is ignored,
	// ragAugment would embed the query and find this chunk.
	if err := store.Ingest("src.go", []string{"package main"}, stubEmbedder{"stub"}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	srv := fakeOllamaEmbedServer(t, []float64{1.0, 0.0})
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Ollama.URL = srv.URL
	cfg.Memory.RagStores = []RagStoreEntry{
		{Name: "test", DBPath: dbPath, EmbeddingModel: "stub", SkipPerPrompt: true},
	}
	cfg.Memory.RagActive = "test"

	ws, _ := NewWorkspace(dir)
	a := NewAgent(cfg, ws)
	a.RagOn = true
	a.Rag = store

	out, info := a.ragAugment("package main", nil)
	if out != "package main" {
		t.Errorf("SkipPerPrompt: expected prompt unchanged; got %q", out)
	}
	if info != nil {
		t.Errorf("SkipPerPrompt: expected nil info; got %v", info)
	}
}

func TestRAGAugment_BudgetTracker_TrimsLowerScoredChunks(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := NewRagStore(dbPath, "stub")
	if err != nil {
		t.Fatalf("NewRagStore: %v", err)
	}
	defer store.db.Close()

	// content1 scores 1.0 against the query vector [1,0]; content2 scores 0.8
	// (still above ragMinScore=0.3, so both are "relevant") — but lower-ranked.
	// Each is ~50 tokens (200 chars / 4) so a tracker with Total=60 fits only
	// the first.
	content1 := strings.Repeat("a", 200)
	content2 := strings.Repeat("b", 200)
	ingestEmbedder := &precomputedEmbedder{
		name: "stub",
		vectors: map[string][]float64{
			content1: {1.0, 0.0},
			content2: {0.8, 0.6},
		},
	}
	if err := store.Ingest("src.go", []string{content1, content2}, ingestEmbedder); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	// Query embedding is fixed at [1,0], matching content1 exactly.
	srv := fakeOllamaEmbedServer(t, []float64{1.0, 0.0})
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Ollama.URL = srv.URL
	cfg.Memory.RagStores = []RagStoreEntry{
		{Name: "test", DBPath: dbPath, EmbeddingModel: "stub"},
	}
	cfg.Memory.RagActive = "test"

	ws, _ := NewWorkspace(dir)
	a := NewAgent(cfg, ws)
	a.RagOn = true
	a.Rag = store

	tracker := NewBudgetTracker(60)
	augmented, info := a.ragAugment("query", tracker)
	if info == nil {
		t.Fatal("expected non-nil RAGAugmentInfo")
	}
	if info.Chunks != 1 {
		t.Errorf("info.Chunks = %d, want 1 (only the highest-scored chunk fits)", info.Chunks)
	}
	if !strings.Contains(augmented, content1) {
		t.Error("expected augmented prompt to contain the highest-scored chunk (content1)")
	}
	if strings.Contains(augmented, content2) {
		t.Error("expected augmented prompt to omit the lower-scored chunk (content2)")
	}
	if !strings.Contains(augmented, "omitted") {
		t.Errorf("expected an omission note in the augmented prompt, got: %s", augmented)
	}
	if !strings.Contains(augmented, "query") {
		t.Error("expected augmented prompt to still end with the original prompt text")
	}
}

// TestBudgetTracker_SharedAcrossRAGAndFileInjection proves the BudgetTracker
// is a genuinely shared per-turn pool, not two independent budgets: RAG
// consuming most of it must measurably reduce what injectOrChunk can still
// fit afterward in the same turn.
func TestBudgetTracker_SharedAcrossRAGAndFileInjection(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := NewRagStore(dbPath, "stub")
	if err != nil {
		t.Fatalf("NewRagStore: %v", err)
	}
	defer store.db.Close()

	// ~200 tokens (800 chars / 4).
	ragContent := strings.Repeat("r", 800)
	ingestEmbedder := &precomputedEmbedder{
		name:    "stub",
		vectors: map[string][]float64{ragContent: {1.0, 0.0}},
	}
	if err := store.Ingest("src.go", []string{ragContent}, ingestEmbedder); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	srv := fakeOllamaEmbedServer(t, []float64{1.0, 0.0})
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.Ollama.URL = srv.URL
	cfg.Ollama.ContextLength = 8192
	cfg.Chunking = DefaultChunkConfig()
	cfg.Memory.RagStores = []RagStoreEntry{
		{Name: "test", DBPath: dbPath, EmbeddingModel: "stub"},
	}
	cfg.Memory.RagActive = "test"

	ws, _ := NewWorkspace(dir)
	a := NewAgent(cfg, ws)
	a.RagOn = true
	a.Rag = store
	a.Client = &mockLLMClient{reply: "ok"}

	// ~40 tokens (160 chars / 4) — comfortably fits a fresh 8192-token budget,
	// but not the ~20 tokens left over after RAG consumes ~200 from a
	// deliberately tight shared tracker.
	fileContent := strings.Repeat("f", 160)
	if err := os.WriteFile(filepath.Join(dir, "small.md"), []byte(fileContent), 0o644); err != nil {
		t.Fatal(err)
	}

	tracker := NewBudgetTracker(220)
	var out strings.Builder
	augmented, _ := a.ragAugment("review small.md", tracker)
	if !strings.Contains(augmented, ragContent) {
		t.Fatalf("expected RAG content in augmented prompt: %s", augmented)
	}

	final := a.injectOrChunk(context.Background(), augmented, &out, tracker)
	if strings.Contains(final, "### File: small.md") {
		t.Error("expected small.md to be skipped — RAG already consumed most of the shared budget")
	}
	if !strings.Contains(final, "small.md") {
		t.Errorf("expected an omission note naming small.md; got:\n%s", final)
	}
}
