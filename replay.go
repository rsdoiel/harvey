package harvey

// replay.go — parseFountainSession and ReplayFromSession.
//
// parseFountainSession walks a Harvey-format Fountain file and extracts ordered
// chat turns (user input + model reply); it is the reader of last resort for an
// old session with no log (sessionload.go). ReplayFromSession re-sends each user
// message of a past session to the current backend, logs fresh responses to a
// new session log, and applies any tagged code blocks with
// backup-before-overwrite protection.

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
	Route      string // the route the line was sent to with @name; "" for an ordinary turn
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
		err = fmt.Errorf("replay: parse %s: %w", path, err)
		if _, classified := ExitClassOf(err); !classified {
			err = ClassedAs(ClassData, err) // not a file error, so the content is wrong
		}
		return "", "", nil, err
	}

	userName = "USER"
	modelName = ""

	// Seed model from title page (written at session start, before any turns).
	// The Model field stores "NAME (backend)" — strip the backend suffix so
	// the returned model name can be used for auto-selection comparisons.
	for _, tp := range doc.TitlePage {
		if strings.EqualFold(tp.Name, "Model") && strings.TrimSpace(tp.Content) != "" {
			m := strings.TrimSpace(tp.Content)
			if i := strings.Index(m, " ("); i > 0 {
				m = m[:i]
			}
			modelName = m
		}
	}

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
				// A new user line after a completed exchange, even within
				// the same scene, starts a new turn — otherwise the second
				// exchange would silently overwrite the first in cur.
				if cur.UserInput != "" && cur.ModelReply != "" {
					turns = append(turns, cur)
					cur = PlaybackTurn{}
				}
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

// ReplayFromSession re-sends each user message of a past session to the
// current LLM backend and writes the answers to a new session: a log of its
// own, marked as a replay of the source, and the Fountain file rendered from
// it at the end. The source may be a session log, its Fountain file, or an old
// Fountain file with no log. Turns that went to a named route are skipped,
// since the current backend is not that route.
//
// Tagged code blocks in replies are applied with backup protection: if a
// target file already exists it is renamed to <path>.bak.<YYYYMMDD-HHMMSS>
// before the new content is written. Files that cannot be backed up or
// written are logged as skipped.
//
// Parameters:
//
//	ctx     (context.Context) — cancellable context for LLM calls.
//	srcPath (string)          — the session to replay.
//	outPath (string)          — the new session's Fountain path; its log sits beside it.
//	out     (io.Writer)       — progress and streaming output.
//
// Returns:
//
//	error — on a missing or malformed source, a failed recorder, or an unrecoverable LLM error.
//
// Example:
//
//	err := agent.ReplayFromSession(ctx, "old.jsonl", "new.spmd", os.Stdout)
func (a *Agent) ReplayFromSession(ctx context.Context, srcPath, outPath string, out io.Writer) error {
	// The source is checked before the backend, so a missing or malformed
	// replay file is reported as that and not as "no backend".
	src, err := LoadSessionInfo(srcPath)
	if err != nil {
		return err
	}
	if a.Client == nil {
		return Unavailablef("replay: no backend connected")
	}
	var turns []PlaybackTurn
	for _, t := range src.Turns {
		if t.Route == "" {
			turns = append(turns, t)
		}
	}
	if len(turns) == 0 {
		fmt.Fprintf(out, "  No chat turns found in %s\n", srcPath)
		return nil
	}

	rec, err := NewRecorder(outPath, a.Client.Name(), a.Workspace.Root)
	if err != nil {
		return fmt.Errorf("replay: create recorder: %w", err)
	}

	// The replay has its own recorder and log; the running session's are put
	// back when it ends.
	saved := *a.replayState()
	defer a.restoreReplayState(saved)
	a.Recorder, a.SessionLog = rec, nil
	a.openSessionLog(outPath, src.Path, out)
	defer a.finishSession(io.Discard)

	fmt.Fprintf(out, "  Replaying %d turns from %s\n", len(turns), srcPath)
	fmt.Fprintf(out, "  Recording to %s\n\n", outPath)

	turnCtx := withCallPurpose(ctx, purposeTurn, nil)
	for i, turn := range turns {
		preview := turn.UserInput
		if len(preview) > 60 {
			preview = preview[:57] + "..."
		}
		fmt.Fprintf(out, "  [%d/%d] %s\n\n", i+1, len(turns), preview)

		a.logUserInputOrigin(turn.UserInput, "", "replay")
		a.logTurnMessage(turn.UserInput)
		a.AddMessage("user", turn.UserInput)

		var buf strings.Builder
		stats, chatErr := a.Client.Chat(turnCtx, a.History, io.MultiWriter(out, &buf))
		if chatErr != nil {
			return fmt.Errorf("replay turn %d: %w", i+1, chatErr)
		}
		reply := strings.TrimSpace(buf.String())
		a.AddMessage("assistant", reply)

		fmt.Fprintln(out)
		fmt.Fprintln(out, dim("  "+stats.Format()))

		a.replayWriteBlocks(reply, out)
		fmt.Fprintln(out)
	}

	fmt.Fprintf(out, green("✓")+" Replay complete. New session: %s\n", outPath)
	return nil
}

// replayState is the part of the agent that belongs to one session's records.
type replayState struct {
	rec                          *Recorder
	log                          *SessionLog
	turn                         int
	lastID, firstDialogID, sysID string
	model                        ModelRef
}

func (a *Agent) replayState() *replayState {
	return &replayState{a.Recorder, a.SessionLog, a.logTurn, a.logLastID, a.logFirstDialogID, a.logSystemID, a.logModel}
}

func (a *Agent) restoreReplayState(s replayState) {
	a.Recorder, a.SessionLog, a.logTurn = s.rec, s.log, s.turn
	a.logLastID, a.logFirstDialogID, a.logSystemID, a.logModel = s.lastID, s.firstDialogID, s.sysID, s.model
}

// replayWriteBlocks applies tagged code blocks found in a replay reply,
// backing up existing files and logging each outcome as a write permission.
func (a *Agent) replayWriteBlocks(reply string, out io.Writer) {
	blocks := findTaggedBlocks(reply)
	if len(blocks) == 0 {
		return
	}
	a.offerOf = len(blocks)
	defer func() { a.offerIndex, a.offerOf = 0, 0 }()

	ts := time.Now().Format("20060102-150405")
	for i, b := range blocks {
		a.offerIndex = i + 1
		abs, err := resolveWorkspacePath(a.Workspace.Root, b.path)
		if err != nil {
			fmt.Fprintf(out, "  ✗ skipped %s: %v\n", b.path, err)
			a.logAuthorsAnswer("write", b.path, actionAll, "skipped: "+err.Error())
			continue
		}

		// Back up any existing file before overwriting.
		if _, statErr := os.Stat(abs); statErr == nil {
			bakPath := abs + ".bak." + ts
			if renErr := os.Rename(abs, bakPath); renErr != nil {
				fmt.Fprintf(out, "  ✗ skipped %s: cannot backup: %v\n", b.path, renErr)
				a.logAuthorsAnswer("write", b.path, actionAll, "skipped: cannot backup")
				continue
			}
			fmt.Fprintf(out, "  ↩ backed up %s → %s\n", b.path, filepath.Base(bakPath))
		}

		if err := func() error {
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return err
			}
			return os.WriteFile(abs, []byte(b.content), 0o644)
		}(); err != nil {
			fmt.Fprintf(out, "  ✗ %s: %v\n", b.path, err)
			a.logAuthorsAnswer("write", b.path, actionAll, "error: "+err.Error())
		} else {
			fmt.Fprintf(out, "  ✓ wrote %s (%d bytes)\n", b.path, len(b.content))
			a.logAuthorsAnswer("write", b.path, actionAll, "ok")
		}
	}
}
