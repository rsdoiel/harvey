package harvey

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Step 2 of the Hailo brief (DR-0030): "hailo" is an engine label over the
// Ollama client. It appears in listings, can be selected, is a distinct cache
// key, and a Hailo server found at ollama.url is relabelled.

// ollamaFamilyServer fakes an Ollama-shaped server. With hailo true it also
// answers /hailo/v1/list as hailo-ollama does. It counts every request.
func ollamaFamilyServer(t *testing.T, hailo bool, models ...string) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		switch {
		case r.URL.Path == "/api/tags":
			var parts []string
			for _, m := range models {
				parts = append(parts, fmt.Sprintf(`{"name":%q,"size":1}`, m))
			}
			fmt.Fprintf(w, `{"models":[%s]}`, strings.Join(parts, ","))
		case r.URL.Path == "/hailo/v1/list" && hailo:
			fmt.Fprint(w, hailoListBody)
		case r.URL.Path == "/api/show" && hailo:
			fmt.Fprint(w, strings.ReplaceAll(hailoShow, `"family":"qwen2.5"`, `"family":"llama3.2"`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// familyAgent is an agent whose Ollama and Hailo URLs are set, with a fake
// device node standing in for the card.
func familyAgent(t *testing.T, ollamaURL, hailoURL string, hailoSet, card bool) *Agent {
	t.Helper()
	a := newTestAgent(t)
	a.Config.Ollama.URL = ollamaURL
	a.Config.Hailo = HailoConfig{URL: hailoURL, URLSet: hailoSet}
	a.hailoDevicePath = fakeDevice(t, card)
	return a
}

func TestHailoBackend_IsLabelledHailo(t *testing.T) {
	srv, _ := ollamaFamilyServer(t, true, "llama3.2:3b")
	b := NewHailoBackend(srv.URL, 0, t.TempDir())
	if b.Name() != "hailo" {
		t.Errorf("Name = %q, want hailo", b.Name())
	}
	models, err := b.ListModels()
	if err != nil || len(models) != 1 || models[0].Engine != "hailo" || models[0].Name != "llama3.2:3b" {
		t.Errorf("ListModels = %+v, %v", models, err)
	}
	if b.StartedByHarvey() {
		t.Error("Harvey never starts hailo-ollama")
	}
	if err := b.Start(context.Background(), "m", io.Discard); err == nil {
		t.Error("Start must refuse: Harvey does not start hailo-ollama")
	} else if !strings.Contains(err.Error(), "systemctl --user start hailo-ollama") {
		t.Errorf("Start error should name the command: %v", err)
	}
	if err := b.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestOllamaFamily_NoCardNoURL_HailoIsNeverProbed(t *testing.T) {
	ol, olHits := ollamaFamilyServer(t, false, "llama3")
	hl, hlHits := ollamaFamilyServer(t, true, "llama3.2:3b")
	a := familyAgent(t, ol.URL, hl.URL, false, false)
	f := a.ollamaFamily()
	if f.HailoURL != "" || f.Relabelled || f.OllamaURL != ol.URL {
		t.Errorf("family = %+v, want only Ollama", f)
	}
	if n := atomic.LoadInt32(hlHits) + atomic.LoadInt32(olHits); n != 0 {
		t.Errorf("made %d request(s), want none (a non-Pi machine pays nothing)", n)
	}
}

func TestOllamaFamily_BothServers(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "llama3")
	hl, _ := ollamaFamilyServer(t, true, "llama3.2:3b")
	f := familyAgent(t, ol.URL, hl.URL, true, true).ollamaFamily()
	if f.OllamaURL != ol.URL || f.HailoURL != hl.URL || f.Relabelled {
		t.Errorf("family = %+v", f)
	}
}

// harvey.local today: ollama.url is hand-edited to the Hailo server.
func TestOllamaFamily_HailoServerAtOllamaURLIsRelabelled(t *testing.T) {
	hl, _ := ollamaFamilyServer(t, true, "llama3.2:3b")
	// card present, hailo.url left at the default, which is the same server
	f := familyAgent(t, hl.URL, hl.URL, false, true).ollamaFamily()
	if !f.Relabelled || f.HailoURL != hl.URL || f.OllamaURL != "" {
		t.Errorf("same-URL family = %+v, want relabelled with no Ollama", f)
	}
	// card present, hailo.url default points nowhere, ollama.url is the Hailo server
	f = familyAgent(t, hl.URL, "http://127.0.0.1:1", false, true).ollamaFamily()
	if !f.Relabelled || f.HailoURL != hl.URL || f.OllamaURL != "" {
		t.Errorf("fallback family = %+v, want relabelled with no Ollama", f)
	}
}

func TestAggregateModels_ListsHailoBesideOllama(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "llama3.2:3b", "llama3")
	hl, _ := ollamaFamilyServer(t, true, "llama3.2:3b")
	a := familyAgent(t, ol.URL, hl.URL, true, true)
	models, err := aggregateModels(a)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, m := range models {
		got[m.Engine+"/"+m.Name] = true
	}
	for _, want := range []string{"ollama/llama3.2:3b", "ollama/llama3", "hailo/llama3.2:3b"} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, got)
		}
	}
}

func TestAggregateModels_RelabelledServerIsListedOnlyAsHailo(t *testing.T) {
	hl, _ := ollamaFamilyServer(t, true, "llama3.2:3b")
	a := familyAgent(t, hl.URL, hl.URL, false, true)
	models, _ := aggregateModels(a)
	for _, m := range models {
		if m.Engine == "ollama" {
			t.Errorf("%s listed as ollama; the server is hailo-ollama", m.Name)
		}
	}
	if len(models) != 1 || models[0].Engine != "hailo" {
		t.Errorf("models = %+v, want one hailo entry", models)
	}
}

func TestUseSelectedModel_Hailo(t *testing.T) {
	ol, _ := ollamaFamilyServer(t, false, "llama3.2:3b")
	hl, _ := ollamaFamilyServer(t, true, "llama3.2:3b")
	a := familyAgent(t, ol.URL, hl.URL, true, true)
	mc, _ := OpenModelCache(a.Workspace, "")
	defer mc.Close()
	a.ModelCache = mc

	var out strings.Builder
	if err := useSelectedModel(a, ModelSummary{Name: "llama3.2:3b", Engine: "hailo"}, &out, false); err != nil {
		t.Fatalf("useSelectedModel: %v", err)
	}
	if a.Backend == nil || a.Backend.Name() != "hailo" || a.Backend.BaseURL() != hl.URL {
		t.Fatalf("backend = %v, want hailo at %s", a.Backend, hl.URL)
	}
	ac, ok := a.Client.(*AnyLLMClient)
	if !ok || ac.ModelName() != "llama3.2:3b" {
		t.Fatalf("client = %T", a.Client)
	}
	// the capability row is the Hailo one, not the Ollama one
	if cap, _ := mc.Get("hailo/llama3.2:3b"); cap == nil {
		t.Error("no capability row under hailo/llama3.2:3b")
	}
	if cap, _ := mc.Get("llama3.2:3b"); cap != nil {
		t.Error("the Ollama row was written for a Hailo model")
	}
	if cap, _ := mc.Get("hailo/llama3.2:3b"); cap != nil && cap.MaxPromptTokens != hailoLlama32PromptTokens {
		t.Errorf("MaxPromptTokens = %d, want the seed %d", cap.MaxPromptTokens, hailoLlama32PromptTokens)
	}

	// switching to the Ollama model of the same name is a switch of engines
	out.Reset()
	if err := useSelectedModel(a, ModelSummary{Name: "llama3.2:3b", Engine: "ollama"}, &out, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Switching from hailo") {
		t.Errorf("no engine switch message: %q", out.String())
	}
	if a.Backend.Name() != "ollama" || a.Backend.BaseURL() != ol.URL {
		t.Errorf("backend = %s at %s, want ollama at %s", a.Backend.Name(), a.Backend.BaseURL(), ol.URL)
	}
}

func TestUseSelectedModel_HailoWithNoServerIsUnavailable(t *testing.T) {
	a := familyAgent(t, "http://127.0.0.1:1", "http://127.0.0.1:1", false, false)
	err := useSelectedModel(a, ModelSummary{Name: "llama3.2:3b", Engine: "hailo"}, io.Discard, false)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := ExitCodeFor(err); got != ClassUnavailable {
		t.Errorf("exit class = %v, want ClassUnavailable", got)
	}
}

func TestAttemptModelSwitch_HailoAlias(t *testing.T) {
	hl, _ := ollamaFamilyServer(t, true, "qwen2.5-coder:1.5b")
	a := familyAgent(t, "http://127.0.0.1:1", hl.URL, true, true)
	a.Config.ModelAliases["coder"] = ModelAlias{Model: "qwen2.5-coder:1.5b", Engine: "hailo"}
	ok, err := attemptModelSwitch(a, "coder", io.Discard)
	if !ok || err != nil {
		t.Fatalf("attemptModelSwitch = %v, %v", ok, err)
	}
	if a.Backend == nil || a.Backend.Name() != "hailo" {
		t.Errorf("backend = %v, want hailo", a.Backend)
	}
}

func TestPruneStaleModelRefs_KnowsHailo(t *testing.T) {
	a := newTestAgent(t)
	a.Config.ModelAliases["live"] = ModelAlias{Model: "llama3.2:3b", Engine: "hailo"}
	a.Config.ModelAliases["gone"] = ModelAlias{Model: "missing:1b", Engine: "hailo"}
	n, err := pruneStaleModelRefs(a, nil, nil, nil, []string{"llama3.2:3b"}, io.Discard)
	if err != nil || n != 1 {
		t.Fatalf("pruned %d, %v; want 1", n, err)
	}
	if _, ok := a.Config.ModelAliases["live"]; !ok {
		t.Error("the live hailo alias was removed")
	}
}

// When the Hailo server is found at ollama.url, aliases that said "ollama"
// were really about it; they move to "hailo" and one line says so.
func TestMigrateRelabelledAliases(t *testing.T) {
	a := newTestAgent(t)
	a.Config.ModelAliases["a"] = ModelAlias{Model: "llama3.2:3b", Engine: "ollama"}
	a.Config.ModelAliases["b"] = ModelAlias{Model: "phi4", Engine: "llamafile"}
	a.Config.ModelAliases["c"] = ModelAlias{Model: "x", Engine: ""}
	var out strings.Builder
	n := migrateRelabelledAliases(a, &out)
	if n != 1 || a.Config.ModelAliases["a"].Engine != "hailo" {
		t.Errorf("migrated %d; a = %+v", n, a.Config.ModelAliases["a"])
	}
	if a.Config.ModelAliases["b"].Engine != "llamafile" || a.Config.ModelAliases["c"].Engine != "" {
		t.Error("an alias for another engine was changed")
	}
	if !strings.Contains(out.String(), "hailo") {
		t.Errorf("no message: %q", out.String())
	}
	out.Reset()
	if n := migrateRelabelledAliases(a, &out); n != 0 || out.Len() != 0 {
		t.Errorf("second run migrated %d and said %q; want silence", n, out.String())
	}
}
