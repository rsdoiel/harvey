package harvey

import (
	"bytes"
	"strings"
	"testing"
)

// `/model use NAME` only searched the llamafile registry and the aliases, so a model
// that the picker lists (an Ollama model, an unregistered .gguf) answered "not found"
// when named. It now falls back to the picker's full list of local models.

func TestMatchModel(t *testing.T) {
	models := []ModelSummary{
		{Name: "llama3.2:3b", Engine: "ollama"},
		{Name: "llama3.2:1b", Engine: "ollama"},
		{Name: "Qwen3-4B-Q5_K_S", Engine: "llamacpp", Path: "/m/Qwen3-4B-Q5_K_S.gguf"},
		{Name: "Apertus-8B", Engine: "llamafile", Path: "/m/Apertus-8B.llamafile"},
		{Name: "shared", Engine: "ollama"},
		{Name: "shared", Engine: "llamacpp", Path: "/m/shared.gguf"},
	}
	for _, tc := range []struct {
		name    string
		query   string
		want    string // the chosen model's name, "" when none
		wantEng string
		ambig   int // expected number of ambiguous candidates
	}{
		{"exact", "llama3.2:3b", "llama3.2:3b", "ollama", 0},
		{"exact ignoring case", "LLAMA3.2:3B", "llama3.2:3b", "ollama", 0},
		{"a unique prefix", "apert", "Apertus-8B", "llamafile", 0},
		{"a unique prefix ignoring case", "qwen3", "Qwen3-4B-Q5_K_S", "llamacpp", 0},
		{"an ambiguous prefix", "llama3.2", "", "", 2},
		{"the same name on two engines", "shared", "", "", 2},
		{"an exact name beats a longer name it prefixes", "llama3.2:1b", "llama3.2:1b", "ollama", 0},
		{"not found", "nosuchmodel", "", "", 0},
		{"empty", "", "", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ambiguous, ok := matchModel(models, tc.query)
			if tc.want != "" {
				if !ok || got.Name != tc.want || got.Engine != tc.wantEng {
					t.Errorf("matchModel(%q) = %+v, %v; want %s on %s", tc.query, got, ok, tc.want, tc.wantEng)
				}
				return
			}
			if ok {
				t.Errorf("matchModel(%q) chose %+v, want no choice", tc.query, got)
			}
			if len(ambiguous) != tc.ambig {
				t.Errorf("matchModel(%q) ambiguous = %d, want %d", tc.query, len(ambiguous), tc.ambig)
			}
		})
	}
}

func withLocalModels(t *testing.T, models []ModelSummary) {
	t.Helper()
	old := listLocalModels
	listLocalModels = func(*Agent) ([]ModelSummary, error) { return models, nil }
	t.Cleanup(func() { listLocalModels = old })
}

func TestModelUse_NamesAnOllamaModelThePickerWouldList(t *testing.T) {
	a := newTestAgent(t)
	withLocalModels(t, []ModelSummary{{Name: "llama3.2:3b", Engine: "ollama"}})
	var out bytes.Buffer
	if err := cmdModel(a, []string{"use", "LLAMA3.2:3b"}, &out); err != nil {
		t.Fatalf("/model use: %v", err)
	}
	if a.Config.Ollama.Model != "llama3.2:3b" {
		t.Errorf("Ollama model = %q, want llama3.2:3b; output %q", a.Config.Ollama.Model, out.String())
	}
	if !strings.Contains(out.String(), "Using model") || strings.Contains(out.String(), "not found") {
		t.Errorf("output %q should confirm the switch", out.String())
	}
}

func TestModelUse_UniquePrefixSwitches(t *testing.T) {
	a := newTestAgent(t)
	withLocalModels(t, []ModelSummary{{Name: "llama3.2:3b", Engine: "ollama"}, {Name: "mistral:7b", Engine: "ollama"}})
	var out bytes.Buffer
	if err := cmdModel(a, []string{"use", "mist"}, &out); err != nil {
		t.Fatal(err)
	}
	if a.Config.Ollama.Model != "mistral:7b" {
		t.Errorf("Ollama model = %q, want mistral:7b", a.Config.Ollama.Model)
	}
}

func TestModelUse_AmbiguousNameListsTheMatchesAndSwitchesNothing(t *testing.T) {
	a := newTestAgent(t)
	before := a.Config.Ollama.Model
	withLocalModels(t, []ModelSummary{{Name: "llama3.2:3b", Engine: "ollama"}, {Name: "llama3.2:1b", Engine: "ollama"}})
	var out bytes.Buffer
	err := cmdModel(a, []string{"use", "llama3"}, &out)
	if ExitCodeFor(err) != ClassUsage {
		t.Fatalf("an ambiguous name should be a usage error, got: %v", err)
	}
	if a.Config.Ollama.Model != before {
		t.Errorf("an ambiguous name switched the model to %q", a.Config.Ollama.Model)
	}
	for _, want := range []string{"llama3.2:3b", "llama3.2:1b", "several"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should list the matches and say several matched (%q)", err, want)
		}
	}
}

func TestModelUse_UnknownNameStillSaysNotFound(t *testing.T) {
	a := newTestAgent(t)
	withLocalModels(t, []ModelSummary{{Name: "llama3.2:3b", Engine: "ollama"}})
	var out bytes.Buffer
	err := cmdModel(a, []string{"use", "nosuch"}, &out)
	if ExitCodeFor(err) != ClassNegative {
		t.Fatalf("an unknown name should be a negative result, got: %v", err)
	}
	if !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "/model list") {
		t.Errorf("error %q should say not found and point at /model list", err)
	}
}

// The registry and aliases keep priority: an alias that names a model still works
// as before, without consulting the disk scan.
func TestModelUse_AliasStillWins(t *testing.T) {
	a := newTestAgent(t)
	a.Config.ModelAliases = map[string]ModelAlias{"fast": {Model: "qwen2.5:0.5b", Engine: "ollama"}}
	called := false
	old := listLocalModels
	listLocalModels = func(*Agent) ([]ModelSummary, error) { called = true; return nil, nil }
	t.Cleanup(func() { listLocalModels = old })
	var out bytes.Buffer
	if err := cmdModel(a, []string{"use", "fast"}, &out); err != nil {
		t.Fatal(err)
	}
	if a.Config.Ollama.Model != "qwen2.5:0.5b" || called {
		t.Errorf("model %q, listLocalModels called=%v; want the alias used and no scan", a.Config.Ollama.Model, called)
	}
}
