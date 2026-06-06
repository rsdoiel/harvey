package harvey

import (
	"strings"
	"testing"
)

// TestCheckPopplerTools_MissingShowsHelpPointer verifies that when poppler
// utilities are absent, the error message includes the /help pdf-tools pointer.
// This test only runs when pdftotext is not installed on the host.
func TestCheckPopplerTools_MissingShowsHelpPointer(t *testing.T) {
	err := checkPopplerTools()
	if err == nil {
		t.Skip("poppler tools are installed on this system — skipping missing-tools test")
	}
	if !strings.Contains(err.Error(), "/help pdf-tools") {
		t.Errorf("error message should contain /help pdf-tools pointer, got: %q", err.Error())
	}
}

// TestCheckPopplerTools_ErrorContainsInstallInstructions verifies that when
// poppler is missing, the error includes platform-specific install commands
// alongside the help pointer.
func TestCheckPopplerTools_ErrorContainsInstallInstructions(t *testing.T) {
	err := checkPopplerTools()
	if err == nil {
		t.Skip("poppler tools are installed on this system — skipping")
	}
	msg := err.Error()
	if !strings.Contains(msg, "brew install") && !strings.Contains(msg, "apt install") {
		t.Error("error should include install instructions")
	}
	if !strings.Contains(msg, "/help pdf-tools") {
		t.Error("error should include /help pdf-tools pointer")
	}
}

// TestHelpGettingStartedLoads verifies that GettingStartedHelpText is non-empty
// (loaded from the embedded guide at init time) and mentions Ollama.
func TestHelpGettingStartedLoads(t *testing.T) {
	if GettingStartedHelpText == "" {
		t.Fatal("GettingStartedHelpText should not be empty")
	}
	if !strings.Contains(GettingStartedHelpText, "Ollama") {
		t.Error("getting-started guide should mention Ollama")
	}
	if !strings.Contains(GettingStartedHelpText, "ollama pull") {
		t.Error("getting-started guide should include an ollama pull example")
	}
}

// TestHelpPDFToolsLoads verifies that PDFToolsHelpText is non-empty and
// mentions the /help pdf-tools pointer topic itself.
func TestHelpPDFToolsLoads(t *testing.T) {
	if PDFToolsHelpText == "" {
		t.Fatal("PDFToolsHelpText should not be empty")
	}
	if !strings.Contains(PDFToolsHelpText, "pdftotext") {
		t.Error("pdf-tools guide should mention pdftotext")
	}
	if !strings.Contains(PDFToolsHelpText, "brew install") {
		t.Error("pdf-tools guide should include macOS install instruction")
	}
}

// TestHelpDispatch_GettingStarted verifies that cmdHelp dispatches
// "getting-started" to the embedded guide without error.
func TestHelpDispatch_GettingStarted(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	var out strings.Builder
	if err := cmdHelp(a, []string{"getting-started"}, &out); err != nil {
		t.Fatalf("cmdHelp getting-started: %v", err)
	}
	if out.Len() == 0 {
		t.Error("cmdHelp getting-started produced no output")
	}
}

// TestHelpDispatch_PDFTools verifies that cmdHelp dispatches "pdf-tools"
// to the embedded guide without error.
func TestHelpDispatch_PDFTools(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	var out strings.Builder
	if err := cmdHelp(a, []string{"pdf-tools"}, &out); err != nil {
		t.Fatalf("cmdHelp pdf-tools: %v", err)
	}
	if out.Len() == 0 {
		t.Error("cmdHelp pdf-tools produced no output")
	}
}

// TestHelpDispatch_Install verifies the "install" alias for getting-started.
func TestHelpDispatch_Install(t *testing.T) {
	a := newTestAgent(t)
	a.registerCommands()
	var out strings.Builder
	if err := cmdHelp(a, []string{"install"}, &out); err != nil {
		t.Fatalf("cmdHelp install: %v", err)
	}
	if out.Len() == 0 {
		t.Error("cmdHelp install produced no output")
	}
}
