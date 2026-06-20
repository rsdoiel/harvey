package harvey

import (
	"strings"
	"testing"
)

// TestPrintHelpTopic_modelRoutesToModelHelpText verifies that the "model"
// topic dispatches to ModelHelpText (the unified /model command guide), not
// to ModelAliasHelpText.
func TestPrintHelpTopic_modelRoutesToModelHelpText(t *testing.T) {
	var buf strings.Builder
	ok := PrintHelpTopic(&buf, "model", "harvey", "0.0.0", "2026-01-01", "abc1234")
	if !ok {
		t.Fatal("PrintHelpTopic returned false for topic \"model\"")
	}
	out := buf.String()
	// ModelHelpText covers /model list|use|show|status — this phrase is unique to it.
	if !strings.Contains(out, "backend-agnostic") {
		t.Errorf("\"model\" topic should dispatch to ModelHelpText; got: %.200s", out)
	}
	// Must NOT contain the NAME section header from ModelAliasHelpText.
	// (ModelHelpText has "# MODEL ALIASES" as a subsection, so check the exact NAME line.)
	if strings.Contains(out, "MODEL ALIAS —") {
		t.Errorf("\"model\" topic should not dispatch to ModelAliasHelpText; got MODEL ALIAS name header")
	}
}

// TestPrintHelpTopic_modelAliasRoutesToModelAliasHelpText verifies that the
// "model-alias" topic dispatches to ModelAliasHelpText.
func TestPrintHelpTopic_modelAliasRoutesToModelAliasHelpText(t *testing.T) {
	var buf strings.Builder
	ok := PrintHelpTopic(&buf, "model-alias", "harvey", "0.0.0", "2026-01-01", "abc1234")
	if !ok {
		t.Fatal("PrintHelpTopic returned false for topic \"model-alias\"")
	}
	out := buf.String()
	if !strings.Contains(out, "MODEL ALIAS") {
		t.Errorf("\"model-alias\" topic should dispatch to ModelAliasHelpText; got: %.200s", out)
	}
}

// TestPrintHelpTopic_modelAndModelAliasAreDifferent ensures the two topics
// produce different output.
func TestPrintHelpTopic_modelAndModelAliasAreDifferent(t *testing.T) {
	var m, ma strings.Builder
	PrintHelpTopic(&m, "model", "harvey", "0.0.0", "2026-01-01", "abc1234")
	PrintHelpTopic(&ma, "model-alias", "harvey", "0.0.0", "2026-01-01", "abc1234")
	if m.String() == ma.String() {
		t.Error("\"model\" and \"model-alias\" topics produce identical output — they should be distinct")
	}
}

// TestHelpTopicsText_includesModelTopics verifies that the topics index lists
// both "model" and "model-alias" so users can discover them.
func TestHelpTopicsText_includesModelTopics(t *testing.T) {
	idx := HelpTopicsText()
	if !strings.Contains(idx, "model") {
		t.Error("HelpTopicsText should mention \"model\"")
	}
	if !strings.Contains(idx, "model-alias") {
		t.Error("HelpTopicsText should mention \"model-alias\"")
	}
}
