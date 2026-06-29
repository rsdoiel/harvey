package harvey

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOllamaBackend_Name(t *testing.T) {
	b := NewOllamaBackend("http://127.0.0.1:11434", 10*time.Second, t.TempDir())
	if b.Name() != "ollama" {
		t.Errorf("Name() = %q, want %q", b.Name(), "ollama")
	}
}

func TestOllamaBackend_BaseURL(t *testing.T) {
	const wantURL = "http://127.0.0.1:11434"
	b := NewOllamaBackend(wantURL, 10*time.Second, t.TempDir())
	if b.BaseURL() != wantURL {
		t.Errorf("BaseURL() = %q, want %q", b.BaseURL(), wantURL)
	}
}

func TestOllamaBackend_SetActiveModel(t *testing.T) {
	b := NewOllamaBackend("http://127.0.0.1:11434", 10*time.Second, t.TempDir())
	if b.ActiveModel() != "" {
		t.Errorf("ActiveModel() before set = %q, want %q", b.ActiveModel(), "")
	}
	b.SetActiveModel("granite3.3:8b")
	if b.ActiveModel() != "granite3.3:8b" {
		t.Errorf("ActiveModel() = %q, want %q", b.ActiveModel(), "granite3.3:8b")
	}
}

func TestOllamaBackend_StartedByHarvey_Default(t *testing.T) {
	b := NewOllamaBackend("http://127.0.0.1:11434", 10*time.Second, t.TempDir())
	if b.StartedByHarvey() {
		t.Error("StartedByHarvey() should be false on a new backend")
	}
}

func TestOllamaBackend_IsRunning_DefaultFalse(t *testing.T) {
	b := NewOllamaBackend("http://127.0.0.1:11434", 10*time.Second, t.TempDir())
	if b.IsRunning() {
		t.Error("IsRunning() should be false before any Detect call")
	}
}

func TestOllamaBackend_Detect_ReachableServer(t *testing.T) {
	// Serve a minimal /api/tags response to simulate a running Ollama.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	b := NewOllamaBackend(srv.URL, 5*time.Second, t.TempDir())
	if !b.Detect() {
		t.Error("Detect() should return true when /api/tags returns 200")
	}
	if !b.IsRunning() {
		t.Error("IsRunning() should be true after a successful Detect")
	}
}

func TestOllamaBackend_Detect_UnreachableServer(t *testing.T) {
	b := NewOllamaBackend("http://127.0.0.1:19999", 1*time.Second, t.TempDir())
	if b.Detect() {
		t.Error("Detect() should return false when server is not reachable")
	}
	if b.IsRunning() {
		t.Error("IsRunning() should be false after a failed Detect")
	}
}

func TestOllamaBackend_ListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{
					{"name": "granite3.3:8b", "size": int64(4_500_000_000), "details": map[string]any{}},
					{"name": "qwen3:8b", "size": int64(5_200_000_000), "details": map[string]any{}},
				},
			})
		case "/api/ps":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b := NewOllamaBackend(srv.URL, 5*time.Second, t.TempDir())
	models, err := b.ListModels()
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("ListModels returned %d models, want 2", len(models))
	}
	for _, m := range models {
		if m.Engine != "ollama" {
			t.Errorf("model %q: Engine = %q, want %q", m.Name, m.Engine, "ollama")
		}
		if m.Path != "" {
			t.Errorf("model %q: Path should be empty for Ollama, got %q", m.Name, m.Path)
		}
	}
	if models[0].Name != "granite3.3:8b" {
		t.Errorf("models[0].Name = %q, want %q", models[0].Name, "granite3.3:8b")
	}
}

func TestOllamaBackend_NewClient_NoModel(t *testing.T) {
	b := NewOllamaBackend("http://127.0.0.1:11434", 10*time.Second, t.TempDir())
	_, err := b.NewClient()
	if err == nil {
		t.Error("NewClient() should return an error when no active model is set")
	}
}

func TestOllamaBackend_NewClient_WithModel(t *testing.T) {
	b := NewOllamaBackend("http://127.0.0.1:11434", 10*time.Second, t.TempDir())
	b.SetActiveModel("granite3.3:8b")
	client, err := b.NewClient()
	if err != nil {
		t.Fatalf("NewClient(): %v", err)
	}
	if client == nil {
		t.Error("NewClient() returned nil client")
	}
}

func TestOllamaBackend_Stop_NotStarted(t *testing.T) {
	b := NewOllamaBackend("http://127.0.0.1:11434", 10*time.Second, t.TempDir())
	if err := b.Stop(); err != nil {
		t.Errorf("Stop() on un-started backend should be no-op, got: %v", err)
	}
}
