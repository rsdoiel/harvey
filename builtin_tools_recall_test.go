package harvey

import (
	"os/exec"
	"strings"
	"testing"
)

// kbForTool builds a knowledge database at the tool agent's default path with
// one concept, one observation citing a retracted source, and returns the agent
// registry. It skips when kb is not installed.
func kbForTool(t *testing.T) (*Agent, *ToolRegistry) {
	t.Helper()
	if _, err := exec.LookPath("kb"); err != nil {
		t.Skip("kb not installed")
	}
	a, reg := newToolAgent(t, nil)
	db := a.knowledgeDBPath()
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("kb", append([]string{"-db", db}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("kb %v: %v\n%s", args, err, out)
		}
	}
	run("project", "add", "demo", "demo")
	run("source", "add", "Bad paper", "--doi", "10.1/bad")
	run("observation", "add", "--project", "demo", "--source-doi", "10.1/bad", "finding", "zebra stripes repel flies")
	run("source", "retract", "1", "Retracted for data fabrication")
	run("concept", "add", "zebra", "striped horse")
	run("link", "observation", "1", "zebra")
	return a, reg
}

func TestRecallKnowledge_ReturnsRowWithRefAndRetraction(t *testing.T) {
	_, reg := kbForTool(t)
	got, err := dispatch(t, reg, "recall_knowledge", map[string]any{"query": "what about zebra?"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[kb:observation:1]", "zebra stripes repel flies", "RETRACTED: Retracted for data fabrication"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRecallKnowledge_NoMatch(t *testing.T) {
	_, reg := kbForTool(t)
	got, err := dispatch(t, reg, "recall_knowledge", map[string]any{"query": "qqqq"})
	if err != nil || !strings.Contains(got, "nothing in the knowledge base matched") {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestRecallKnowledge_EmptyQuery(t *testing.T) {
	_, reg := newToolAgent(t, nil)
	if _, err := dispatch(t, reg, "recall_knowledge", map[string]any{"query": "  "}); err == nil {
		t.Fatal("expected an error for an empty query")
	}
}

func TestRecallKnowledge_KBNotInstalled(t *testing.T) {
	_, reg := newToolAgent(t, nil)
	t.Setenv("PATH", t.TempDir())
	got, err := dispatch(t, reg, "recall_knowledge", map[string]any{"query": "zebra"})
	if err != nil || !strings.Contains(got, "kb is not installed") {
		t.Errorf("got %q, %v", got, err)
	}
}

// DR-0037 item 5: a model never writes a memory, and the old read tool is gone.
func TestMemoryToolsAreGone(t *testing.T) {
	_, reg := newToolAgent(t, nil)
	for _, name := range []string{"add_memory", "update_memory", "delete_memory", "retrieve_memory"} {
		if _, err := dispatch(t, reg, name, map[string]any{}); err == nil || !strings.Contains(err.Error(), "unknown") {
			t.Errorf("%s should be an unknown tool, got %v", name, err)
		}
	}
}
