package harvey

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func readKBFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "kb", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The contract DR-0037 item 4 names first: a retracted source's note reaches
// the JSON, and the text harvey injects carries it.
func TestKBRecallContract_RetractionNoteReachesThePrompt(t *testing.T) {
	res, err := ParseKBRecall(readKBFixture(t, "concept_recall_retracted.json"))
	if err != nil {
		t.Fatal(err)
	}
	var h *KBRecallHit
	for i := range res.Hits {
		if len(res.Hits[i].Sources) > 0 {
			h = &res.Hits[i]
		}
	}
	if h == nil {
		t.Fatal("fixture has no hit with a cited source")
	}
	if !h.Retracted || !h.Sources[0].Retracted || h.Sources[0].RetractionNote != "Retracted for data fabrication" {
		t.Fatalf("retraction not decoded: %+v", h)
	}
	got := FormatKBRecallHit(*h)
	for _, want := range []string{"Cited:", "Bad paper (doi: 10.1/bad)", "RETRACTED: Retracted for data fabrication"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestKBRecallContract_KindsAndMaterial(t *testing.T) {
	res, err := ParseKBRecall(readKBFixture(t, "concept_recall_kinds.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) == 0 || len(res.Matched) == 0 {
		t.Fatalf("expected hits and matched concepts: %+v", res)
	}
	seen := map[string]bool{}
	for _, h := range res.Hits {
		seen[h.Kind] = true
		if h.Kind == "record" && (h.Material == nil || h.Material.Path == "") {
			t.Errorf("record hit has no material path: %+v", h)
		}
		if h.Kind == "document_gist" && FormatKBRecallHit(h) != "" {
			t.Errorf("an unreviewed document row (empty excerpt) must not be injected: %+v", h)
		}
	}
	if !seen["record"] || !seen["document_gist"] {
		t.Errorf("fixture should hold a record and a document_gist, got %v", seen)
	}
	if got := res.Hits[0].Ref(); !strings.HasPrefix(got, "kb:") {
		t.Errorf("Ref = %q", got)
	}
}

func TestParseKBRecall_NothingMatchedIsEmptyNotAnError(t *testing.T) {
	res, err := ParseKBRecall(readKBFixture(t, "concept_recall_none.json"))
	if err != nil || len(res.Hits) != 0 {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestParseKBRecall_OtherErrorsAndGarbage(t *testing.T) {
	if _, err := ParseKBRecall([]byte(`{"error":"database is locked","class":"temp_fail","code":75}`)); err == nil ||
		!strings.Contains(err.Error(), "database is locked") {
		t.Errorf("temp_fail should be an error carrying kb's message, got %v", err)
	}
	for _, bad := range []string{"", "not json", "[1,2]"} {
		if _, err := ParseKBRecall([]byte(bad)); err == nil {
			t.Errorf("%q should be an error", bad)
		}
	}
}

func TestFormatKBRecallHit_RetractedWithoutListedSources(t *testing.T) {
	got := FormatKBRecallHit(KBRecallHit{Kind: "observation", ID: 1, Excerpt: "x", Retracted: true})
	if !strings.Contains(got, "RETRACTED") {
		t.Errorf("got %q", got)
	}
}

// Against the installed kb, so a change in kb's JSON shows up here. Skipped
// when kb is absent.
func TestKBRecallContract_InstalledKB(t *testing.T) {
	if _, err := exec.LookPath("kb"); err != nil {
		t.Skip("kb not installed")
	}
	db := filepath.Join(t.TempDir(), "t.db")
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("kb", append([]string{"-db", db}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("kb %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("project", "add", "demo", "demo")
	run("source", "add", "Bad paper", "--doi", "10.1/bad")
	run("observation", "add", "--project", "demo", "--source-doi", "10.1/bad", "finding", "zebra stripes repel flies")
	run("source", "retract", "1", "Retracted for data fabrication")
	run("concept", "add", "zebra", "striped horse")
	run("link", "observation", "1", "zebra")
	out, err := exec.Command("kb", "-db", db, "-json", "concept", "recall", "zebra").Output()
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	res, err := ParseKBRecall(out)
	if err != nil || len(res.Hits) != 1 {
		t.Fatalf("got %+v, %v", res, err)
	}
	if s := FormatKBRecallHit(res.Hits[0]); !strings.Contains(s, "RETRACTED: Retracted for data fabrication") {
		t.Errorf("retraction note missing from:\n%s", s)
	}
	// And nothing matched is exit 1 with the "negative" class, which we read as empty.
	out2, _ := exec.Command("kb", "-db", db, "-json", "concept", "recall", "qqqq").CombinedOutput()
	if r, err := ParseKBRecall(out2); err != nil || len(r.Hits) != 0 {
		t.Errorf("no-match: %+v, %v", r, err)
	}
}

// DR-0090 (kb 0.0.30): a draft observation is not recalled. The fixtures were
// captured from kb 0.0.30; in both, a model-drafted observation shares the
// concept with, or is the only observation of, a concept that is recalled.
func TestKBRecallContract_DraftIsNotRecalled(t *testing.T) {
	res, err := ParseKBRecall(readKBFixture(t, "concept_recall_draft_excluded.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].ID != 1 {
		t.Fatalf("want only the reviewed observation, got %+v", res.Hits)
	}
	for _, h := range res.Hits {
		if strings.Contains(FormatKBRecallHit(h), "model guess") {
			t.Errorf("a draft reached the prompt text: %q", FormatKBRecallHit(h))
		}
	}
}

// A concept that only a draft is linked to still matches, with no hits: an
// empty result, not an error and not the draft's text.
func TestKBRecallContract_ConceptOnlyADraftLinksHasNoHits(t *testing.T) {
	res, err := ParseKBRecall(readKBFixture(t, "concept_recall_draft_only.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 || len(res.Matched) != 1 || res.Matched[0] != "okapi" {
		t.Fatalf("got %+v", res)
	}
}

// Against the installed kb: a draft recorded with `add --draft` stays out of
// recall until promoted. Skipped when kb is absent or older than harvey needs.
func TestKBRecallContract_InstalledKBHidesDrafts(t *testing.T) {
	if _, err := lookKB(); err != nil {
		t.Skipf("no usable kb: %v", err)
	}
	db := filepath.Join(t.TempDir(), "t.db")
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("kb", append([]string{"-db", db}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("kb %v: %v\n%s", args, err, out)
		}
	}
	run("project", "add", "demo", "demo")
	run("concept", "add", "zebra", "striped horse")
	run("observation", "add", "--project", "demo", "finding", "zebra stripes repel flies")
	run("observation", "add", "--project", "demo", "--draft", "--by", "harvey test", "finding", "zebra stripes are a model guess")
	run("link", "observation", "1", "zebra")
	run("link", "observation", "2", "zebra")
	out, err := exec.Command("kb", "-db", db, "-json", "concept", "recall", "zebra").Output()
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	res, err := ParseKBRecall(out)
	if err != nil || len(res.Hits) != 1 || strings.Contains(res.Hits[0].Excerpt, "model guess") {
		t.Fatalf("got %+v, %v", res, err)
	}
}
