package harvey

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Layer 1 of the prompt budget: a per-model limit on the whole prompt (system
// prompt + history + new message), kept in the model cache next to ToolMode.
// hailo-ollama's llama3.2:3b answers HTTP 500 "read failed" once the prompt
// passes about 3,000 characters, far below any context window it advertises
// (kb observation 377). Limits are in the units of estimateTokens (chars/4).

// hailoShowFor serves /api/show as hailo-ollama answers it, for family fam.
func hailoShowFor(t *testing.T, fam string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, strings.ReplaceAll(hailoShow, `"family":"qwen2.5"`, `"family":"`+fam+`"`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestModelCache_MaxPromptTokensRoundTrips(t *testing.T) {
	a, mc := newTestAgentWithCache(t, "")
	_ = a
	if err := mc.Set(&ModelCapability{Name: "m", MaxPromptTokens: 700, ProbeLevel: "fast", ProbedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := mc.Get("m")
	if err != nil || got == nil || got.MaxPromptTokens != 700 {
		t.Fatalf("Get: %+v, %v", got, err)
	}
	all, err := mc.All()
	if err != nil || len(all) != 1 || all[0].MaxPromptTokens != 700 {
		t.Fatalf("All: %+v, %v", all, err)
	}
}

func TestFastProbeModel_SeedsPromptLimitForHailoLlama32(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cap, err := FastProbeModel(ctx, hailoShowFor(t, "llama3.2"), "llama3.2:3b")
	if err != nil {
		t.Fatal(err)
	}
	if cap.MaxPromptTokens != hailoLlama32PromptTokens {
		t.Errorf("MaxPromptTokens = %d, want %d", cap.MaxPromptTokens, hailoLlama32PromptTokens)
	}
}

// qwen2.5-coder:1.5b on the same server took harvey's whole 6,249-character
// prompt; nothing is known to cap it, so the probe leaves it unknown (0).
func TestFastProbeModel_LeavesOtherHailoModelsUnlimited(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cap, err := FastProbeModel(ctx, hailoShowFor(t, "qwen2.5"), "qwen2.5-coder:1.5b")
	if err != nil {
		t.Fatal(err)
	}
	if cap.MaxPromptTokens != 0 {
		t.Errorf("MaxPromptTokens = %d, want 0", cap.MaxPromptTokens)
	}
}

// A limit the user set, or one learned from a failure, must survive the
// re-probe that every model selection runs; the seed only fills an empty one.
func TestProbe_KeepsUserSetPromptLimitOverSeed(t *testing.T) {
	for _, c := range []struct {
		name     string
		existing int
		want     int
	}{
		{"user value wins", 500, 500},
		{"empty takes the seed", 0, hailoLlama32PromptTokens},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, mc := newTestAgentWithCache(t, "")
			a.Config.Ollama.URL = hailoShowFor(t, "llama3.2")
			if err := mc.Set(&ModelCapability{Name: "llama3.2:3b", MaxPromptTokens: c.existing, ProbeLevel: "fast", ProbedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			a.probeOllamaModelAndCache("llama3.2:3b")
			got, _ := mc.Get("llama3.2:3b")
			if got == nil || got.MaxPromptTokens != c.want {
				t.Errorf("MaxPromptTokens = %+v, want %d", got, c.want)
			}
		})
	}
}

func TestCmdModelLimit(t *testing.T) {
	a, mc := newTestAgentWithCache(t, "phi4:latest")
	var out strings.Builder

	if err := cmdModel(a, []string{"limit", "900"}, &out); err != nil {
		t.Fatalf("limit 900: %v", err)
	}
	if got, _ := mc.Get("phi4:latest"); got == nil || got.MaxPromptTokens != 900 {
		t.Fatalf("after limit 900: %+v", got)
	}
	out.Reset()
	if err := cmdModel(a, []string{"limit"}, &out); err != nil || !strings.Contains(out.String(), "900") {
		t.Errorf("show: err=%v out=%q", err, out.String())
	}
	if err := cmdModel(a, []string{"limit", "other:1b", "400"}, &out); err != nil {
		t.Fatalf("named: %v", err)
	}
	if got, _ := mc.Get("other:1b"); got == nil || got.MaxPromptTokens != 400 {
		t.Errorf("named model: %+v", got)
	}
	if err := cmdModel(a, []string{"limit", "auto"}, &out); err != nil {
		t.Fatalf("auto: %v", err)
	}
	if got, _ := mc.Get("phi4:latest"); got.MaxPromptTokens != 0 {
		t.Errorf("after auto: %d", got.MaxPromptTokens)
	}
}

func TestCmdModelLimit_BadValuesAreUsageErrors(t *testing.T) {
	a, _ := newTestAgentWithCache(t, "phi4:latest")
	for _, v := range []string{"abc", "-5", "0x10", ""} {
		args := []string{"limit", v}
		if v == "" {
			args = []string{"limit", "a", "b", "c"}
		}
		err := cmdModel(a, args, &strings.Builder{})
		if got := ExitCodeFor(err); got != ClassUsage {
			t.Errorf("limit %q: class %v (err %v), want usage", v, got, err)
		}
	}
}

// promptLimitAgent is an agent whose active model has a cached prompt limit.
func promptLimitAgent(t *testing.T, limit int) *Agent {
	t.Helper()
	a, mc := newTestAgentWithCache(t, "llama3.2:3b")
	if err := mc.Set(&ModelCapability{Name: "llama3.2:3b", MaxPromptTokens: limit, ProbeLevel: "fast", ProbedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPromptTokenLimit(t *testing.T) {
	if got := promptLimitAgent(t, 700).promptTokenLimit(); got != 700 {
		t.Errorf("promptTokenLimit = %d, want 700", got)
	}
	if got := promptLimitAgent(t, 0).promptTokenLimit(); got != 0 {
		t.Errorf("unknown limit = %d, want 0", got)
	}
}

func TestPromptBudgetError(t *testing.T) {
	a := promptLimitAgent(t, 700)
	a.AddMessage("system", strings.Repeat("s", 2000))
	a.AddMessage("user", "short question")
	if err := a.promptBudgetError(); err != nil {
		t.Errorf("under the limit: %v", err)
	}
	a.AddMessage("user", strings.Repeat("u", 1500))
	err := a.promptBudgetError()
	if err == nil {
		t.Fatal("over the limit: no error")
	}
	for _, want := range []string{"llama3.2:3b", "700", "/model limit"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if got := ExitCodeFor(err); got != ClassData {
		t.Errorf("class %v, want data", got)
	}
}

func TestPromptBudgetError_NoLimitNoError(t *testing.T) {
	a := promptLimitAgent(t, 0)
	a.AddMessage("system", strings.Repeat("s", 100000))
	if err := a.promptBudgetError(); err != nil {
		t.Errorf("unknown limit: %v", err)
	}
}

// A turn that would overflow the model's prompt limit is refused before any
// request is sent, and the refused message does not stay in the history.
func TestRunChatTurn_RefusesPromptOverLimitWithoutSending(t *testing.T) {
	var chats int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			chats++
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	a := promptLimitAgent(t, 700)
	a.Client = newOllamaLLMClient(srv.URL, "llama3.2:3b", 0)
	a.AddMessage("system", strings.Repeat("s", 2000))
	before := len(a.History)

	_, _, err := a.runChatTurn(context.Background(), strings.Repeat("u", 1500), &strings.Builder{}, nil, false, "")
	if err == nil {
		t.Fatal("expected the turn to be refused")
	}
	if got := ExitCodeFor(err); got != ClassData {
		t.Errorf("class %v, want data (err %v)", got, err)
	}
	if chats != 0 {
		t.Errorf("%d /api/chat requests were sent", chats)
	}
	if len(a.History) != before {
		t.Errorf("history has %d messages, want %d", len(a.History), before)
	}
}

func TestSystemPromptBudgetCheck(t *testing.T) {
	a := promptLimitAgent(t, 700)
	a.AddMessage("system", strings.Repeat("s", 6249)) // harvey's own prompt on harvey.local

	// At a terminal: a warning, so /model use or /model limit can still be typed.
	var out strings.Builder
	if err := a.checkSystemPromptBudget(&out, true); err != nil {
		t.Errorf("interactive: %v", err)
	}
	if !strings.Contains(out.String(), "/model limit") {
		t.Errorf("interactive: no warning, got %q", out.String())
	}
	// The system prompt is the problem, so "shorten the message" is wrong advice.
	if !strings.Contains(out.String(), "system prompt") || strings.Contains(out.String(), "shorten the message") {
		t.Errorf("interactive: wrong advice for an oversize system prompt: %q", out.String())
	}

	// Unattended: nobody can fix it, so it fails as bad content (65).
	err := a.checkSystemPromptBudget(&strings.Builder{}, false)
	if got := ExitCodeFor(err); got != ClassData {
		t.Errorf("non-interactive: class %v (err %v), want data", got, err)
	}

	// Fits: silent.
	b := promptLimitAgent(t, 700)
	b.AddMessage("system", strings.Repeat("s", 1000))
	out.Reset()
	if err := b.checkSystemPromptBudget(&out, false); err != nil || out.Len() != 0 {
		t.Errorf("fits: err=%v out=%q", err, out.String())
	}
}
