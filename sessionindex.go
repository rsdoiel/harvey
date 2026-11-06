package harvey

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
)

// sessionindex.go — the in-process index over a session log (plan L6, DR-0036).
//
// The assembler needs per-turn lookups and spawning kb for each is rejected, so
// harvey keeps a small index of its own log: for every event its kind, level,
// turn and place in the file (not its payload), plus the pins and compactions
// derived from the pin, unpin and compaction events. The index is updated as
// events are appended and can be rebuilt from the file; the two must agree.
//
// Contract with the events that do not exist yet (to be written by L7 and L8):
//   pin         data {"text": ...}      the pin is the event itself, named by its id
//   unpin       data {"pin": "<id>"}    removes that pin
//   compaction  data {"sources": ["<id>", ...]}   the events the summary stands for

/** IndexEntry is what the index keeps of one event: enough to find it and to
 * answer which turn it belongs to, not its payload.
 *
 * Fields:
 *   ID    (string)     — the event id, "<session>#<seq>".
 *   Kind  (EventKind)  — the event kind.
 *   Level (EventLevel) — info or debug.
 *   Turn  (int)        — the number of user_input events up to and including this
 *                        event; 0 before the first.
 *   Off   (int64)      — byte offset of the event's line in the log file.
 *   Len   (int)        — length of the line, without its newline.
 *
 * Example:
 *   e, _ := idx.Lookup("s#0007")
 *   fmt.Println(e.Kind, e.Turn)
 */
type IndexEntry struct {
	ID    string
	Kind  EventKind
	Level EventLevel
	Turn  int
	Off   int64
	Len   int
}

/** CompactionRef is a compaction event and the events it summarises.
 *
 * Example:
 *   for _, c := range idx.Compactions() { fmt.Println(c.ID, len(c.Sources)) }
 */
type CompactionRef struct {
	ID      string
	Sources []string
}

/** SessionIndex indexes one session log. It is safe for concurrent use. The
 * payloads stay in the file; Event reads one back by its place.
 *
 * Example:
 *   idx, torn, err := BuildSessionIndex("agents/sessions/s.jsonl")
 *   last := idx.LastTurns(3)
 */
type SessionIndex struct {
	mu          sync.RWMutex
	path        string
	entries     []IndexEntry
	byID        map[string]int
	turns       []int // entries index of each user_input, in order
	pins        []string
	compactions []CompactionRef
	end         int64 // offset after the last indexed line
}

// newSessionIndex returns an empty index over the file at path.
func newSessionIndex(path string) *SessionIndex {
	return &SessionIndex{path: path, byID: map[string]int{}}
}

// add indexes one event whose line starts at off and is n bytes long.
func (x *SessionIndex) add(e Event, off int64, n int) {
	x.mu.Lock()
	defer x.mu.Unlock()
	i := len(x.entries)
	if e.Kind == KindUserInput && e.Level != LevelDebug {
		x.turns = append(x.turns, i)
	}
	x.entries = append(x.entries, IndexEntry{ID: e.ID, Kind: e.Kind, Level: e.Level, Turn: len(x.turns), Off: off, Len: n})
	x.byID[e.ID] = i
	x.end = off + int64(n) + 1
	switch e.Kind {
	case KindPin:
		x.pins = append(x.pins, e.ID)
	case KindUnpin:
		var d struct {
			Pin string `json:"pin"`
		}
		_ = json.Unmarshal(e.Data, &d)
		for j, p := range x.pins {
			if p == d.Pin {
				x.pins = append(x.pins[:j:j], x.pins[j+1:]...)
				break
			}
		}
	case KindCompaction:
		var d struct {
			Sources []string `json:"sources"`
		}
		_ = json.Unmarshal(e.Data, &d)
		x.compactions = append(x.compactions, CompactionRef{ID: e.ID, Sources: d.Sources})
	}
}

/** BuildSessionIndex reads a session log and indexes it. It is the resume path
 * and the check on the live index. A torn final line is ignored and reported,
 * as ReadSessionLog does; it does not check ids and sequence, which the log
 * writer guarantees and ReadSessionLog verifies.
 *
 * Parameters:
 *   path (string) — the .jsonl file.
 *
 * Returns:
 *   *SessionIndex — the index.
 *   bool          — true when a torn final line was ignored.
 *   error         — no_input (66) if the file is missing, data (65) for a bad line.
 *
 * Example:
 *   idx, torn, err := BuildSessionIndex(path)
 */
func BuildSessionIndex(path string) (*SessionIndex, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, ClassedAs(ClassNoInput, err)
	}
	defer f.Close()
	x := newSessionIndex(path)
	r := bufio.NewReaderSize(f, 1<<20)
	var off int64
	for lineNo := 1; ; lineNo++ {
		line, rerr := r.ReadBytes('\n')
		if len(line) > 0 {
			complete := bytes.HasSuffix(line, []byte("\n"))
			var e Event
			if err := json.Unmarshal(bytes.TrimSpace(line), &e); err != nil || !e.Kind.Valid() || !e.Level.Valid() {
				if !complete && errors.Is(rerr, io.EOF) {
					return x, true, nil
				}
				return nil, false, Dataf("%s line %d: not an event", path, lineNo)
			}
			n := len(line)
			if complete {
				n--
			}
			x.add(e, off, n)
			off += int64(len(line))
		}
		if rerr != nil {
			return x, false, nil
		}
	}
}

// setPath follows a renamed log.
func (x *SessionIndex) setPath(p string) {
	x.mu.Lock()
	x.path = p
	x.mu.Unlock()
}

/** Len returns the number of indexed events.
 *
 * Example:
 *   n := idx.Len()
 */
func (x *SessionIndex) Len() int {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return len(x.entries)
}

/** Turns returns the number of turns, which is the number of user_input events.
 *
 * Example:
 *   n := idx.Turns()
 */
func (x *SessionIndex) Turns() int {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return len(x.turns)
}

/** Lookup finds an event's entry by id.
 *
 * Parameters:
 *   id (string) — the event id.
 *
 * Returns:
 *   IndexEntry — the entry.
 *   bool       — false if there is no such event.
 *
 * Example:
 *   e, ok := idx.Lookup("s#0003")
 */
func (x *SessionIndex) Lookup(id string) (IndexEntry, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	i, ok := x.byID[id]
	if !ok {
		return IndexEntry{}, false
	}
	return x.entries[i], true
}

/** LastTurns returns the entries from the start of the n-th last turn to the
 * end of the log, level-debug events left out. The events before the first
 * turn are not part of any turn. n greater than the number of turns returns
 * every turn; n of 0 or less returns nothing.
 *
 * Parameters:
 *   n (int) — how many turns from the end.
 *
 * Returns:
 *   []IndexEntry — a copy, in log order.
 *
 * Example:
 *   for _, e := range idx.LastTurns(2) { fmt.Println(e.ID) }
 */
func (x *SessionIndex) LastTurns(n int) []IndexEntry {
	x.mu.RLock()
	defer x.mu.RUnlock()
	if n <= 0 || len(x.turns) == 0 {
		return nil
	}
	if n > len(x.turns) {
		n = len(x.turns)
	}
	var out []IndexEntry
	for _, e := range x.entries[x.turns[len(x.turns)-n]:] {
		if e.Level != LevelDebug {
			out = append(out, e)
		}
	}
	return out
}

/** Pins returns the ids of the pin events that have not been unpinned, oldest
 * first.
 *
 * Example:
 *   ids := idx.Pins()
 */
func (x *SessionIndex) Pins() []string {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return append([]string(nil), x.pins...)
}

/** Compactions returns the compaction events and the events each stands for,
 * oldest first.
 *
 * Example:
 *   for _, c := range idx.Compactions() { fmt.Println(c.ID, c.Sources) }
 */
func (x *SessionIndex) Compactions() []CompactionRef {
	x.mu.RLock()
	defer x.mu.RUnlock()
	out := make([]CompactionRef, len(x.compactions))
	for i, c := range x.compactions {
		out[i] = CompactionRef{ID: c.ID, Sources: append([]string(nil), c.Sources...)}
	}
	return out
}

/** Event reads one event back from the log file by its id.
 *
 * Parameters:
 *   id (string) — the event id.
 *
 * Returns:
 *   Event — the event.
 *   error — negative (1) if the id is unknown, no_input (66) or data (65) if
 *           the file cannot be read as the index expects.
 *
 * Example:
 *   ev, err := idx.Event("s#0003")
 */
func (x *SessionIndex) Event(id string) (Event, error) {
	e, ok := x.Lookup(id)
	if !ok {
		return Event{}, Negativef("no event %s", id)
	}
	x.mu.RLock()
	path := x.path
	x.mu.RUnlock()
	f, err := os.Open(path)
	if err != nil {
		return Event{}, ClassedAs(ClassNoInput, err)
	}
	defer f.Close()
	buf := make([]byte, e.Len)
	if _, err := f.ReadAt(buf, e.Off); err != nil {
		return Event{}, ClassedAs(ClassIO, err)
	}
	var ev Event
	if err := json.Unmarshal(buf, &ev); err != nil || ev.ID != id {
		return Event{}, Dataf("%s: the line at %d is not event %s", path, e.Off, id)
	}
	return ev, nil
}

/** Equal reports whether two indexes describe the same log: the same entries,
 * pins and compactions. The file path is not compared. It is how a rebuilt
 * index is checked against the live one.
 *
 * Parameters:
 *   o (*SessionIndex) — the other index.
 *
 * Returns:
 *   bool — true if they agree.
 *
 * Example:
 *   if !live.Equal(rebuilt) { log.Fatal("index drift") }
 */
func (x *SessionIndex) Equal(o *SessionIndex) bool {
	x.mu.RLock()
	defer x.mu.RUnlock()
	o.mu.RLock()
	defer o.mu.RUnlock()
	if len(x.entries) != len(o.entries) || len(x.pins) != len(o.pins) || len(x.compactions) != len(o.compactions) {
		return false
	}
	for i := range x.entries {
		if x.entries[i] != o.entries[i] {
			return false
		}
	}
	for i := range x.pins {
		if x.pins[i] != o.pins[i] {
			return false
		}
	}
	for i := range x.compactions {
		a, b := x.compactions[i], o.compactions[i]
		if a.ID != b.ID || strings.Join(a.Sources, "\x00") != strings.Join(b.Sources, "\x00") {
			return false
		}
	}
	return true
}
