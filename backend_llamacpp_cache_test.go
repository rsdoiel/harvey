package harvey

import (
	"os"
	"path/filepath"
	"testing"
)

// llama.cpp keeps the models it downloads (-hf) in its own cache, so that is
// where Harvey looks unless llama_cpp.models_dir says otherwise. ~/Models is a
// llamafile convention and is not scanned for .gguf files by default.

// isolateLlamaCppEnv points HOME at a fresh directory and clears the variables
// that move the llama.cpp cache, returning the fake home.
func isolateLlamaCppEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LLAMA_CACHE", "")
	t.Setenv("XDG_CACHE_HOME", "")
	return home
}

func writeStubGGUF(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLlamaCppDefaultModelsDir_HomeCache(t *testing.T) {
	home := isolateLlamaCppEnv(t)
	want := filepath.Join(home, ".cache", "llama.cpp")
	if got := llamaCppDefaultModelsDir(); got != want {
		t.Errorf("default dir = %q, want %q", got, want)
	}
}

func TestLlamaCppDefaultModelsDir_XDGCacheHome(t *testing.T) {
	isolateLlamaCppEnv(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdg)
	want := filepath.Join(xdg, "llama.cpp")
	if got := llamaCppDefaultModelsDir(); got != want {
		t.Errorf("default dir = %q, want %q", got, want)
	}
}

func TestLlamaCppDefaultModelsDir_LlamaCacheWins(t *testing.T) {
	isolateLlamaCppEnv(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cache := t.TempDir()
	t.Setenv("LLAMA_CACHE", cache)
	if got := llamaCppDefaultModelsDir(); got != cache {
		t.Errorf("default dir = %q, want LLAMA_CACHE %q", got, cache)
	}
}

func TestLlamaCppModelsDir_ExplicitConfigWins(t *testing.T) {
	isolateLlamaCppEnv(t)
	t.Setenv("LLAMA_CACHE", t.TempDir())
	cfg := DefaultConfig()
	cfg.LlamaCpp.ModelsDir = "/srv/gguf"
	if got := llamaCppModelsDir(cfg); got != "/srv/gguf" {
		t.Errorf("models dir = %q, want the configured /srv/gguf", got)
	}
}

func TestLlamaCppModelsDir_UnsetUsesCacheNotHomeModels(t *testing.T) {
	home := isolateLlamaCppEnv(t)
	cfg := DefaultConfig()
	cfg.LlamaCpp.ModelsDir = ""
	got := llamaCppModelsDir(cfg)
	if got == filepath.Join(home, "Models") {
		t.Errorf("models dir = %q: ~/Models is a llamafile convention, not llama.cpp's", got)
	}
	if want := filepath.Join(home, ".cache", "llama.cpp"); got != want {
		t.Errorf("models dir = %q, want %q", got, want)
	}
}

func TestLlamaCppBackend_ListModels_ReadsLlamaCache(t *testing.T) {
	home := isolateLlamaCppEnv(t)
	writeStubGGUF(t, filepath.Join(home, ".cache", "llama.cpp"), "phi4-Q4_K_M.gguf")
	// A .gguf a person left in ~/Models is not llama.cpp's cache.
	writeStubGGUF(t, filepath.Join(home, "Models"), "stray-Q4_K_M.gguf")

	cfg := DefaultConfig()
	cfg.LlamaCpp.ModelsDir = ""
	b := NewLlamaCppBackend(cfg, t.TempDir())
	models, err := b.ListModels()
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 || models[0].Name != "phi4-Q4_K_M" {
		t.Errorf("models = %+v, want only phi4-Q4_K_M from the llama.cpp cache", models)
	}
}

func TestLlamaCppBackend_ListModels_MissingCacheIsEmpty(t *testing.T) {
	isolateLlamaCppEnv(t) // no ~/.cache/llama.cpp at all
	cfg := DefaultConfig()
	cfg.LlamaCpp.ModelsDir = ""
	models, err := NewLlamaCppBackend(cfg, t.TempDir()).ListModels()
	if err != nil || len(models) != 0 {
		t.Errorf("ListModels = %+v, %v; want no models and no error", models, err)
	}
}

// DefaultConfig must not bake ~/Models into llama_cpp.models_dir, or the
// default above could never take effect.
func TestDefaultConfig_LlamaCppModelsDirUnset(t *testing.T) {
	isolateLlamaCppEnv(t)
	if got := DefaultConfig().LlamaCpp.ModelsDir; got != "" {
		t.Errorf("DefaultConfig().LlamaCpp.ModelsDir = %q, want unset", got)
	}
}
