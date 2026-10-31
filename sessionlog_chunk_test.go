package harvey

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// chunkDoc is four paragraphs of about 250 bytes, so a chunk size of 300 gives
// one chunk to each.
func chunkDoc() string {
	var b strings.Builder
	for i := 1; i <= 4; i++ {
		b.WriteString("Paragraph " + string(rune('0'+i)) + " says a number of things about chunked analysis. ")
		b.WriteString(strings.Repeat("It goes on at some length so that the paragraph is long enough. ", 3))
		b.WriteString("\n\n")
	}
	return b.String()
}

func chunkSession(t *testing.T, failOn int) (dir string) {
	t.Helper()
	// Interactive, as at a terminal: a failed command there does not end the run
	// with its exit class, which a non-interactive session would.
	scriptedFailOn, scriptedTerminal = failOn, true
	t.Cleanup(func() { scriptedFailOn, scriptedTerminal = 0, false })
	return scriptedRunWith(t, false, func(root string) {
		os.WriteFile(filepath.Join(root, "doc.md"), []byte(chunkDoc()), 0o644)
	}, false, "/read-chunks doc.md --chunk-size 300 summarise it", "/exit")
}

func TestChunkAnalysis_TheLogRecordsEachChunkAndTheSynthesis(t *testing.T) {
	dir := chunkSession(t, 0)
	events, _, _ := logAndFountain(t, dir)
	type req struct {
		Form, Purpose string
		Source        struct {
			Path      string `json:"path"`
			Chunk     int    `json:"chunk"`
			Of        int    `json:"of"`
			Chunks    int    `json:"chunks"`
			DocType   string `json:"doc_type"`
			ChunkSize int    `json:"chunk_size"`
		}
	}
	var chunks, synth []req
	for _, e := range events {
		if e.Kind != KindModelRequest || e.Level != LevelInfo {
			continue
		}
		var r req
		_ = json.Unmarshal(e.Data, &r)
		switch r.Purpose {
		case "chunk":
			chunks = append(chunks, r)
		case "synthesis":
			synth = append(synth, r)
		}
	}
	if len(chunks) != 4 || len(synth) != 1 {
		t.Fatalf("%d chunk requests and %d synthesis requests, want 4 and 1", len(chunks), len(synth))
	}
	for i, c := range chunks {
		if c.Form != "adhoc" || c.Source.Path != "doc.md" || c.Source.Chunk != i+1 || c.Source.Of != 4 ||
			c.Source.DocType != "prose" || c.Source.ChunkSize != 300 {
			t.Errorf("chunk %d source = %+v", i+1, c)
		}
	}
	if synth[0].Source.Path != "doc.md" || synth[0].Source.Chunks != 4 {
		t.Errorf("synthesis source = %+v", synth[0])
	}
}

func TestRenderFountain_ChunkAnalysisMatchesTheRecorder(t *testing.T) {
	for name, failOn := range map[string]int{"all chunks ok": 0, "the second chunk fails": 2, "the synthesis fails": 5} {
		t.Run(name, func(t *testing.T) {
			dir := chunkSession(t, failOn)
			events, rec, lp := logAndFountain(t, dir)
			got := string(RenderFountain(events, RenderOptions{Zone: time.Local, Blobs: BlobDir(lp)}))
			// Scene headings are upper case in Fountain.
			if !strings.Contains(got, "INT. CHUNK ANALYSIS — DOC.MD") || !strings.Contains(got, "Synthesizing results.") {
				t.Errorf("no chunk analysis scene in the rendering:\n%s", got)
			}
			if a, b := normaliseFountain(rec), normaliseFountain(got); a != b {
				t.Errorf("rendering differs from the recorder's\n--- recorder ---\n%s\n--- renderer ---\n%s", a, b)
			}
		})
	}
}

// Inside a turn (the model asked for an analysis, or an oversized file was
// chunked) the chunk responses are not the turn's reply, and the recorder wrote
// the analysis scene before the turn's own scene.
func TestRenderFountain_ChunkAnalysisInsideATurnIsNotTheReply(t *testing.T) {
	l, opts := openTestLog(t, func(o *SessionLogOptions) { o.Start.User = "ALICE"; o.Start.Backend = "ollama"; o.Start.Model = "m" })
	mustAppend(t, l, KindUserInput, LevelInfo, map[string]any{"text": "summarise the big file"})
	src := func(n int) map[string]any {
		return map[string]any{"path": "big.md", "chunk": n, "of": 1, "doc_type": "prose", "chunk_size": 100}
	}
	r1 := mustAppend(t, l, KindModelRequest, LevelInfo, map[string]any{"form": "adhoc", "purpose": "chunk", "source": src(1), "backend": "ollama", "model": "m"})
	mustAppend(t, l, KindModelResponse, LevelInfo, map[string]any{"request": r1, "status": "ok", "text": "CHUNK SAYS"})
	r2 := mustAppend(t, l, KindModelRequest, LevelInfo, map[string]any{"form": "adhoc", "purpose": "synthesis", "source": map[string]any{"path": "big.md", "chunks": 1}, "backend": "ollama", "model": "m"})
	mustAppend(t, l, KindModelResponse, LevelInfo, map[string]any{"request": r2, "status": "ok", "text": "SYNTHESIS SAYS"})
	r3 := mustAppend(t, l, KindModelRequest, LevelInfo, map[string]any{"form": "assembled", "backend": "ollama", "model": "m"})
	mustAppend(t, l, KindModelResponse, LevelInfo, map[string]any{"request": r3, "status": "ok", "text": "THE REPLY"})
	_ = l.Close("quit")
	events, _, _ := ReadSessionLog(logPath(opts))
	got := string(RenderFountain(events, RenderOptions{Zone: time.UTC}))
	chunk := strings.Index(got, "INT. CHUNK ANALYSIS — BIG.MD")
	turn := strings.Index(got, "INT. HARVEY AND ALICE TALKING")
	if !(chunk >= 0 && chunk < turn) {
		t.Errorf("the analysis scene should come before the turn's scene (%d, %d):\n%s", chunk, turn, got)
	}
	reply := strings.Index(got, "\nM\nTHE REPLY")
	if reply < 0 {
		t.Errorf("the turn's reply should be THE REPLY, not a chunk's answer:\n%s", got)
	}
	if strings.Contains(got[turn:], "SYNTHESIS SAYS") || strings.Contains(got[turn:], "CHUNK SAYS") {
		t.Errorf("a chunk answer leaked into the turn scene:\n%s", got[turn:])
	}
}
