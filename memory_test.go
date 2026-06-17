package harvey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── ParseMemoryDoc ────────────────────────────────────────────────────────────

func TestParseMemoryDoc_Valid(t *testing.T) {
	input := `---
id: git_fix_a3f891
type: tool_use
created_at: "2026-05-25T12:00:00Z"
updated_at: "2026-05-25T12:00:00Z"
supersedes: []
tags:
    - git
    - error
    - fix
description: Fixed fatal not a git repository
summary: When git reports fatal not a git repository run git init.
source_session: agents/sessions/test.spmd
---

FADE IN:

INT. MEMORY 2026-05-25 12:00:00

RSDOIEL
I got: fatal: not a git repository.

HARVEY
Running git init.

THE END.
`
	doc, err := ParseMemoryDoc([]byte(input))
	if err != nil {
		t.Fatalf("ParseMemoryDoc: %v", err)
	}
	if doc.Meta.ID != "git_fix_a3f891" {
		t.Errorf("ID: got %q, want %q", doc.Meta.ID, "git_fix_a3f891")
	}
	if doc.Meta.Type != MemoryTypeToolUse {
		t.Errorf("Type: got %q, want %q", doc.Meta.Type, MemoryTypeToolUse)
	}
	if len(doc.Meta.Tags) != 3 {
		t.Errorf("Tags length: got %d, want 3", len(doc.Meta.Tags))
	}
	if !strings.Contains(doc.FountainBody, "FADE IN:") {
		t.Error("FountainBody should contain FADE IN:")
	}
	if !strings.Contains(doc.FountainBody, "THE END.") {
		t.Error("FountainBody should contain THE END.")
	}
	if !strings.Contains(doc.FountainBody, "INT. MEMORY") {
		t.Error("FountainBody should contain INT. MEMORY scene heading")
	}
}

func TestParseMemoryDoc_MissingFrontMatter(t *testing.T) {
	_, err := ParseMemoryDoc([]byte("FADE IN:\n\nRSDOIEL\nHello.\n"))
	if err == nil {
		t.Fatal("expected error for missing front matter")
	}
}

func TestParseMemoryDoc_UnclosedFrontMatter(t *testing.T) {
	_, err := ParseMemoryDoc([]byte("---\nid: foo\ntype: tool_use\n"))
	if err == nil {
		t.Fatal("expected error for unclosed front matter")
	}
}

func TestParseMemoryDoc_MissingID(t *testing.T) {
	input := "---\ntype: tool_use\ndescription: test\n---\nFADE IN:\n"
	_, err := ParseMemoryDoc([]byte(input))
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

// ── Round-trip ────────────────────────────────────────────────────────────────

func TestMemoryDocRoundTrip(t *testing.T) {
	doc := NewMemoryDoc(
		"wf_001",
		MemoryTypeWorkflow,
		"Deploy Go binary to Raspberry Pi",
		"Cross-compile with GOARCH=arm64 then scp to the Pi.",
		[]string{"go", "deploy", "raspberry-pi"},
	)
	doc.FountainBody = BuildFountainBody("2026-05-25 12:00:00", [][2]string{
		{"RSDOIEL", "How do I deploy to the Pi?"},
		{"HARVEY", "Cross-compile with GOARCH=arm64 GOOS=linux, then scp the binary."},
	})

	data, err := doc.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}

	doc2, err := ParseMemoryDoc(data)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if doc2.Meta.ID != doc.Meta.ID {
		t.Errorf("ID mismatch: %q vs %q", doc2.Meta.ID, doc.Meta.ID)
	}
	if doc2.Meta.Type != doc.Meta.Type {
		t.Errorf("Type mismatch: %q vs %q", doc2.Meta.Type, doc.Meta.Type)
	}
	if doc2.Meta.Description != doc.Meta.Description {
		t.Errorf("Description mismatch")
	}
	if doc2.FountainBody != doc.FountainBody {
		t.Errorf("FountainBody mismatch:\ngot:  %q\nwant: %q",
			doc2.FountainBody, doc.FountainBody)
	}
}

// ── FilePath / ArchivePath ────────────────────────────────────────────────────

func TestMemoryDocFilePath(t *testing.T) {
	doc := &MemoryDoc{Meta: MemoryMeta{ID: "abc123", Type: MemoryTypeToolUse}}
	got := doc.FilePath("/memories")
	want := "/memories/tool_use/abc123.fountain"
	if got != want {
		t.Errorf("FilePath: got %q, want %q", got, want)
	}
}

func TestMemoryDocArchivePath(t *testing.T) {
	doc := &MemoryDoc{Meta: MemoryMeta{ID: "abc123", Type: MemoryTypeWorkflow}}
	got := doc.ArchivePath("/memories")
	want := "/memories/archive/workflow/abc123.fountain"
	if got != want {
		t.Errorf("ArchivePath: got %q, want %q", got, want)
	}
}

// ── EmbedText ─────────────────────────────────────────────────────────────────

func TestEmbedText(t *testing.T) {
	doc := NewMemoryDoc("x", MemoryTypeToolUse, "desc", "summary text", []string{"a", "b"})
	text := doc.EmbedText()
	if !strings.Contains(text, "desc") {
		t.Error("EmbedText should contain description")
	}
	if !strings.Contains(text, "a b") {
		t.Error("EmbedText should contain joined tags")
	}
	if !strings.Contains(text, "summary text") {
		t.Error("EmbedText should contain summary")
	}
}

func TestEmbedText_IncludesAction(t *testing.T) {
	doc := NewMemoryDoc("x", MemoryTypeToolUse, "desc", "summary", []string{"tag"})
	doc.Meta.Action = "Run git init in project root."
	text := doc.EmbedText()
	if !strings.Contains(text, "Run git init in project root.") {
		t.Error("EmbedText should contain action when non-empty")
	}
}

func TestEmbedText_EmptyActionOmitted(t *testing.T) {
	doc := NewMemoryDoc("x", MemoryTypeToolUse, "desc", "summary", []string{"tag"})
	// Action is empty by default; EmbedText should not contain trailing whitespace
	// or an empty token from a blank action.
	text := doc.EmbedText()
	if strings.HasSuffix(text, " ") {
		t.Error("EmbedText should not have trailing space when action is empty")
	}
}

func TestParseMemoryDoc_DefaultConfidence(t *testing.T) {
	input := `---
id: conf_test_001
type: tool_use
created_at: "2026-06-01T00:00:00Z"
updated_at: "2026-06-01T00:00:00Z"
supersedes: []
tags: []
description: Test memory without confidence field
summary: Verifies that confidence defaults to 0.5 when absent.
---

FADE IN:

INT. MEMORY 2026-06-01 00:00:00

HARVEY
No confidence field in front matter.

THE END.
`
	doc, err := ParseMemoryDoc([]byte(input))
	if err != nil {
		t.Fatalf("ParseMemoryDoc: %v", err)
	}
	if doc.Meta.Confidence != 0.5 {
		t.Errorf("Confidence: got %v, want 0.5", doc.Meta.Confidence)
	}
}

func TestNewMemoryDoc_DefaultConfidence(t *testing.T) {
	doc := NewMemoryDoc("x", MemoryTypeToolUse, "desc", "summary", []string{"tag"})
	if doc.Meta.Confidence != 0.5 {
		t.Errorf("NewMemoryDoc Confidence: got %v, want 0.5", doc.Meta.Confidence)
	}
}

// ── GenerateMemoryID ──────────────────────────────────────────────────────────

func TestGenerateMemoryID_UniqueAndPrefixed(t *testing.T) {
	id1 := GenerateMemoryID(MemoryTypeToolUse)
	time.Sleep(2 * time.Millisecond)
	id2 := GenerateMemoryID(MemoryTypeToolUse)
	if id1 == id2 {
		t.Error("GenerateMemoryID produced duplicate IDs")
	}
	if !strings.HasPrefix(id1, "tool_use_") {
		t.Errorf("ID %q should start with tool_use_", id1)
	}
}

// ── BuildFountainBody ─────────────────────────────────────────────────────────

func TestBuildFountainBody(t *testing.T) {
	body := BuildFountainBody("2026-05-25 12:00:00", [][2]string{
		{"RSDOIEL", "Hello"},
		{"HARVEY", "World"},
	})
	if !strings.Contains(body, "FADE IN:") {
		t.Error("should contain FADE IN:")
	}
	if !strings.Contains(body, "INT. MEMORY 2026-05-25 12:00:00") {
		t.Error("should contain INT. MEMORY heading with timestamp")
	}
	if !strings.Contains(body, "RSDOIEL\nHello") {
		t.Error("should contain first turn")
	}
	if !strings.Contains(body, "THE END.") {
		t.Error("should contain THE END.")
	}
}

// ── Scrub ─────────────────────────────────────────────────────────────────────

func TestScrubPathNormalization(t *testing.T) {
	content := "working_directory: /home/alice/myproject/src"
	result := Scrub(content, "/home/alice/myproject")
	if strings.Contains(result.Content, "/home/alice/myproject") {
		t.Error("workspace path should be replaced")
	}
	if !strings.Contains(result.Content, "<workspace>") {
		t.Error("should contain <workspace> placeholder")
	}
	if len(result.Flags) != 0 {
		t.Errorf("no sensitive patterns expected, got: %v", result.Flags)
	}
}

func TestScrubPatternFlagging(t *testing.T) {
	cases := []struct {
		content string
		pattern string
	}{
		{"export PASSWORD=hunter2", "password"},
		{"Authorization: Bearer abc123", "Bearer "},
		{"-----BEGIN RSA PRIVATE KEY-----", "-----BEGIN"},
		{"api_key: sk-1234", "api_key"},
	}
	for _, tc := range cases {
		result := Scrub(tc.content, "")
		found := false
		for _, f := range result.Flags {
			if strings.Contains(strings.ToLower(f), strings.ToLower(tc.pattern)) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pattern %q not flagged in %q; flags: %v", tc.pattern, tc.content, result.Flags)
		}
	}
}

func TestScrubNoFalsePositives(t *testing.T) {
	content := "Running git init in the project directory."
	result := Scrub(content, "")
	if len(result.Flags) != 0 {
		t.Errorf("unexpected flags: %v", result.Flags)
	}
}

func TestScrubDeduplicatesFlags(t *testing.T) {
	// "password" appears twice; should only produce one flag.
	content := "password=x, check password again"
	result := Scrub(content, "")
	count := 0
	for _, f := range result.Flags {
		if strings.Contains(f, "password") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 password flag, got %d", count)
	}
}

// ── Manifest ──────────────────────────────────────────────────────────────────

func TestManifestIsMinedFalse(t *testing.T) {
	m := &Manifest{Sessions: []ManifestEntry{}}
	if m.IsMined("agents/sessions/foo.spmd") {
		t.Error("IsMined should be false for unknown session")
	}
}

func TestManifestRecordAndIsMined(t *testing.T) {
	m := &Manifest{Sessions: []ManifestEntry{}}
	m.Record("agents/sessions/foo.spmd", []string{"tool_use_abc"}, 2)
	if !m.IsMined("agents/sessions/foo.spmd") {
		t.Error("IsMined should be true after Record")
	}
	if m.IsMined("agents/sessions/bar.spmd") {
		t.Error("IsMined should be false for different session")
	}
}

func TestManifestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{}
	m.Record("agents/sessions/foo.spmd", []string{"id1", "id2"}, 3)

	if err := m.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	m2, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m2.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(m2.Sessions))
	}
	e := m2.Sessions[0]
	if e.Path != "agents/sessions/foo.spmd" {
		t.Errorf("Path: got %q", e.Path)
	}
	if len(e.MemoriesCreated) != 2 {
		t.Errorf("MemoriesCreated: got %d", len(e.MemoriesCreated))
	}
	if e.MemoriesSkipped != 3 {
		t.Errorf("MemoriesSkipped: got %d", e.MemoriesSkipped)
	}
}

func TestLoadManifestMissingFile(t *testing.T) {
	dir := t.TempDir()
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest on missing file: %v", err)
	}
	if len(m.Sessions) != 0 {
		t.Error("expected empty sessions for missing manifest")
	}
}

func TestManifestUnminedSessions(t *testing.T) {
	dir := t.TempDir()

	// Create two session files.
	for _, name := range []string{"a.spmd", "b.spmd", "c.fountain"} {
		os.WriteFile(filepath.Join(dir, name), []byte("session"), 0o644)
	}
	// Create a non-session file.
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme"), 0o644)

	m := &Manifest{}
	m.Record(filepath.Join(dir, "a.spmd"), nil, 0)

	unmined, err := m.UnminedSessions(dir)
	if err != nil {
		t.Fatalf("UnminedSessions: %v", err)
	}
	if len(unmined) != 2 {
		t.Errorf("expected 2 unmined sessions, got %d: %v", len(unmined), unmined)
	}
	for _, p := range unmined {
		if filepath.Base(p) == "a.spmd" {
			t.Error("a.spmd should be mined and not returned")
		}
		if filepath.Base(p) == "README.md" {
			t.Error("README.md should not be returned")
		}
	}
}
