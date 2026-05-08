package harvey

// recorder.go — writes Harvey sessions as Fountain screenplay source files.
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

// Recorder writes a Harvey session to a Fountain screenplay source file,
// using append-writes so in-progress sessions survive crashes.
//
// Three characters appear in the script:
//   - USER    — the human operator (name from $USER env, ALL CAPS)
//   - HARVEY  — the agent program
//   - MODEL   — the LLM backend (model name, ALL CAPS, e.g. GEMMA4)
//
// Example:
//
//	r, err := NewRecorder("session.fountain", "Ollama (gemma4:latest)", "/home/user/proj")
//	if err != nil { log.Fatal(err) }
//	defer r.Close()
//	r.RecordTurnWithStats("Hi", "Hello!", stats, []string{"Ollama (gemma4:latest)"}, "")
//	r.StartAgentScene("Harvey proposes to write 1 file.")
//	r.RecordAgentAction("write", "hello.bash", "yes", "ok")
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
	for _, elem := range []*fountain.Element{
		{Type: fountain.TitlePageType, Name: "Title", Content: "Harvey Session"},
		{Type: fountain.TitlePageType, Name: "Credit", Content: "Recorded by Harvey"},
		{Type: fountain.TitlePageType, Name: "Author", Content: user},
		{Type: fountain.TitlePageType, Name: "Date", Content: now.Format("2006-01-02 15:04:05")},
		{Type: fountain.TitlePageType, Name: "Draft date", Content: now.Format("2006-01-02")},
		{Type: fountain.TitlePageType, Name: "Model", Content: r.modelName},
	} {
		fmt.Fprintln(f, fountainSrc(elem))
	}

	// Blank line ends the title page, then opening transition.
	fmt.Fprintln(f)
	r.writeTransition("FADE IN:")
	return r, nil
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

// RecordTurn appends a chat turn without stats. See RecordTurnWithStats.
//
// Parameters:
//
//	userInput   (string) — the user's raw input text.
//	harveyReply (string) — the LLM's complete response text.
//
// Returns:
//
//	error — if the write fails.
//
// Example:
//
//	err := r.RecordTurn("What is 2+2?", "2 + 2 = 4.")
func (r *Recorder) RecordTurn(userInput, harveyReply string) error {
	return r.RecordTurnWithStats(userInput, harveyReply, ChatStats{}, nil, "")
}

// RecordTurnWithStats appends a full chat turn as a Fountain scene.
//
// Scene structure:
//
//	INT. HARVEY AND {USER} TALKING {TIMESTAMP}
//
//	Harvey and {USER} are in chat mode. Model: {MODEL}. Workspace: {ws}.
//
//	{USER}
//	(user's input)
//
//	HARVEY
//	Forwarding to {MODEL}.
//
//	{MODEL}
//	(LLM reply)
//
//	Routing to llama3.1:8b              ← routeStep action block (omitted when empty)
//
//	{models} · {reply} reply + {ctx} ctx · {elapsed} · {tok/s} tok/s
//
// Parameters:
//
//	userInput   (string)    — the user's raw input text.
//	harveyReply (string)    — the LLM's complete response text.
//	stats       (ChatStats) — LLM call stats; omitted when empty.
//	models      ([]string)  — ordered model names that handled the turn; nil for single-model sessions.
//	routeStep   (string)    — routing decision text, e.g. "Routing to llama3.1:8b" or
//	                          "Answered by llama3.2:1b"; empty when routing is disabled.
//
// Returns:
//
//	error — if the write fails.
//
// Example:
//
//	err := r.RecordTurnWithStats("Hello", "Hi!", stats, []string{"llama3.2:1b", "Ollama (llama3.1:8b)"}, "Routing to llama3.1:8b")
func (r *Recorder) RecordTurnWithStats(userInput, harveyReply string, stats ChatStats, models []string, routeStep string) error {
	ts := time.Now().Format("2006-01-02 15:04:05")

	r.writeSceneHeading(fmt.Sprintf("INT. HARVEY AND %s TALKING %s", r.userName, ts))
	r.writeAction(fmt.Sprintf(
		"Harvey and %s are in chat mode. Model: %s. Workspace: %s.",
		r.userName, r.modelName, r.workspace,
	))
	r.writeDialogue(r.userName, "", userInput)
	r.writeDialogue("HARVEY", "", fmt.Sprintf("Forwarding to %s.", r.modelName))
	r.writeDialogue(r.modelName, "", harveyReply)
	if routeStep != "" {
		r.writeAction(routeStep)
	}
	if statText := stats.FormatWithModels(models); statText != "" {
		r.writeAction(statText)
	}
	return nil
}

// StartAgentScene opens a new INT. AGENT MODE scene with an action block
// describing the proposed actions. Call this before the first RecordAgentAction
// of a group.
//
// Parameters:
//
//	description (string) — one-sentence description of what Harvey proposes to do.
//
// Returns:
//
//	error — if the write fails.
//
// Example:
//
//	r.StartAgentScene("Harvey proposes to write 2 file(s) to the workspace.")
func (r *Recorder) StartAgentScene(description string) error {
	ts := time.Now().Format("2006-01-02 15:04:05")
	r.writeSceneHeading(fmt.Sprintf("INT. AGENT MODE %s", ts))
	r.writeDialogue("HARVEY", "", description)
	return nil
}

// RecordAgentAction records one agent action as HARVEY proposing, USER
// confirming, and a note with the outcome.
//
// Parameters:
//
//	kind       (string) — "write", "run", etc.
//	target     (string) — file path or command line.
//	userChoice (string) — "yes", "no", "all", or "quit".
//	outcome    (string) — "ok", "skipped", "aborted", or "error: <msg>".
//
// Returns:
//
//	error — if the write fails.
//
// Example:
//
//	r.RecordAgentAction("write", "testout/hello.bash", "yes", "ok")
func (r *Recorder) RecordAgentAction(kind, target, userChoice, outcome string) error {
	// HARVEY proposes
	proposal := fmt.Sprintf("%s %s", strings.Title(kind), target) //nolint:staticcheck
	if kind == "write" {
		proposal = fmt.Sprintf("Write %s?", target)
	} else if kind == "run" {
		proposal = fmt.Sprintf("Run: %s?", target)
	}
	r.writeDialogue("HARVEY", "", proposal)

	// USER responds
	r.writeDialogue(r.userName, "", userChoice)

	// Outcome as a Fountain note
	r.writeNote(fmt.Sprintf("%s: %s — %s", kind, target, outcome))
	return nil
}

// RecordShellCommand appends a shell-execution scene to the recording.
//
// Scene structure:
//
//	INT. SHELL {TIMESTAMP}
//
//	{USER}
//	! cmdLine                              ← dialogue (the command the user ran)
//
//	SHELL
//	(output text)                          ← dialogue (combined stdout+stderr)
//
//	[[shell: cmdLine — exit N]]            ← Fountain note with exit code
//
// Parameters:
//
//	cmdLine  (string) — the shell command as typed by the user (without "!").
//	output   (string) — combined stdout+stderr captured from the command.
//	exitCode (int)    — process exit code; 0 means success.
//
// Returns:
//
//	error — if the write fails.
//
// Example:
//
//	r.RecordShellCommand("git status", "On branch main\n...", 0)
func (r *Recorder) RecordShellCommand(cmdLine, output string, exitCode int) error {
	ts := time.Now().Format("2006-01-02 15:04:05")
	r.writeSceneHeading(fmt.Sprintf("INT. SHELL %s", ts))
	r.writeDialogue(r.userName, "", "! "+cmdLine)
	if output != "" {
		r.writeDialogue("SHELL", "", output)
	}
	r.writeNote(fmt.Sprintf("shell: %s — exit %d", cmdLine, exitCode))
	return nil
}

// RecordSkillLoad appends a skill-activation scene to the recording.
//
// Scene structure:
//
//	INT. SKILL {NAME} {TIMESTAMP}
//
//	Harvey executes the {name} skill.      ← action (Harvey's action)
//
//	{description}                          ← action (what the skill does)
//
//	{NAME}
//	{body}                                 ← dialogue (skill's output/instructions)
//
// The skill name is uppercased to form the Fountain character name, e.g.
// "go-review" → "GO-REVIEW". The description is rendered as a stage-direction
// action block so it reads as the skill taking action. The body is the skill's
// dialogue — what it delivers to Harvey.
//
// Parameters:
//
//	name        (string) — skill identifier, e.g. "go-review".
//	description (string) — one-line description from the skill's frontmatter.
//	body        (string) — full markdown body of the SKILL.md file.
//
// Returns:
//
//	error — if the write fails.
//
// Example:
//
//	r.RecordSkillLoad("go-review", "Review Go source code for quality issues.", body)
func (r *Recorder) RecordSkillLoad(name, description, body string) error {
	ts := time.Now().Format("2006-01-02 15:04:05")
	skillChar := strings.ToUpper(name)

	r.writeSceneHeading(fmt.Sprintf("INT. SKILL %s %s", skillChar, ts))

	// Harvey's action: executing the skill.
	r.writeAction(fmt.Sprintf("Harvey executes the %s skill.", name))

	// Skill's action: what the skill does, as a stage direction.
	if description != "" {
		r.writeAction(description)
	}

	// Skill's dialogue: the instructions/output it delivers.
	if body != "" {
		r.writeDialogue(skillChar, "", body)
	}

	return nil
}

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
//	path := DefaultSessionPath("/home/user/project/harvey/sessions")
//	// "/home/user/project/harvey/sessions/harvey-session-20260415-142300.spmd"
func DefaultSessionPath(dir string) string {
	ts := time.Now().Format("20060102-150405")
	return fmt.Sprintf("%s/harvey-session-%s.spmd", dir, ts)
}

// ─── private write helpers ────────────────────────────────────────────────────

// writeSceneHeading appends a scene heading preceded by two blank lines.
func (r *Recorder) writeSceneHeading(content string) {
	elem := &fountain.Element{Type: fountain.SceneHeadingType, Content: content}
	fmt.Fprintf(r.f, "\n\n%s\n\n", fountainSrc(elem))
}

// writeAction appends an action (stage-direction) block followed by a blank line.
func (r *Recorder) writeAction(content string) {
	elem := &fountain.Element{Type: fountain.ActionType, Content: content}
	fmt.Fprintf(r.f, "%s\n\n", fountainSrc(elem))
}

// writeDialogue appends a CHARACTER / (parenthetical) / dialogue block.
// parenthetical and text may each be empty — empty strings are omitted.
func (r *Recorder) writeDialogue(character, parenthetical, text string) {
	charElem := &fountain.Element{Type: fountain.CharacterType, Content: character}
	fmt.Fprintln(r.f, fountainSrc(charElem))
	if parenthetical != "" {
		paren := &fountain.Element{Type: fountain.ParentheticalType, Content: parenthetical}
		fmt.Fprintln(r.f, fountainSrc(paren))
	}
	if text != "" {
		diag := &fountain.Element{Type: fountain.DialogueType, Content: text}
		fmt.Fprintln(r.f, fountainSrc(diag))
	}
	fmt.Fprintln(r.f) // blank line ends the dialogue block
}

// writeNote appends a Fountain note ([[...]]) followed by a blank line.
func (r *Recorder) writeNote(content string) {
	elem := &fountain.Element{Type: fountain.NoteType, Content: content}
	fmt.Fprintf(r.f, "%s\n\n", fountainSrc(elem))
}

// writeTransition appends a transition line preceded by a blank line.
func (r *Recorder) writeTransition(content string) {
	elem := &fountain.Element{Type: fountain.TransitionType, Content: content}
	fmt.Fprintf(r.f, "\n%s\n", fountainSrc(elem))
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
