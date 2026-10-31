package harvey

// recorder.go — the session's Fountain file while it is open, and the Fountain
// source writers the renderer uses.
//
// During a session the file holds only the title page (DR-0038): the scenes are
// rendered from the session log at close (sessionrender.go, sessionfile.go).
// The scene structure below is what the renderer produces.
//
// The fountain module (github.com/rsdoiel/fountain) provides Element and its
// type constants as the structural vocabulary. Because Element.String() is a
// display renderer (adds indentation/word-wrap for print), we use our own
// fountainSrc() helper to emit raw Fountain source syntax that can be round-
// tripped by fountain.ParseFile().
//
// Scene structure
// ──────────────
//   INT. HARVEY AND {USER} TALKING {TIMESTAMP}   ← one per chat turn
//
//       Harvey and {USER} are in chat mode. …    ← action block (state)
//
//       {USER}
//       user's input text
//
//       HARVEY
//       Forwarding to {MODEL}.
//
//       {MODEL}
//       LLM reply text
//
//       [[stats: …]]                              ← Fountain note
//
//   INT. AGENT MODE {TIMESTAMP}                  ← one per agent-action group
//
//       HARVEY
//       Harvey proposes to write 2 file(s)…      ← dialogue (Harvey speaking)
//
//       HARVEY
//       Write testout/hello.bash?
//
//       {USER}
//       yes
//
//       [[write: testout/hello.bash — ok]]

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rsdoiel/fountain"
)

// fountainSrc converts an Element to raw Fountain source text (no indentation).
// This is intentionally different from Element.String(), which is a display
// renderer and adds screenplay indentation unsuitable for source files.
func fountainSrc(elem *fountain.Element) string {
	switch elem.Type {
	case fountain.TitlePageType:
		return elem.Name + ": " + elem.Content
	case fountain.SceneHeadingType:
		return strings.ToUpper(strings.TrimSpace(elem.Content))
	case fountain.ActionType:
		return strings.TrimSpace(elem.Content)
	case fountain.CharacterType:
		return strings.ToUpper(strings.TrimSpace(elem.Content))
	case fountain.ParentheticalType:
		s := strings.TrimSpace(elem.Content)
		if !strings.HasPrefix(s, "(") {
			s = "(" + s + ")"
		}
		return s
	case fountain.DialogueType:
		return strings.TrimSpace(elem.Content)
	case fountain.NoteType:
		return "[[" + strings.TrimSpace(elem.Content) + "]]"
	case fountain.TransitionType:
		return strings.TrimSpace(elem.Content)
	default:
		return strings.TrimSpace(elem.Content)
	}
}

// ToolCallRecord holds the name, raw JSON arguments, result status, and
// optional character attribution of a single tool invocation, for use in
// session recording.
type ToolCallRecord struct {
	Name      string // tool function name, e.g. "read_file"
	Args      string // raw JSON arguments string
	Result    string // "ok" or "error: <first line>" — empty defaults to "ok"
	Character string // ALL-CAPS model name when attributed; empty = HARVEY (default)
}

// formatToolCallNote returns the content of a [[...]] Fountain note for a
// tool call, e.g. "tool: read_file({"path":"foo.go"}) — ok".
// When tc.Character is set, the prefix becomes "CHARACTER.tool: ...".
func formatToolCallNote(tc ToolCallRecord) string {
	nameArgs := tc.Name + "()"
	if tc.Args != "" && tc.Args != "{}" && tc.Args != "null" {
		nameArgs = tc.Name + "(" + tc.Args + ")"
	}
	result := tc.Result
	if result == "" {
		result = "ok"
	}
	if tc.Character != "" {
		return tc.Character + ".tool: " + nameArgs + " — " + result
	}
	return "tool: " + nameArgs + " — " + result
}

// Recorder holds the Fountain file of a session open from its title page until
// the session ends, when the file is rendered from the session log over it.
// It writes nothing else; the session log is the record.
//
// Example:
//
//	r, err := NewRecorder("session.spmd", "ollama (gemma4:latest)", "/home/user/proj")
//	if err != nil { log.Fatal(err) }
//	defer r.Close()
type Recorder struct {
	f         *os.File
	path      string
	userName  string // from $USER, ALL CAPS
	modelName string // LLM name, ALL CAPS
	workspace string
}

// NewRecorder creates (or truncates) the Fountain file at path, writes the
// title page and FADE IN:, and returns a ready-to-use Recorder.
//
// Parameters:
//
//	path      (string) — .fountain file path for the session script.
//	model     (string) — backend string from Client.Name(), e.g. "Ollama (gemma4:latest)".
//	workspace (string) — absolute path of the Harvey workspace root.
//
// Returns:
//
//	*Recorder — open recorder; caller must call Close() when done.
//	error     — if the file cannot be created or the header cannot be written.
//
// Example:
//
//	r, err := NewRecorder("harvey-session.fountain", "Ollama (gemma4:latest)", "/home/user/proj")
func NewRecorder(path, model, workspace string) (*Recorder, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("recorder: cannot create %s: %w", path, err)
	}

	user := strings.ToUpper(os.Getenv("USER"))
	if user == "" {
		user = "OPERATOR"
	}

	r := &Recorder{
		f:         f,
		path:      path,
		userName:  user,
		modelName: extractModelName(model),
		workspace: workspace,
	}

	// Title page (key: value pairs, no blank lines between them).
	now := time.Now()
	backend := backendFromClientName(model)
	for _, elem := range []*fountain.Element{
		{Type: fountain.TitlePageType, Name: "Title", Content: "Harvey Session"},
		{Type: fountain.TitlePageType, Name: "Credit", Content: "Recorded by Harvey"},
		{Type: fountain.TitlePageType, Name: "Author", Content: user},
		{Type: fountain.TitlePageType, Name: "Date", Content: now.Format("2006-01-02 15:04:05")},
		{Type: fountain.TitlePageType, Name: "Draft date", Content: now.Format("2006-01-02")},
		{Type: fountain.TitlePageType, Name: "Model", Content: r.modelName + " (" + backend + ")"},
		{Type: fountain.TitlePageType, Name: "Backend", Content: backend},
	} {
		fmt.Fprintln(f, fountainSrc(elem))
	}

	// Blank line ends the title page, then opening transition.
	fmt.Fprintln(f)
	r.writeTransition("FADE IN:")
	return r, nil
}

// backendFromClientName extracts the provider prefix from a client Name()
// string of the form "provider (model)", returning it lowercased.
// Returns "unknown" if the format is not recognised.
func backendFromClientName(name string) string {
	if i := strings.Index(name, " ("); i > 0 {
		return strings.ToLower(name[:i])
	}
	return "unknown"
}

// Path returns the file path this recorder is writing to.
//
// Returns:
//
//	string — the file path passed to NewRecorder.
//
// Example:
//
//	fmt.Println(r.Path()) // "/home/user/harvey-session.fountain"
func (r *Recorder) Path() string { return r.path }

// Close writes THE END. and closes the session file.
// After Close, the Recorder must not be used.
//
// Returns:
//
//	error — if the file cannot be closed.
//
// Example:
//
//	defer r.Close()
func (r *Recorder) Close() error {
	r.writeTransition("THE END.")
	return r.f.Close()
}

/** Rename renames the session file on disk to newPath without ending the
 * session. The recorder continues appending to the renamed file.
 * On failure it attempts to reopen the original file so recording can continue.
 *
 * Parameters:
 *   newPath (string) — absolute destination path for the renamed file.
 *
 * Returns:
 *   error — if sync, close, rename, or reopen fails.
 *
 * Example:
 *   err := r.Rename("/sessions/my-feature.spmd")
 */
func (r *Recorder) Rename(newPath string) error {
	if err := r.f.Sync(); err != nil {
		return err
	}
	oldPath := r.path
	if err := r.f.Close(); err != nil {
		return err
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		// Reopen at original path so recording can continue.
		if f, openErr := os.OpenFile(oldPath, os.O_WRONLY|os.O_APPEND, 0o644); openErr == nil {
			r.f = f
		}
		return err
	}
	f, err := os.OpenFile(newPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	r.f = f
	r.path = newPath
	return nil
}

// DefaultSessionPath returns a timestamped default filename for a session
// script in the given directory, using the .spmd extension.
//
// Parameters:
//
//	dir (string) — directory in which to place the file.
//
// Returns:
//
//	string — path of the form "<dir>/harvey-session-YYYYMMDD-HHMMSS.spmd".
//
// Example:
//
//	path := DefaultSessionPath("/home/user/project/agents/sessions")
//	// "/home/user/project/agents/sessions/harvey-session-20260415-142300.spmd"
func DefaultSessionPath(dir string) string {
	ts := time.Now().Format("20060102-150405")
	return fmt.Sprintf("%s/harvey-session-%s.spmd", dir, ts)
}

// ─── private write helpers ────────────────────────────────────────────────────

func (r *Recorder) writeTransition(content string) { fountainOut{r.f}.transition(content) }

/** fountainOut writes Fountain elements to w with the blank-line conventions
 * of a session file. The live recorder and the renderer (sessionrender.go)
 * both write through it, so the two cannot drift apart.
 *
 * Example:
 *   out := fountainOut{os.Stdout}
 *   out.dialogue("HARVEY", "", "Hello")
 */
type fountainOut struct{ w io.Writer }

func (o fountainOut) sceneHeading(content string) {
	elem := &fountain.Element{Type: fountain.SceneHeadingType, Content: content}
	fmt.Fprintf(o.w, "\n\n%s\n\n", fountainSrc(elem))
}

func (o fountainOut) action(content string) {
	elem := &fountain.Element{Type: fountain.ActionType, Content: content}
	fmt.Fprintf(o.w, "%s\n\n", fountainSrc(elem))
}

func (o fountainOut) dialogue(character, parenthetical, text string) {
	charElem := &fountain.Element{Type: fountain.CharacterType, Content: character}
	fmt.Fprintln(o.w, fountainSrc(charElem))
	if parenthetical != "" {
		paren := &fountain.Element{Type: fountain.ParentheticalType, Content: parenthetical}
		fmt.Fprintln(o.w, fountainSrc(paren))
	}
	if text != "" {
		diag := &fountain.Element{Type: fountain.DialogueType, Content: text}
		fmt.Fprintln(o.w, fountainSrc(diag))
	}
	fmt.Fprintln(o.w) // blank line ends the dialogue block
}

func (o fountainOut) note(content string) {
	elem := &fountain.Element{Type: fountain.NoteType, Content: content}
	fmt.Fprintf(o.w, "%s\n\n", fountainSrc(elem))
}

func (o fountainOut) transition(content string) {
	elem := &fountain.Element{Type: fountain.TransitionType, Content: content}
	fmt.Fprintf(o.w, "\n%s\n", fountainSrc(elem))
}

// extractModelName extracts a clean ALL-CAPS model identifier from a backend
// name string returned by Client.Name().
//
// Examples:
//
//	"Ollama (gemma4:latest)"                 → "GEMMA4"
//	"Ollama (MichelRosselli/apertus:latest)" → "APERTUS"
//	"anthropic (claude-sonnet-4-20250514)"   → "CLAUDE-SONNET-4-20250514"
//	"none"                                   → "MODEL"
func extractModelName(backend string) string {
	// Extract content inside parentheses, if present.
	if i := strings.Index(backend, "("); i >= 0 {
		backend = strings.TrimSuffix(strings.TrimSpace(backend[i+1:]), ")")
	}
	// Strip version tag: "gemma4:latest" → "gemma4"
	if i := strings.Index(backend, ":"); i >= 0 {
		backend = backend[:i]
	}
	// Strip namespace prefix: "MichelRosselli/apertus" → "apertus"
	if i := strings.LastIndex(backend, "/"); i >= 0 {
		backend = backend[i+1:]
	}
	name := strings.ToUpper(strings.TrimSpace(backend))
	if name == "" {
		return "MODEL"
	}
	return name
}
