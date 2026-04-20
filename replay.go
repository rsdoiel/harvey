package harvey

// replay.go — ContinueFromFountain and ReplayFromFountain.
//
// Both functions share parseFountainSession, which walks a Harvey-format
// Fountain file and extracts ordered chat turns (user input + model reply).
// Non-chat scenes (INT. AGENT MODE, INT. SKILL ...) are silently skipped.
//
// ContinueFromFountain pre-populates Agent history so the REPL resumes with
// full context. ReplayFromFountain re-sends each user message to the current
// backend, records fresh responses to a new Fountain file, and applies any
// tagged code blocks with backup-before-overwrite protection.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rsdoiel/fountain"
)

// PlaybackTurn holds one chat exchange extracted from a Harvey Fountain file.
type PlaybackTurn struct {
	UserInput  string // USER's message text
	ModelReply string // model's original response (kept for comparison in replay)
}

// parseFountainSession walks a Harvey Fountain session file and returns the
// user name, model name, and ordered chat turns. Non-chat scenes are skipped.
//
// The user name is extracted from the first chat scene heading
// ("INT. HARVEY AND {USER} TALKING …"). The model name is extracted from the
// first HARVEY dialogue line that begins "Forwarding to {MODEL}.".
//
// Parameters:
//
//	path (string) — path to a Harvey .fountain session file.
//
// Returns:
//
//	userName  (string)         — ALL-CAPS user name found in the heading.
//	modelName (string)         — ALL-CAPS model name found in HARVEY's dialogue.
//	turns     ([]PlaybackTurn) — chat turns in document order.
//	error     — if the file cannot be parsed.
//
// Example:
//
//	user, model, turns, err := parseFountainSession("session.fountain")
func parseFountainSession(path string) (userName, modelName string, turns []PlaybackTurn, err error) {
	doc, err := fountain.ParseFile(path)
	if err != nil {
		return "", "", nil, fmt.Errorf("replay: parse %s: %w", path, err)
	}

	userName = "USER"
	modelName = "MODEL"

	inChatScene := false
	lastChar := ""
	var cur PlaybackTurn
	inTurn := false

	for _, elem := range doc.Elements {
		switch elem.Type {
		case fountain.SceneHeadingType:
			heading := strings.ToUpper(strings.TrimSpace(elem.Content))
			if strings.Contains(heading, " HARVEY AND ") && strings.Contains(heading, " TALKING ") {
				if inTurn && cur.UserInput != "" {
					turns = append(turns, cur)
				}
				cur = PlaybackTurn{}
				inTurn = true
				inChatScene = true
				// Extract user name: "INT. HARVEY AND {USER} TALKING {TIMESTAMP}"
				if i := strings.Index(heading, " HARVEY AND "); i >= 0 {
					rest := heading[i+len(" HARVEY AND "):]
					if j := strings.Index(rest, " TALKING "); j > 0 {
						userName = strings.TrimSpace(rest[:j])
					}
				}
			} else {
				inChatScene = false
			}
			lastChar = ""

		case fountain.CharacterType:
			if inChatScene {
				lastChar = strings.ToUpper(strings.TrimSpace(fountain.CharacterName(elem)))
			}

		case fountain.DialogueType:
			if !inChatScene {
				continue
			}
			text := strings.TrimSpace(elem.Content)
			switch {
			case lastChar == userName:
				cur.UserInput = text
			case lastChar == "HARVEY" && strings.HasPrefix(text, "Forwarding to "):
				m := strings.TrimPrefix(text, "Forwarding to ")
				m = strings.TrimSuffix(strings.TrimSpace(m), ".")
				if m != "" {
					modelName = m
				}
			case lastChar != "" && lastChar != "HARVEY" && lastChar != userName:
				// Any character that is not HARVEY and not USER is the model.
				cur.ModelReply = text
			}
		}
	}
	if inTurn && cur.UserInput != "" {
		turns = append(turns, cur)
	}

	return userName, modelName, turns, nil
}

// ContinueFromFountain loads the chat history from a Harvey Fountain session
// file and appends it to the Agent's current history so the conversation can
// be resumed. Call this after the system prompt has been added to history.
//
// Parameters:
//
//	path (string) — path to a Harvey .fountain session file.
//
// Returns:
//
//	int   — number of chat turns loaded.
//	error — if the file cannot be parsed.
//
// Example:
//
//	n, err := agent.ContinueFromFountain("session.fountain")
//	fmt.Printf("Loaded %d turns\n", n)
func (a *Agent) ContinueFromFountain(path string) (int, error) {
	_, _, turns, err := parseFountainSession(path)
	if err != nil {
		return 0, err
	}
	for _, t := range turns {
		if t.UserInput != "" {
			a.AddMessage("user", t.UserInput)
		}
		if t.ModelReply != "" {
			a.AddMessage("assistant", t.ModelReply)
		}
	}
	return len(turns), nil
}

// ReplayFromFountain re-sends each user message from srcPath to the current
// LLM backend, records fresh responses to a new Fountain file at outPath,
// and streams progress to out.
//
// Tagged code blocks in replies are applied with backup protection: if a
// target file already exists it is renamed to <path>.bak.<YYYYMMDD-HHMMSS>
// before the new content is written. Files that cannot be backed up or
// written are noted as skipped in the new Fountain recording.
//
// Parameters:
//
//	ctx     (context.Context) — cancellable context for LLM calls.
//	srcPath (string)          — Harvey Fountain session file to replay.
//	outPath (string)          — destination for the new session recording.
//	out     (io.Writer)       — progress and streaming output.
//
// Returns:
//
//	error — on parse failure, recorder creation failure, or unrecoverable LLM error.
//
// Example:
//
//	err := agent.ReplayFromFountain(ctx, "old.fountain", "new.fountain", os.Stdout)
func (a *Agent) ReplayFromFountain(ctx context.Context, srcPath, outPath string, out io.Writer) error {
	if a.Client == nil {
		return fmt.Errorf("replay: no backend connected")
	}

	_, _, turns, err := parseFountainSession(srcPath)
	if err != nil {
		return err
	}
	if len(turns) == 0 {
		fmt.Fprintf(out, "  No chat turns found in %s\n", srcPath)
		return nil
	}

	rec, err := NewRecorder(outPath, a.Client.Name(), a.Workspace.Root)
	if err != nil {
		return fmt.Errorf("replay: create recorder: %w", err)
	}
	defer rec.Close()

	fmt.Fprintf(out, "  Replaying %d turns from %s\n", len(turns), srcPath)
	fmt.Fprintf(out, "  Recording to %s\n\n", outPath)

	for i, turn := range turns {
		preview := turn.UserInput
		if len(preview) > 60 {
			preview = preview[:57] + "..."
		}
		fmt.Fprintf(out, "  [%d/%d] %s\n\n", i+1, len(turns), preview)

		a.AddMessage("user", turn.UserInput)

		var buf strings.Builder
		stats, chatErr := a.Client.Chat(ctx, a.History, io.MultiWriter(out, &buf))
		if chatErr != nil {
			return fmt.Errorf("replay turn %d: %w", i+1, chatErr)
		}
		reply := strings.TrimSpace(buf.String())
		a.AddMessage("assistant", reply)
		_ = rec.RecordTurnWithStats(turn.UserInput, reply, stats, []string{a.Client.Name()}, "")

		fmt.Fprintln(out)
		fmt.Fprintln(out, dim("  "+stats.Format()))

		a.replayWriteBlocks(reply, rec, out)
		fmt.Fprintln(out)
	}

	fmt.Fprintf(out, green("✓")+" Replay complete. New session: %s\n", outPath)
	return nil
}

// replayWriteBlocks applies tagged code blocks found in a replay reply,
// backing up existing files and noting failures as Fountain notes.
func (a *Agent) replayWriteBlocks(reply string, rec *Recorder, out io.Writer) {
	blocks := findTaggedBlocks(reply)
	if len(blocks) == 0 {
		return
	}

	ts := time.Now().Format("20060102-150405")
	if rec != nil {
		_ = rec.StartAgentScene(fmt.Sprintf("Harvey proposes to write %d file(s).", len(blocks)))
	}

	for _, b := range blocks {
		abs, err := a.Workspace.AbsPath(b.path)
		if err != nil {
			fmt.Fprintf(out, "  ✗ skipped %s: %v\n", b.path, err)
			if rec != nil {
				_ = rec.RecordAgentAction("write", b.path, "all", "skipped: "+err.Error())
			}
			continue
		}

		// Back up any existing file before overwriting.
		if _, statErr := os.Stat(abs); statErr == nil {
			bakPath := abs + ".bak." + ts
			if renErr := os.Rename(abs, bakPath); renErr != nil {
				fmt.Fprintf(out, "  ✗ skipped %s: cannot backup: %v\n", b.path, renErr)
				if rec != nil {
					_ = rec.RecordAgentAction("write", b.path, "all", "skipped: cannot backup")
				}
				continue
			}
			fmt.Fprintf(out, "  ↩ backed up %s → %s\n", b.path, filepath.Base(bakPath))
		}

		if err := a.Workspace.WriteFile(b.path, []byte(b.content), 0o644); err != nil {
			fmt.Fprintf(out, "  ✗ %s: %v\n", b.path, err)
			if rec != nil {
				_ = rec.RecordAgentAction("write", b.path, "all", "error: "+err.Error())
			}
		} else {
			fmt.Fprintf(out, "  ✓ wrote %s (%d bytes)\n", b.path, len(b.content))
			if rec != nil {
				_ = rec.RecordAgentAction("write", b.path, "all", "ok")
			}
		}
	}
}
