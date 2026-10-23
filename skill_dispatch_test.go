package harvey

import (
	"testing"
)

// ─── MatchesTrigger ──────────────────────────────────────────────────────────

func TestMatchesTrigger_empty(t *testing.T) {
	skill := &SkillMeta{Trigger: ""}
	if MatchesTrigger(skill, "anything") {
		t.Error("want false when Trigger is empty")
	}
}

func TestMatchesTrigger_keywordHit(t *testing.T) {
	skill := &SkillMeta{Trigger: "pdf extract"}
	if !MatchesTrigger(skill, "please extract the pdf for me") {
		t.Error("want true: prompt contains keyword 'pdf'")
	}
}

func TestMatchesTrigger_keywordMiss(t *testing.T) {
	skill := &SkillMeta{Trigger: "pdf extract"}
	if MatchesTrigger(skill, "summarize the document") {
		t.Error("want false: prompt has no matching keyword")
	}
}

func TestMatchesTrigger_keywordCaseInsensitive(t *testing.T) {
	skill := &SkillMeta{Trigger: "PDF"}
	if !MatchesTrigger(skill, "convert the Pdf file") {
		t.Error("want true: keyword match is case-insensitive")
	}
}

func TestMatchesTrigger_regexpHit(t *testing.T) {
	skill := &SkillMeta{Trigger: `/\bpdf\b/`}
	if !MatchesTrigger(skill, "process a pdf now") {
		t.Error("want true: regexp matches 'pdf'")
	}
}

func TestMatchesTrigger_regexpMiss(t *testing.T) {
	skill := &SkillMeta{Trigger: `/\bpdf\b/`}
	if MatchesTrigger(skill, "there is no match here") {
		t.Error("want false: regexp does not match")
	}
}

func TestMatchesTrigger_malformedRegexp(t *testing.T) {
	skill := &SkillMeta{Trigger: `/[unclosed/`}
	// Must not panic; must return false.
	if MatchesTrigger(skill, "anything") {
		t.Error("want false for malformed regexp")
	}
}

func TestMatchesTrigger_regexpWithFlag(t *testing.T) {
	// /pattern/i — trailing flag after closing slash must still match.
	skill := &SkillMeta{Trigger: `/\bpdf\b/i`}
	if !MatchesTrigger(skill, "process a PDF now") {
		t.Error("want true: regexp with trailing /i flag should match")
	}
}

func TestMatchesTrigger_multiFilePattern(t *testing.T) {
	trigger := `/creat.*\b(demo|component|module|package|project|app|site|page|api)\b|write.*\b(files?|components?|modules?)\b|build.*\b(demo|component|project|app)\b/`
	skill := &SkillMeta{Trigger: trigger}
	cases := []struct {
		prompt string
		want   bool
	}{
		{"Create a web component demo for schema.org Person type", true},
		{"create a new module for the project", true},
		{"build a REST API demo with Go", true},
		{"write the component files for me", true},
		{"what is a web component?", false},
		{"how do I edit this file?", false},
	}
	for _, c := range cases {
		got := MatchesTrigger(skill, c.prompt)
		if got != c.want {
			t.Errorf("MatchesTrigger(%q) = %v, want %v", c.prompt, got, c.want)
		}
	}
}

func TestMatchesTrigger_keywordNoSubstringFalsePositive(t *testing.T) {
	// "at" from "at session start" must NOT fire because the filename
	// Rethinking_Data_Use_in_Large_Language_Models.pdf contains "at" inside
	// the word "data". Word-boundary matching must prevent this.
	skill := &SkillMeta{Trigger: "before making changes to this project | at session start | when asked about project history or past decisions"}
	if MatchesTrigger(skill, "please read Rethinking_Data_Use_in_Large_Language_Models.pdf") {
		t.Error("want false: keyword 'at' must not match as substring inside 'data' in a filename")
	}
}

// ─── SortedSkillNames ────────────────────────────────────────────────────────

func TestSortedSkillNames(t *testing.T) {
	cat := SkillCatalog{
		"zebra": {},
		"alpha": {},
		"beta":  {},
	}
	got := SortedSkillNames(cat)
	want := []string{"alpha", "beta", "zebra"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("SortedSkillNames[%d]: want %q, got %q", i, w, got[i])
		}
	}
}
