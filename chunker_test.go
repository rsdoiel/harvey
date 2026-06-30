package harvey

import (
	"strings"
	"testing"
)

// ── DetectDocType ─────────────────────────────────────────────────────────────

func TestDetectDocType(t *testing.T) {
	cases := []struct {
		path string
		want DocType
	}{
		{"README.md", DocTypeProse},
		{"notes.txt", DocTypeProse},
		{"index.html", DocTypeProse},
		{"config.yaml", DocTypeProse},
		{"schema.json", DocTypeProse},
		{"data.unknown", DocTypeProse},
		{"noextension", DocTypeProse},
		{"main.go", DocTypeSource},
		{"app.ts", DocTypeSource},
		{"script.py", DocTypeSource},
		{"lib.js", DocTypeSource},
		{"prog.pas", DocTypeSource},
	}
	for _, tc := range cases {
		got := DetectDocType(tc.path)
		if got != tc.want {
			t.Errorf("DetectDocType(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// ── ChunkDocument ─────────────────────────────────────────────────────────────

func TestChunkDocument_SingleChunk(t *testing.T) {
	// Content well below threshold (ChunkSizeBytes=6000, threshold=4500).
	cfg := DefaultChunkConfig()
	content := "Short document.\n\nOnly two paragraphs.\n\nNot very long."
	chunks := ChunkDocument(content, cfg, DocTypeProse)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Content, "Short document") {
		t.Error("chunk does not contain original content")
	}
}

func TestChunkDocument_EmptyContent(t *testing.T) {
	cfg := DefaultChunkConfig()
	chunks := ChunkDocument("", cfg, DocTypeProse)
	if len(chunks) == 0 {
		t.Error("ChunkDocument must return at least one chunk")
	}
}

func TestChunkDocument_ParagraphSplit(t *testing.T) {
	cfg := DefaultChunkConfig()
	cfg.ChunkSizeBytes = 100 // tiny threshold to force splitting
	cfg.Overlap = "none"

	// Three paragraphs each ~60 bytes; threshold = 75 bytes.
	p1 := strings.Repeat("a", 60)
	p2 := strings.Repeat("b", 60)
	p3 := strings.Repeat("c", 60)
	content := p1 + "\n\n" + p2 + "\n\n" + p3

	chunks := ChunkDocument(content, cfg, DocTypeProse)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks from 3 paragraphs at threshold=75, got %d", len(chunks))
	}
	// Every paragraph should appear in exactly one chunk.
	seen := map[string]int{"a": 0, "b": 0, "c": 0}
	for _, ch := range chunks {
		if strings.Contains(ch.Content, p1) {
			seen["a"]++
		}
		if strings.Contains(ch.Content, p2) {
			seen["b"]++
		}
		if strings.Contains(ch.Content, p3) {
			seen["c"]++
		}
	}
	for letter, count := range seen {
		if count == 0 {
			t.Errorf("paragraph %q missing from all chunks", letter)
		}
	}
}

func TestChunkDocument_SourceSplit(t *testing.T) {
	cfg := DefaultChunkConfig()
	cfg.ChunkSizeBytes = 80 // threshold = 60 bytes; fn1 is ~111 bytes so it triggers a split
	cfg.Overlap = "none"

	// Two Go functions separated by a blank line; each ~111 bytes after padding.
	fn1 := "func Foo() {\n\treturn\n}"
	fn2 := "func Bar() {\n\treturn\n}"
	// Pad each to ~111 bytes (22 + 1 + 88).
	fn1 = fn1 + "\n" + strings.Repeat("// comment\n", 8)
	fn2 = fn2 + "\n" + strings.Repeat("// comment\n", 8)
	content := fn1 + "\n\n" + fn2

	chunks := ChunkDocument(content, cfg, DocTypeSource)
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 chunks from two Go functions, got %d", len(chunks))
	}
	foundFoo := false
	foundBar := false
	for _, ch := range chunks {
		if strings.Contains(ch.Content, "func Foo") {
			foundFoo = true
		}
		if strings.Contains(ch.Content, "func Bar") {
			foundBar = true
		}
	}
	if !foundFoo {
		t.Error("func Foo missing from all chunks")
	}
	if !foundBar {
		t.Error("func Bar missing from all chunks")
	}
}

func TestChunkDocument_ClosingBraceMerge(t *testing.T) {
	// A closing brace on its own after a blank line should be merged
	// into the preceding unit for DocTypeSource, not become its own chunk.
	cfg := DefaultChunkConfig()
	cfg.ChunkSizeBytes = 10000 // large — no splitting expected
	cfg.Overlap = "none"

	content := "func Foo() {\n\tx := 1\n\n}\n\nfunc Bar() {}"
	chunks := ChunkDocument(content, cfg, DocTypeSource)

	// The closing } of Foo should be in the same chunk as the opening.
	for _, ch := range chunks {
		if strings.Contains(ch.Content, "func Foo") {
			if !strings.Contains(ch.Content, "}") {
				t.Error("closing brace of Foo not merged into its chunk")
			}
		}
	}
}

func TestChunkDocument_Overlap(t *testing.T) {
	cfg := DefaultChunkConfig()
	cfg.ChunkSizeBytes = 100 // small threshold to force at least 2 chunks
	cfg.Overlap = "paragraph"

	p1 := strings.Repeat("x", 60)
	p2 := strings.Repeat("y", 60)
	p3 := strings.Repeat("z", 60)
	content := p1 + "\n\n" + p2 + "\n\n" + p3

	chunks := ChunkDocument(content, cfg, DocTypeProse)
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 chunks, got %d", len(chunks))
	}

	// The last paragraph of chunk[0] must appear at the start of chunk[1].
	last0 := lastParagraph(chunks[0].Content)
	if last0 == "" {
		t.Fatal("could not identify last paragraph of chunk[0]")
	}
	if !strings.HasPrefix(chunks[1].Content, last0) {
		t.Errorf("chunk[1] does not begin with last paragraph of chunk[0]:\nwant prefix: %q\ngot start:   %q",
			last0, chunks[1].Content[:min(len(last0)+20, len(chunks[1].Content))])
	}
}

func TestChunkDocument_NoOverlap(t *testing.T) {
	cfg := DefaultChunkConfig()
	cfg.ChunkSizeBytes = 100
	cfg.Overlap = "none"

	p1 := strings.Repeat("a", 60)
	p2 := strings.Repeat("b", 60)
	p3 := strings.Repeat("c", 60)
	content := p1 + "\n\n" + p2 + "\n\n" + p3

	chunks := ChunkDocument(content, cfg, DocTypeProse)
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 chunks, got %d", len(chunks))
	}

	// No paragraph should appear in two consecutive chunks.
	for i := 0; i < len(chunks)-1; i++ {
		for _, para := range strings.Split(chunks[i].Content, "\n\n") {
			para = strings.TrimSpace(para)
			if para == "" {
				continue
			}
			if strings.Contains(chunks[i+1].Content, para) {
				t.Errorf("paragraph %q appears in both chunk[%d] and chunk[%d] (overlap=none)",
					para[:min(20, len(para))], i, i+1)
			}
		}
	}
}

func TestChunkDocument_MaxChunksNotEnforced(t *testing.T) {
	// ChunkDocument does not cap at MaxChunks — the caller handles the warning.
	cfg := DefaultChunkConfig()
	cfg.ChunkSizeBytes = 50 // tiny: many chunks
	cfg.MaxChunks = 2
	cfg.Overlap = "none"

	// Build content that will produce >2 chunks.
	var b strings.Builder
	for i := 0; i < 10; i++ {
		b.WriteString(strings.Repeat("w", 60))
		b.WriteString("\n\n")
	}
	chunks := ChunkDocument(b.String(), cfg, DocTypeProse)
	if len(chunks) <= cfg.MaxChunks {
		t.Errorf("expected >%d chunks but got %d (MaxChunks should not be enforced here)",
			cfg.MaxChunks, len(chunks))
	}
}

// ── DocumentChunk line numbers ────────────────────────────────────────────────

func TestChunkDocument_LineNumbers_SingleChunk(t *testing.T) {
	cfg := DefaultChunkConfig()
	// 5 lines: p1 (line 1), blank (2), p2 (3), blank (4), p3 (5).
	content := "First paragraph.\n\nSecond paragraph.\n\nThird paragraph."
	chunks := ChunkDocument(content, cfg, DocTypeProse)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].StartLine != 1 {
		t.Errorf("StartLine: got %d, want 1", chunks[0].StartLine)
	}
	if chunks[0].EndLine != 5 {
		t.Errorf("EndLine: got %d, want 5", chunks[0].EndLine)
	}
}

func TestChunkDocument_LineNumbers_MultiChunk_NoOverlap(t *testing.T) {
	cfg := DefaultChunkConfig()
	cfg.ChunkSizeBytes = 100 // threshold = 75; two 60-byte paras exceed it
	cfg.Overlap = "none"

	// p1 at line 1, p2 at line 3, p3 at line 5 (each single-line para).
	p1 := strings.Repeat("a", 60)
	p2 := strings.Repeat("b", 60)
	p3 := strings.Repeat("c", 60)
	content := p1 + "\n\n" + p2 + "\n\n" + p3

	chunks := ChunkDocument(content, cfg, DocTypeProse)
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 chunks, got %d", len(chunks))
	}
	if chunks[0].StartLine != 1 {
		t.Errorf("chunk[0].StartLine: got %d, want 1", chunks[0].StartLine)
	}
	// With no-overlap, adjacent spans must not overlap.
	for i := 0; i < len(chunks)-1; i++ {
		if chunks[i].EndLine >= chunks[i+1].StartLine {
			t.Errorf("chunk[%d].EndLine=%d overlaps chunk[%d].StartLine=%d (no-overlap mode)",
				i, chunks[i].EndLine, i+1, chunks[i+1].StartLine)
		}
	}
}

func TestChunkDocument_LineNumbers_MultiChunk_Overlap(t *testing.T) {
	cfg := DefaultChunkConfig()
	cfg.ChunkSizeBytes = 100
	cfg.Overlap = "paragraph"

	p1 := strings.Repeat("x", 60)
	p2 := strings.Repeat("y", 60)
	p3 := strings.Repeat("z", 60)
	content := p1 + "\n\n" + p2 + "\n\n" + p3

	chunks := ChunkDocument(content, cfg, DocTypeProse)
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 chunks, got %d", len(chunks))
	}
	// With overlap, adjacent chunks share a boundary line (the overlap paragraph).
	if chunks[1].StartLine > chunks[0].EndLine {
		t.Errorf("overlap mode: chunk[1].StartLine=%d should be ≤ chunk[0].EndLine=%d",
			chunks[1].StartLine, chunks[0].EndLine)
	}
}

func TestChunkDocument_LineNumbers_MultilineParas(t *testing.T) {
	cfg := DefaultChunkConfig()
	// para1 spans lines 1–3, para2 starts at line 5.
	para1 := "line one\nline two\nline three"
	para2 := "line five"
	content := para1 + "\n\n" + para2
	chunks := ChunkDocument(content, cfg, DocTypeProse)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk (content fits in budget), got %d", len(chunks))
	}
	if chunks[0].StartLine != 1 {
		t.Errorf("StartLine: got %d, want 1", chunks[0].StartLine)
	}
	// para2 is on line 5 and has no newlines, so EndLine = 5.
	if chunks[0].EndLine != 5 {
		t.Errorf("EndLine: got %d, want 5", chunks[0].EndLine)
	}
}

func TestChunkDocument_LineNumbers_StartsAtOne(t *testing.T) {
	// The first line of any document must be StartLine=1.
	cfg := DefaultChunkConfig()
	content := "hello world"
	chunks := ChunkDocument(content, cfg, DocTypeProse)
	if chunks[0].StartLine != 1 {
		t.Errorf("StartLine of first chunk: got %d, want 1", chunks[0].StartLine)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// lastParagraph returns the last non-empty paragraph of s.
func lastParagraph(s string) string {
	parts := strings.Split(s, "\n\n")
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		if p != "" {
			return p
		}
	}
	return ""
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
