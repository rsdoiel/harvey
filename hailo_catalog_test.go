package harvey

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Step 4 of the Hailo brief (DR-0030): the Hailo catalog (what the card can
// run) is shown beside what is pulled, and a model that is not pulled can be
// pulled after a confirmation. DR-0022 leaves Ollama pulls to the ollama CLI.

const pullSuccessStream = `{"status":"pulling manifest"}
{"status":"pulling abc","digest":"sha256:abc","total":1000,"completed":0}
{"status":"pulling abc","digest":"sha256:abc","total":1000,"completed":500}
{"status":"pulling abc","digest":"sha256:abc","total":1000,"completed":1000}
{"status":"verifying sha256 digest"}
{"status":"success"}
`

// catalogServer fakes hailo-ollama with a pulled set and a wider catalog. A
// successful pull adds the model to the pulled set. It records /api/pull bodies.
type catalogServer struct {
	*httptest.Server
	mu         sync.Mutex
	pulled     []string
	catalog    []string
	pullStream string // body of a /api/pull answer
	pullStatus int
	pulls      []map[string]any
	pullType   string
}

func newCatalogServer(t *testing.T, pulled, catalog []string) *catalogServer {
	t.Helper()
	cs := &catalogServer{pulled: pulled, catalog: catalog, pullStream: pullSuccessStream, pullStatus: 200}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cs.mu.Lock()
		defer cs.mu.Unlock()
		switch r.URL.Path {
		case "/api/tags":
			var parts []string
			for _, m := range cs.pulled {
				parts = append(parts, fmt.Sprintf(`{"name":%q,"size":1}`, m))
			}
			fmt.Fprintf(w, `{"models":[%s]}`, strings.Join(parts, ","))
		case "/hailo/v1/list":
			b, _ := json.Marshal(map[string]any{"models": cs.catalog})
			w.Write(b)
		case "/api/show":
			fmt.Fprint(w, strings.ReplaceAll(hailoShow, `"family":"qwen2.5"`, `"family":"llama3.2"`))
		case "/api/pull":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			cs.pulls = append(cs.pulls, m)
			cs.pullType = r.Header.Get("Content-Type")
			w.WriteHeader(cs.pullStatus)
			io.WriteString(w, cs.pullStream)
			if strings.Contains(cs.pullStream, `"success"`) {
				if name, _ := m["model"].(string); name != "" {
					cs.pulled = append(cs.pulled, name)
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(cs.Close)
	return cs
}

func (cs *catalogServer) pullCount() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return len(cs.pulls)
}

var (
	catPulled  = []string{"llama3.2:3b"}
	catCatalog = []string{"llama3.2:3b", "qwen2.5-coder:1.5b", "qwen2:1.5b"}
)

func catalogAgent(t *testing.T) (*Agent, *catalogServer) {
	t.Helper()
	cs := newCatalogServer(t, append([]string{}, catPulled...), catCatalog)
	return familyAgent(t, "http://127.0.0.1:1", cs.URL, true, true), cs
}

// ─── listing ─────────────────────────────────────────────────────────────────

func TestListFamilyModels_IncludesTheCatalogMarkedNotPulled(t *testing.T) {
	a, _ := catalogAgent(t)
	got := map[string]bool{} // name -> NotPulled
	for _, m := range a.listFamilyModels() {
		if m.Engine != "hailo" {
			t.Errorf("unexpected engine in %+v", m)
		}
		if _, dup := got[m.Name]; dup {
			t.Errorf("%s listed twice", m.Name)
		}
		got[m.Name] = m.NotPulled
	}
	want := map[string]bool{"llama3.2:3b": false, "qwen2.5-coder:1.5b": true, "qwen2:1.5b": true}
	for n, np := range want {
		if v, ok := got[n]; !ok || v != np {
			t.Errorf("%s: listed=%v notPulled=%v, want notPulled=%v", n, ok, v, np)
		}
	}
}

func TestModelList_LabelsNotPulled(t *testing.T) {
	a, _ := catalogAgent(t)
	var out strings.Builder
	if err := cmdModelList(a, &out); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		switch {
		case strings.Contains(line, "qwen2.5-coder:1.5b"), strings.Contains(line, "qwen2:1.5b"):
			if !strings.Contains(line, "not pulled") {
				t.Errorf("catalog entry not labelled: %q", line)
			}
		case strings.Contains(line, "llama3.2:3b"):
			if strings.Contains(line, "not pulled") {
				t.Errorf("pulled model labelled not pulled: %q", line)
			}
		}
	}
}

// ─── /model pull ─────────────────────────────────────────────────────────────

func TestModelPull_ConfirmedPullsAndShowsProgress(t *testing.T) {
	a, cs := catalogAgent(t)
	attended(t, a, "y\n")
	var out strings.Builder
	if err := cmdModel(a, []string{"pull", "qwen2.5-coder:1.5b"}, &out); err != nil {
		t.Fatalf("/model pull: %v\n%s", err, out.String())
	}
	if cs.pullCount() != 1 {
		t.Fatalf("pulls = %d, want 1", cs.pullCount())
	}
	if cs.pulls[0]["model"] != "qwen2.5-coder:1.5b" || cs.pulls[0]["stream"] != true {
		t.Errorf("pull body = %v, want model and stream true (hailo-ollama reads \"model\", not \"name\")", cs.pulls[0])
	}
	if cs.pullType != "application/json" {
		t.Errorf("Content-Type = %q", cs.pullType)
	}
	for _, want := range []string{"[y/N]", "50%", "100%", "Pulled qwen2.5-coder:1.5b"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestModelPull_UnattendedNeedsYes(t *testing.T) {
	a, cs := catalogAgent(t)
	err := cmdModel(a, []string{"pull", "qwen2.5-coder:1.5b"}, io.Discard)
	if ExitCodeFor(err) != ClassNegative {
		t.Fatalf("exit class = %v (%v), want negative", ExitCodeFor(err), err)
	}
	if cs.pullCount() != 0 {
		t.Error("pulled without a confirmation")
	}
	if err := cmdModel(a, []string{"pull", "--yes", "qwen2.5-coder:1.5b"}, io.Discard); err != nil {
		t.Fatalf("with --yes: %v", err)
	}
	if cs.pullCount() != 1 {
		t.Errorf("pulls = %d, want 1", cs.pullCount())
	}
}

func TestModelPull_DeclinedPullsNothing(t *testing.T) {
	a, cs := catalogAgent(t)
	attended(t, a, "n\n")
	err := cmdModel(a, []string{"pull", "qwen2.5-coder:1.5b"}, io.Discard)
	if ExitCodeFor(err) != ClassNegative || cs.pullCount() != 0 {
		t.Errorf("declined: err %v, pulls %d", err, cs.pullCount())
	}
}

func TestModelPull_AlreadyPulledSaysSo(t *testing.T) {
	a, cs := catalogAgent(t)
	var out strings.Builder
	if err := cmdModel(a, []string{"pull", "--yes", "llama3.2:3b"}, &out); err != nil {
		t.Fatal(err)
	}
	if cs.pullCount() != 0 || !strings.Contains(out.String(), "already pulled") {
		t.Errorf("pulls %d, output %q", cs.pullCount(), out.String())
	}
}

func TestModelPull_NotInTheCatalog(t *testing.T) {
	a, cs := catalogAgent(t)
	err := cmdModel(a, []string{"pull", "--yes", "nosuch:1b"}, io.Discard)
	if ExitCodeFor(err) != ClassNegative || cs.pullCount() != 0 {
		t.Errorf("err %v (class %v), pulls %d", err, ExitCodeFor(err), cs.pullCount())
	}
	if !strings.Contains(err.Error(), "catalog") {
		t.Errorf("error should mention the catalog: %v", err)
	}
}

// DR-0022: Ollama models are pulled with the ollama CLI.
func TestModelPull_OllamaIsLeftToTheCLI(t *testing.T) {
	a, cs := catalogAgent(t)
	err := cmdModel(a, []string{"pull", "ollama/llama3"}, io.Discard)
	if ExitCodeFor(err) != ClassUsage {
		t.Fatalf("exit class = %v (%v), want usage", ExitCodeFor(err), err)
	}
	if !strings.Contains(err.Error(), "ollama pull llama3") || cs.pullCount() != 0 {
		t.Errorf("error %q; pulls %d", err, cs.pullCount())
	}
}

func TestModelPull_NoNameIsUsage(t *testing.T) {
	a, _ := catalogAgent(t)
	if err := cmdModel(a, []string{"pull"}, io.Discard); ExitCodeFor(err) != ClassUsage {
		t.Errorf("exit class = %v (%v), want usage", ExitCodeFor(err), err)
	}
}

func TestModelPull_QualifiedHailoName(t *testing.T) {
	a, cs := catalogAgent(t)
	if err := cmdModel(a, []string{"pull", "--yes", "hailo/qwen2:1.5b"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if cs.pullCount() != 1 || cs.pulls[0]["model"] != "qwen2:1.5b" {
		t.Errorf("pulls = %v", cs.pulls)
	}
}

// hailo-ollama answers an unknown model with HTTP 200 and {"error": ...}.
func TestModelPull_ErrorLineInA200Stream(t *testing.T) {
	a, cs := catalogAgent(t)
	cs.pullStream = `{"error":"model 'qwen2:1.5b' not found"}`
	err := cmdModel(a, []string{"pull", "--yes", "qwen2:1.5b"}, io.Discard)
	if ExitCodeFor(err) != ClassNegative || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err %v (class %v)", err, ExitCodeFor(err))
	}
}

func TestModelPull_StreamEndingBeforeSuccessIsAnIOError(t *testing.T) {
	a, cs := catalogAgent(t)
	cs.pullStream = `{"status":"pulling manifest"}` + "\n" + `{"status":"pulling abc","total":1000,"completed":300}` + "\n"
	err := cmdModel(a, []string{"pull", "--yes", "qwen2:1.5b"}, io.Discard)
	if ExitCodeFor(err) != ClassIO {
		t.Errorf("exit class = %v (%v), want io", ExitCodeFor(err), err)
	}
}

func TestModelPull_ServerErrorIsUnavailable(t *testing.T) {
	a, cs := catalogAgent(t)
	cs.pullStatus, cs.pullStream = 500, "boom"
	err := cmdModel(a, []string{"pull", "--yes", "qwen2:1.5b"}, io.Discard)
	if ExitCodeFor(err) != ClassUnavailable {
		t.Errorf("exit class = %v (%v), want unavailable", ExitCodeFor(err), err)
	}
}

func TestModelPull_NoHailoServerIsUnavailable(t *testing.T) {
	a := familyAgent(t, "http://127.0.0.1:1", "http://127.0.0.1:1", false, false)
	err := cmdModel(a, []string{"pull", "--yes", "qwen2:1.5b"}, io.Discard)
	if ExitCodeFor(err) != ClassUnavailable {
		t.Errorf("exit class = %v (%v), want unavailable", ExitCodeFor(err), err)
	}
}

// ─── choosing a model that is not pulled ─────────────────────────────────────

func TestModelUse_NotPulledOffersThePullThenUsesIt(t *testing.T) {
	a, cs := catalogAgent(t)
	attended(t, a, "y\n")
	var out strings.Builder
	if err := cmdModel(a, []string{"use", "hailo/qwen2.5-coder:1.5b"}, &out); err != nil {
		t.Fatalf("/model use: %v\n%s", err, out.String())
	}
	if cs.pullCount() != 1 {
		t.Errorf("pulls = %d, want 1", cs.pullCount())
	}
	if a.Backend == nil || a.Backend.Name() != "hailo" || a.Backend.ActiveModel() != "qwen2.5-coder:1.5b" {
		t.Errorf("backend = %v, want hailo qwen2.5-coder:1.5b", a.Backend)
	}
}

func TestModelUse_NotPulledUnattendedDoesNotPull(t *testing.T) {
	a, cs := catalogAgent(t)
	err := cmdModel(a, []string{"use", "hailo/qwen2.5-coder:1.5b"}, io.Discard)
	if ExitCodeFor(err) != ClassNegative || !strings.Contains(err.Error(), "/model pull") {
		t.Errorf("err %v (class %v); want negative naming /model pull", err, ExitCodeFor(err))
	}
	if cs.pullCount() != 0 || a.Backend != nil {
		t.Errorf("pulls %d, backend %v", cs.pullCount(), a.Backend)
	}
}

func TestModelUse_NotPulledDeclinedUsesNothing(t *testing.T) {
	a, cs := catalogAgent(t)
	attended(t, a, "n\n")
	err := cmdModel(a, []string{"use", "hailo/qwen2.5-coder:1.5b"}, io.Discard)
	if err == nil || cs.pullCount() != 0 || a.Backend != nil {
		t.Errorf("err %v, pulls %d, backend %v", err, cs.pullCount(), a.Backend)
	}
}

// ─── the start-up picker ─────────────────────────────────────────────────────

func TestPickOllamaModel_ListsTheCatalogAndPullsTheChoice(t *testing.T) {
	a, cs := catalogAgent(t)
	attended(t, a, "y\n") // the answer to the pull confirmation
	var out strings.Builder
	// entries: 1 llama3.2:3b, 2 qwen2.5-coder:1.5b (not pulled), 3 qwen2:1.5b (not pulled)
	if err := a.pickOllamaModel(newTestBufioReader("2\n"), &out, ""); err != nil {
		t.Fatalf("pickOllamaModel: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "not pulled") {
		t.Errorf("picker does not mark unpulled models:\n%s", out.String())
	}
	if cs.pullCount() != 1 || a.Backend == nil || a.Backend.ActiveModel() != "qwen2.5-coder:1.5b" {
		t.Errorf("pulls %d, backend %v", cs.pullCount(), a.Backend)
	}
}

// A session's model that is only in the catalog is never pulled on its own.
func TestPickOllamaModel_PreferredModelNotPulledIsNotAutoSelected(t *testing.T) {
	a, cs := catalogAgent(t)
	var out strings.Builder
	_ = a.pickOllamaModel(newTestBufioReader("1\n"), &out, "qwen2.5-coder:1.5b")
	if cs.pullCount() != 0 {
		t.Error("a pull started without being chosen")
	}
	if a.Backend == nil || a.Backend.ActiveModel() != "llama3.2:3b" {
		t.Errorf("backend = %v, want the user's pick (the pulled model)", a.Backend)
	}
}
