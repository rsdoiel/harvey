package harvey

import "testing"

func TestLoadHelpGuide(t *testing.T) {
	for _, name := range []string{"ollama", "pdf-tools", "getting-started"} {
		data, err := LoadHelpGuide(name)
		if err != nil {
			t.Errorf("LoadHelpGuide(%q): %v", name, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("LoadHelpGuide(%q): returned empty content", name)
		}
	}
}

func TestPackageLevelHelpTextVars(t *testing.T) {
	if PDFToolsHelpText == "" {
		t.Error("PDFToolsHelpText is empty; embedded pdf-tools.md failed to load")
	}
	if GettingStartedHelpText == "" {
		t.Error("GettingStartedHelpText is empty; embedded getting-started.md failed to load")
	}
}
