package harvey

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// sessionLogVersion is the schema version written in session_start (DR-0039).
const sessionLogVersion = 1

// defaultSpillCap is the size above which a payload goes to a side file
// (DR-0039 item 4). It is a first guess, measured on the Pi before it is fixed.
const defaultSpillCap = 8 * 1024

// blobPreviewRunes is how much of a spilled payload stays in the event.
const blobPreviewRunes = 200

/** EventKind is the kind of one session-log event. The set is closed
 * (DR-0039 as amended by DR-0040); Valid reports whether a value belongs.
 *
 * Example:
 *   if KindUserInput.Valid() { ... }
 */
type EventKind string

// The kinds of a session-log event.
const (
	KindSessionStart  EventKind = "session_start"
	KindSessionEnd    EventKind = "session_end"
	KindUserInput     EventKind = "user_input"
	KindModelRequest  EventKind = "model_request"
	KindModelResponse EventKind = "model_response"
	KindToolCall      EventKind = "tool_call"
	KindToolResult    EventKind = "tool_result"
	KindKBCall        EventKind = "kb_call"
	KindKBResult      EventKind = "kb_result"
	KindInjection     EventKind = "injection"
	KindCompaction    EventKind = "compaction"
	KindPin           EventKind = "pin"
	KindUnpin         EventKind = "unpin"
	KindModelSwitch   EventKind = "model_switch"
	KindCommand       EventKind = "command"
	KindPermission    EventKind = "permission"
	KindFault         EventKind = "fault"
)

var eventKinds = []EventKind{
	KindSessionStart, KindSessionEnd, KindUserInput, KindModelRequest, KindModelResponse,
	KindToolCall, KindToolResult, KindKBCall, KindKBResult, KindInjection, KindCompaction,
	KindPin, KindUnpin, KindModelSwitch, KindCommand, KindPermission, KindFault,
}

/** EventKinds returns every valid event kind.
 *
 * Returns:
 *   []EventKind — a copy of the closed vocabulary.
 *
 * Example:
 *   for _, k := range EventKinds() { fmt.Println(k) }
 */
func EventKinds() []EventKind { return append([]EventKind(nil), eventKinds...) }

/** Valid reports whether k is one of the closed set of event kinds.
 *
 * Returns:
 *   bool — true for a known kind.
 *
 * Example:
 *   KindFault.Valid() // true
 *   EventKind("bogus").Valid() // false
 */
func (k EventKind) Valid() bool {
	for _, v := range eventKinds {
		if k == v {
			return true
		}
	}
	return false
}

/** EventLevel separates events every reader wants (info) from the extra detail
 * --debug adds (debug); rendering and kb ingest skip debug events by default
 * (DR-0038).
 *
 * Example:
 *   l.Append(KindFault, LevelDebug, data)
 */
type EventLevel string

// The levels of a session-log event.
const (
	LevelInfo  EventLevel = "info"
	LevelDebug EventLevel = "debug"
)

// Valid reports whether l is a known level.
func (l EventLevel) Valid() bool { return l == LevelInfo || l == LevelDebug }

/** Event is one line of a session log: the envelope of DR-0039. T is display
 * only; nothing that renders or assembles a prompt may read it.
 *
 * Fields:
 *   ID    (string)          — "<session>#<seq>", for example "s#0042" (DR-0038).
 *   T     (string)          — UTC time, RFC 3339.
 *   Kind  (EventKind)       — one of the closed vocabulary.
 *   Level (EventLevel)      — info or debug.
 *   Data  (json.RawMessage) — the kind's payload.
 *
 * Example:
 *   events, _, _ := ReadSessionLog(path)
 *   fmt.Println(events[0].ID, events[0].Kind)
 */
type Event struct {
	ID    string          `json:"id"`
	T     string          `json:"t"`
	Kind  EventKind       `json:"kind"`
	Level EventLevel      `json:"level"`
	Data  json.RawMessage `json:"data"`
}

/** Window is the context window harvey believes the model has, and how it
 * learned it (configured, probed or learned).
 *
 * Example:
 *   w := Window{N: 4096, Source: "configured"}
 */
type Window struct {
	N      int    `json:"n"`
	Source string `json:"source"`
}

/** SystemPrompt is a system-prompt version. The text is stored once per
 * version, in session_start (DR-0039); manifests name it by ID.
 *
 * Example:
 *   sp := SystemPrompt{ID: "sys-v1", Text: "You are Harvey."}
 */
type SystemPrompt struct {
	ID   string
	Text string
}

/** SessionStart holds what session_start records beyond the session id.
 *
 * Fields:
 *   HarveyVersion (string)       — harvey's version.
 *   User          (string)       — the author's name as the Fountain file shows it (upper case); "" is written as OPERATOR.
 *   Workspace     (string)       — the workspace root, stored as given.
 *   Backend, Model (string)      — the backend and model in use.
 *   System        (SystemPrompt) — the system prompt and its version id.
 *   Window        (Window)       — the context window.
 *   ReplayOf      (string)       — source session id for a replay; "" otherwise (DR-0040).
 *
 * Example:
 *   s := SessionStart{Backend: "ollama", Model: "llama3.2:3b"}
 */
type SessionStart struct {
	HarveyVersion string
	User          string
	Workspace     string
	Backend       string
	Model         string
	System        SystemPrompt
	Window        Window
	ReplayOf      string
}

/** SessionLogOptions configures OpenSessionLog.
 *
 * Fields:
 *   Dir      (string)       — directory for <session>.jsonl, normally agents/sessions.
 *   Session  (string)       — session id, the file stem; no "/", "\\" or "#".
 *   Start    (SessionStart) — the session_start payload.
 *   Debug    (bool)         — also write level-debug events (--debug).
 *   SpillCap (int)          — payload size above which content spills to a blob; 0 means the default (8 KB).
 *   Now      (func() time.Time) — clock for the display-only t field; nil means time.Now.
 *
 * Example:
 *   l, err := OpenSessionLog(SessionLogOptions{Dir: dir, Session: "20261030-1412-harvey"})
 */
type SessionLogOptions struct {
	Dir      string
	Session  string
	Start    SessionStart
	Debug    bool
	SpillCap int
	Now      func() time.Time
}

/** Content is a payload that is either inline or spilled to a blob. Inline
 * content with no flags marshals as a bare JSON string; anything else is an
 * object. Fields marked * in DR-0039 are Content values.
 *
 * Fields:
 *   Text    (string)   — the scrubbed text, when inline.
 *   Blob    (string)   — SHA-256 of the scrubbed text, when spilled.
 *   Bytes   (int)      — size of the scrubbed text, when spilled.
 *   Preview (string)   — the first characters of a spilled payload.
 *   Flags   ([]string) — sensitive words found but not redacted, for review.
 *
 * Example:
 *   c, _ := l.Content(output)
 *   l.Append(KindToolResult, LevelInfo, map[string]any{"output": c})
 */
type Content struct {
	Text    string
	Blob    string
	Bytes   int
	Preview string
	Flags   []string
}

type contentObject struct {
	Text    string   `json:"text,omitempty"`
	Blob    string   `json:"blob,omitempty"`
	Bytes   int      `json:"bytes,omitempty"`
	Preview string   `json:"preview,omitempty"`
	Flags   []string `json:"flags,omitempty"`
}

// MarshalJSON writes inline unflagged content as a bare string, otherwise an object.
func (c Content) MarshalJSON() ([]byte, error) {
	if c.Blob == "" && len(c.Flags) == 0 {
		return json.Marshal(c.Text)
	}
	return json.Marshal(contentObject{c.Text, c.Blob, c.Bytes, c.Preview, c.Flags})
}

// UnmarshalJSON reads either form.
func (c *Content) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		*c = Content{}
		return json.Unmarshal(b, &c.Text)
	}
	var o contentObject
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	*c = Content{o.Text, o.Blob, o.Bytes, o.Preview, o.Flags}
	return nil
}

/** SessionLog is the append-only JSONL record of one session (DR-0036,
 * DR-0038, DR-0039). It is safe for concurrent use. Every event is written
 * with one write and synced before Append returns, so a crash loses at most
 * the event in flight.
 *
 * Example:
 *   l, _ := OpenSessionLog(opts)
 *   id, _ := l.Append(KindUserInput, LevelInfo, map[string]any{"text": "hi"})
 *   _ = l.Close("quit")
 */
type SessionLog struct {
	mu        sync.Mutex
	f         *os.File
	dir       string
	session   string
	workspace string
	debug     bool
	spillCap  int
	now       func() time.Time
	path      string
	seq       int
	closed    bool
}

/** OpenSessionLog creates <Dir>/<Session>.jsonl and writes event 1,
 * session_start. It never overwrites: an existing log is a cant_create
 * error (exit 73). A bad session id is a usage error (exit 2).
 *
 * Parameters:
 *   opts (SessionLogOptions) — see the type.
 *
 * Returns:
 *   *SessionLog — the open log.
 *   error       — classified per workspace DR-0003.
 *
 * Example:
 *   l, err := OpenSessionLog(SessionLogOptions{Dir: dir, Session: "s1"})
 */
func OpenSessionLog(opts SessionLogOptions) (*SessionLog, error) {
	if err := checkSessionID(opts.Session); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return nil, ClassedAs(ClassCantCreate, err)
	}
	path := filepath.Join(opts.Dir, opts.Session+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, CantCreatef("session log %s already exists", path)
		}
		return nil, ClassedAs(ClassCantCreate, err)
	}
	l := &SessionLog{
		f: f, path: path, dir: opts.Dir, session: opts.Session, workspace: opts.Start.Workspace,
		debug: opts.Debug, spillCap: opts.SpillCap, now: opts.Now,
	}
	if l.spillCap <= 0 {
		l.spillCap = defaultSpillCap
	}
	if l.now == nil {
		l.now = time.Now
	}
	// The scrubber note is written after session_start, which is always event 1.
	sys, sysRep, err := l.content(opts.Start.System.Text, false)
	if err != nil {
		f.Close()
		return nil, err
	}
	sum := sha256.Sum256([]byte(opts.Start.System.Text))
	data := map[string]any{
		"v":              sessionLogVersion,
		"session":        opts.Session,
		"harvey_version": opts.Start.HarveyVersion,
		"user":           sessionUser(opts.Start.User),
		"workspace":      opts.Start.Workspace,
		"backend":        opts.Start.Backend,
		"model":          opts.Start.Model,
		"system":         map[string]any{"id": opts.Start.System.ID, "sha256": hex.EncodeToString(sum[:]), "text": sys},
		"window":         opts.Start.Window,
		"debug":          opts.Debug,
	}
	if opts.Start.ReplayOf != "" {
		data["replay_of"] = opts.Start.ReplayOf
	}
	if _, err := l.Append(KindSessionStart, LevelInfo, data); err != nil {
		f.Close()
		return nil, err
	}
	if err := l.noteRedactions(sysRep); err != nil {
		f.Close()
		return nil, err
	}
	return l, nil
}

func checkSessionID(s string) error {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\#`) {
		return Usagef("invalid session id %q: it must be non-empty and contain no '/', '\\' or '#'", s)
	}
	return nil
}

/** Append writes one event and returns its id. A level-debug event is dropped
 * (returning "" and consuming no sequence number) unless the log was opened
 * with Debug, so ids never have a gap. An unknown kind or level, or an append
 * after Close, is an error.
 *
 * Parameters:
 *   kind  (EventKind)  — the event kind.
 *   level (EventLevel) — info or debug.
 *   data  (any)        — the payload; marshalled to JSON.
 *
 * Returns:
 *   string — the event id, "<session>#<seq>".
 *   error  — on a bad kind or level, a closed log, or a failed write.
 *
 * Example:
 *   id, err := l.Append(KindUserInput, LevelInfo, map[string]any{"text": "hi"})
 */
func (l *SessionLog) Append(kind EventKind, level EventLevel, data any) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.appendLocked(kind, level, data)
}

func (l *SessionLog) appendLocked(kind EventKind, level EventLevel, data any) (string, error) {
	if !kind.Valid() {
		return "", Usagef("unknown event kind %q", kind)
	}
	if !level.Valid() {
		return "", Usagef("unknown event level %q", level)
	}
	if l.closed {
		return "", Negativef("session log %s is closed", l.session)
	}
	if level == LevelDebug && !l.debug {
		return "", nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return "", Dataf("event payload: %w", err)
	}
	id := fmt.Sprintf("%s#%04d", l.session, l.seq+1)
	line, err := json.Marshal(Event{ID: id, T: l.now().UTC().Format(time.RFC3339), Kind: kind, Level: level, Data: raw})
	if err != nil {
		return "", Dataf("event: %w", err)
	}
	if _, err := l.f.Write(append(line, '\n')); err != nil {
		return "", ClassedAs(ClassIO, err)
	}
	if err := l.f.Sync(); err != nil {
		return "", ClassedAs(ClassIO, err)
	}
	l.seq++
	return id, nil
}

/** Content scrubs text (ScrubEvent), then keeps it inline or, when it is over
 * the spill cap, writes it once to <session>.blobs/<sha256> and returns a
 * reference. The blob is on disk and synced before Content returns, so an
 * event that names it can never point at a missing file. The hash is of the
 * scrubbed text. With Debug on, a redaction adds a level-debug fault note
 * naming the classes and counts, never the values.
 *
 * Parameters:
 *   text (string) — the raw payload text.
 *
 * Returns:
 *   Content — inline or a blob reference, with any flags.
 *   error   — if the blob cannot be written.
 *
 * Example:
 *   c, err := l.Content(fileText)
 *   l.Append(KindToolResult, LevelInfo, map[string]any{"output": c})
 */
func (l *SessionLog) Content(text string) (Content, error) {
	c, rep, err := l.content(text, false)
	if err != nil {
		return Content{}, err
	}
	return c, l.noteRedactions(rep)
}

// content scrubs text and spills it if large, without writing the scrubber
// note, so OpenSessionLog can keep session_start first.
func (l *SessionLog) content(text string, forceBlob bool) (Content, ScrubReport, error) {
	scrubbed, rep := ScrubEvent(text, l.workspace)
	c := Content{Text: scrubbed, Flags: rep.Flags}
	if forceBlob || len(scrubbed) > l.spillCap {
		sum := sha256.Sum256([]byte(scrubbed))
		c = Content{
			Blob: hex.EncodeToString(sum[:]), Bytes: len(scrubbed),
			Preview: preview(scrubbed), Flags: rep.Flags,
		}
		if err := l.writeBlob(c.Blob, scrubbed); err != nil {
			return Content{}, rep, err
		}
	}
	return c, rep, nil
}

// noteRedactions appends the level-debug scrubber note for a report with
// redactions: the classes and counts, never the values.
func (l *SessionLog) noteRedactions(rep ScrubReport) error {
	note := rep.String()
	if note == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err := l.appendLocked(KindFault, LevelDebug, map[string]string{
		"source": "scrubber", "message": "redacted: " + note, "severity": "warning",
	})
	return err
}

/** ContentBlob is Content that always goes to a blob, however small: for a
 * payload that is file content or a whole prompt, which the log should hold
 * once and by hash (DR-0040 item 2). Scrubbing, hashing and the write order
 * are the same as Content.
 *
 * Parameters:
 *   text (string) — the raw payload text.
 *
 * Returns:
 *   Content — a blob reference with a preview.
 *   error   — if the blob cannot be written.
 *
 * Example:
 *   c, err := l.ContentBlob(promptJSON)
 */
func (l *SessionLog) ContentBlob(text string) (Content, error) {
	c, rep, err := l.content(text, true)
	if err != nil {
		return Content{}, err
	}
	return c, l.noteRedactions(rep)
}

/** Path returns the file the log is written to.
 *
 * Returns:
 *   string — <dir>/<session>.jsonl.
 *
 * Example:
 *   fmt.Println(l.Path())
 */
func (l *SessionLog) Path() string { return l.path }

func preview(s string) string {
	if utf8.RuneCountInString(s) <= blobPreviewRunes {
		return s
	}
	n := 0
	for i := range s {
		if n == blobPreviewRunes {
			return s[:i]
		}
		n++
	}
	return s
}

func (l *SessionLog) writeBlob(name, text string) error {
	dir := filepath.Join(l.dir, l.session+".blobs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ClassedAs(ClassCantCreate, err)
	}
	final := filepath.Join(dir, name)
	if _, err := os.Stat(final); err == nil {
		return nil
	}
	tmp, err := os.CreateTemp(dir, ".blob-*")
	if err != nil {
		return ClassedAs(ClassCantCreate, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return ClassedAs(ClassIO, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return ClassedAs(ClassIO, err)
	}
	if err := tmp.Close(); err != nil {
		return ClassedAs(ClassIO, err)
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		return ClassedAs(ClassIO, err)
	}
	return nil
}

/** Close writes session_end (reason, and the sequence number of that final
 * event) and seals the log. A second Close does nothing.
 *
 * Parameters:
 *   reason (string) — quit, crash_recovered or error.
 *
 * Returns:
 *   error — if the final event or the file close fails.
 *
 * Example:
 *   defer l.Close("quit")
 */
func (l *SessionLog) Close(reason string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	_, err := l.appendLocked(KindSessionEnd, LevelInfo, map[string]any{"reason": reason, "events": l.seq + 1})
	l.closed = true
	if cerr := l.f.Close(); err == nil && cerr != nil {
		err = ClassedAs(ClassIO, cerr)
	}
	return err
}

/** ReadSessionLog reads and validates a session log. Every line must be an
 * event of a known kind and level; the first must be session_start; ids must
 * be "<session>#<seq>" for one session, gapless from 1. A malformed interior
 * line, an unknown kind, a gap or a foreign session id is a data error
 * (exit 65). A torn final line (a crash mid-write) is tolerated and reported.
 *
 * Parameters:
 *   path (string) — the .jsonl file.
 *
 * Returns:
 *   []Event — the complete events.
 *   bool    — true when a torn final line was ignored.
 *   error   — no_input if the file is missing, data if it is invalid.
 *
 * Example:
 *   events, torn, err := ReadSessionLog("agents/sessions/s1.jsonl")
 */
func ReadSessionLog(path string) ([]Event, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, ClassedAs(ClassNoInput, err)
	}
	defer f.Close()
	var events []Event
	var lines [][]byte
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			lines = append(lines, append([]byte(nil), line...))
		}
		if err != nil {
			break
		}
	}
	torn := false
	session := ""
	for i, raw := range lines {
		last := i == len(lines)-1
		complete := bytes.HasSuffix(raw, []byte("\n"))
		var e Event
		if err := json.Unmarshal(bytes.TrimSpace(raw), &e); err != nil {
			if last && !complete {
				torn = true
				break
			}
			return nil, false, Dataf("%s line %d: %v", path, i+1, err)
		}
		if !e.Kind.Valid() {
			return nil, false, Dataf("%s line %d: unknown event kind %q", path, i+1, e.Kind)
		}
		if !e.Level.Valid() {
			return nil, false, Dataf("%s line %d: unknown event level %q", path, i+1, e.Level)
		}
		hash := strings.LastIndexByte(e.ID, '#')
		seq, serr := 0, error(nil)
		if hash > 0 {
			seq, serr = strconv.Atoi(e.ID[hash+1:])
		}
		if hash <= 0 || serr != nil {
			return nil, false, Dataf("%s line %d: bad event id %q", path, i+1, e.ID)
		}
		if i == 0 {
			if e.Kind != KindSessionStart {
				return nil, false, Dataf("%s: the first event is %s, not session_start", path, e.Kind)
			}
			session = e.ID[:hash]
		}
		if e.ID[:hash] != session {
			return nil, false, Dataf("%s line %d: event %q belongs to another session than %q", path, i+1, e.ID, session)
		}
		if seq != len(events)+1 {
			return nil, false, Dataf("%s line %d: id %q breaks the sequence (expected #%04d); an event is missing", path, i+1, e.ID, len(events)+1)
		}
		events = append(events, e)
	}
	return events, torn, nil
}

// sessionUser is the name written to session_start: the given name, upper
// case, or OPERATOR. It is recorded so that a rendering needs no environment.
func sessionUser(name string) string {
	if name = strings.ToUpper(strings.TrimSpace(name)); name != "" {
		return name
	}
	return "OPERATOR"
}
