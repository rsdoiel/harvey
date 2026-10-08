package harvey

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// ragAgent is an agent with RAG on over a one-chunk store, whose embedding
// requests go to ollamaURL.
func ragAgent(t *testing.T, ollamaURL string) *Agent {
	t.Helper()
	dir := t.TempDir()
	store, err := NewRagStore(filepath.Join(dir, "test.db"), "stub")
	if err != nil {
		t.Fatalf("NewRagStore: %v", err)
	}
	t.Cleanup(func() { store.db.Close() })
	if err := store.Ingest("src.go", []string{"package main"}, stubEmbedder{"stub"}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	cfg := DefaultConfig()
	cfg.Ollama.URL = ollamaURL
	cfg.Memory.RagStores = []RagStoreEntry{{Name: "test", DBPath: filepath.Join(dir, "test.db"), EmbeddingModel: "stub"}}
	cfg.Memory.RagActive = "test"
	ws, _ := NewWorkspace(dir)
	a := NewAgent(cfg, ws)
	a.RagOn = true
	a.Rag = store
	a.hailoDevicePath = filepath.Join(t.TempDir(), "no-hailo0") // not this machine's card
	return a
}

const closedOllama = "http://127.0.0.1:1"

func TestRagAugment_EmbedFailureIsReportedOncePerSession(t *testing.T) {
	a := ragAgent(t, closedOllama)
	var out strings.Builder

	for i := 0; i < 3; i++ {
		got, info := a.ragAugmentTo(&out, "package main", nil)
		if got != "package main" || info != nil {
			t.Fatalf("a failed embedding must leave the prompt alone, got (%q, %v)", got, info)
		}
	}
	if n := strings.Count(out.String(), "RAG"); n != 1 {
		t.Errorf("the failure should be reported once, was reported %d times:\n%s", n, out.String())
	}
	if !strings.Contains(out.String(), "127.0.0.1:1") {
		t.Errorf("the report should carry the cause: %q", out.String())
	}
	if !strings.Contains(out.String(), "without") {
		t.Errorf("the report should say the prompt went without retrieved context: %q", out.String())
	}
}

func TestRagAugment_ADifferentFailureIsReportedAgain(t *testing.T) {
	a := ragAgent(t, closedOllama)
	var out strings.Builder

	a.ragAugmentTo(&out, "package main", nil)
	a.Config.Ollama.URL = "http://127.0.0.1:2"
	a.ragAugmentTo(&out, "package main", nil)

	if n := strings.Count(out.String(), "RAG"); n != 2 {
		t.Errorf("two different failures should be two reports, got %d:\n%s", n, out.String())
	}
}

func TestRagAugment_WorkingEmbedderSaysNothing(t *testing.T) {
	srv := fakeOllamaEmbedServer(t, []float64{1.0, 0.0})
	defer srv.Close()
	a := ragAgent(t, srv.URL)
	var out strings.Builder

	_, info := a.ragAugmentTo(&out, "package main", nil)
	if info == nil {
		t.Fatal("setup: the working embedder should have found a chunk")
	}
	if out.Len() != 0 {
		t.Errorf("nothing to report when embedding works, got %q", out.String())
	}
}

func TestRagAugment_HailoOnlyMachineSaysHailoCannotEmbed(t *testing.T) {
	rs := newRecordingServer(t, true)
	a := ragAgent(t, closedOllama)
	a.Config.Hailo = HailoConfig{URL: rs.URL, URLSet: true}
	var out strings.Builder

	a.ragAugmentTo(&out, "package main", nil)

	if !strings.Contains(out.String(), "Hailo cannot embed; no Ollama at") {
		t.Errorf("a Hailo-only machine should be told plainly, got %q", out.String())
	}
}

func TestRunChatTurn_RagFailureIsReportedOnceAcrossTurns(t *testing.T) {
	a := ragAgent(t, closedOllama)
	a.Client = &mockLLMClient{reply: "ok"}
	var out strings.Builder

	for i := 0; i < 2; i++ {
		if _, _, err := a.runChatTurn(context.Background(), "package main", &out, newReader(""), false, ""); err != nil {
			t.Fatalf("runChatTurn: %v", err)
		}
	}
	if n := strings.Count(out.String(), "RAG"); n != 1 {
		t.Errorf("two turns, one failure: want one report, got %d:\n%s", n, out.String())
	}
}
