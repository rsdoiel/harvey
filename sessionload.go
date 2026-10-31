package harvey

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// sessionload.go reads past sessions from their logs (plan item L4b, decided
// 2026-10-30): resume, replay and the session list work from the session log, not
// from the Fountain file, which is a presentation of it. A session recorded
// before the log existed has only a .spmd or .fountain file; that is still read,
// through the old parser, as a fallback.

/** SessionInfo is what harvey knows about a past session.
 *
 * Fields:
 *   Name      (string)         — the file name without extension.
 *   Path      (string)         — the log, or for an old session the Fountain file.
 *   Source    (string)         — "log" or "fountain".
 *   Date      (time.Time)      — when the session started (the file's time for an old one).
 *   Model     (string)         — the model it started with, as "backend (model)".
 *   LastModel (string)         — the model it ended with; the same unless it switched.
 *   Turns     ([]PlaybackTurn) — the turns that were answered, in order.
 *   Ended     (bool)           — the session closed cleanly.
 *
 * Example:
 *   info, err := LoadSessionInfo("agents/sessions/harvey-session-20261030-143612.spmd")
 */
type SessionInfo struct {
	Name      string
	Path      string
	Source    string
	Date      time.Time
	Model     string
	LastModel string
	Turns     []PlaybackTurn
	Ended     bool
}

/** LoadSessionInfo reads a past session. The path may be the session log
 * (.jsonl), its Fountain file (.spmd or .fountain, which resolves to the log
 * beside it), or a name without extension. A session with a log is read from
 * the log; one without is read from its Fountain file with the old parser.
 *
 * A turn counts when the model answered it: a failed or cancelled turn is not
 * in the live history either. Turns sent to a route (@name) are kept, marked
 * with their route.
 *
 * Parameters:
 *   path (string) — the session's log or Fountain file, or its name.
 *
 * Returns:
 *   SessionInfo — the session.
 *   error       — no_input if nothing is there, data if the log is malformed.
 *
 * Example:
 *   info, err := LoadSessionInfo(path)
 *   if err == nil { fmt.Println(len(info.Turns), "turns") }
 */
func LoadSessionInfo(path string) (SessionInfo, error) {
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	switch ext {
	case ".jsonl":
		return loadSessionLog(path)
	case ".spmd", ".fountain":
		if _, err := os.Stat(stem + ".jsonl"); err == nil {
			return loadSessionLog(stem + ".jsonl")
		}
		return loadSessionFountain(path)
	default:
		for _, cand := range []string{path + ".jsonl", path + ".spmd", path + ".fountain"} {
			if _, err := os.Stat(cand); err == nil {
				return LoadSessionInfo(cand)
			}
		}
		_, err := os.Stat(path)
		if err == nil {
			err = errors.New("not a session file")
		}
		return SessionInfo{}, ClassedAs(ClassNoInput, err)
	}
}

func loadSessionFountain(path string) (SessionInfo, error) {
	_, model, turns, err := parseFountainSession(path)
	if err != nil {
		return SessionInfo{}, err
	}
	info := SessionInfo{
		Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), Path: path, Source: "fountain",
		Model: model, LastModel: model, Turns: turns, Ended: true,
	}
	if fi, err := os.Stat(path); err == nil {
		info.Date = fi.ModTime()
	}
	return info, nil
}

func loadSessionLog(path string) (SessionInfo, error) {
	events, torn, err := ReadSessionLog(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return SessionInfo{}, ClassedAs(ClassNoInput, err)
		}
		return SessionInfo{}, err
	}
	opts := RenderOptions{Blobs: BlobDir(path)}
	info := SessionInfo{
		Name: strings.TrimSuffix(filepath.Base(path), ".jsonl"), Path: path, Source: "log",
		Ended: !torn && events[len(events)-1].Kind == KindSessionEnd,
	}
	var start struct {
		Backend string `json:"backend"`
		Model   string `json:"model"`
	}
	_ = json.Unmarshal(events[0].Data, &start)
	info.Model = start.Backend + " (" + start.Model + ")"
	info.LastModel = info.Model
	if t, err := time.Parse(time.RFC3339, events[0].T); err == nil {
		info.Date = t.Local()
	}
	for i := 1; i < len(events); i++ {
		e := events[i]
		if e.Level == LevelDebug {
			continue
		}
		switch e.Kind {
		case KindModelSwitch:
			var d struct {
				To ModelRef `json:"to"`
			}
			_ = json.Unmarshal(e.Data, &d)
			info.LastModel = d.To.Backend + " (" + d.To.Model + ")"
		case KindUserInput:
			end := turnEnd(events, i)
			if reply, ok := answeredReply(events[i:end], opts); ok {
				var in struct {
					Text  json.RawMessage `json:"text"`
					Route string          `json:"route"`
				}
				_ = json.Unmarshal(e.Data, &in)
				info.Turns = append(info.Turns, PlaybackTurn{UserInput: opts.contentOf(in.Text), ModelReply: reply, Route: in.Route})
			}
			i = end - 1
		}
	}
	return info, nil
}

// answeredReply returns the text of the last ok model response that answers the
// turn itself, not a side request (chunk analysis, a pipeline step) made during it.
func answeredReply(turn []Event, opts RenderOptions) (string, bool) {
	adhoc := map[string]bool{}
	reply, ok := "", false
	for _, e := range turn[1:] {
		if e.Level == LevelDebug {
			continue
		}
		switch e.Kind {
		case KindModelRequest:
			var rq renderRequest
			_ = json.Unmarshal(e.Data, &rq)
			if rq.Form == "adhoc" {
				adhoc[e.ID] = true
			}
		case KindModelResponse:
			var r renderResponse
			_ = json.Unmarshal(e.Data, &r)
			var link struct {
				Request string `json:"request"`
			}
			_ = json.Unmarshal(e.Data, &link)
			if r.Status == "ok" && !adhoc[link.Request] {
				reply, ok = opts.contentOf(r.Text), true
			}
		}
	}
	return reply, ok
}

/** SessionListing is one line of the session list.
 *
 * Example:
 *   for _, s := range listings { fmt.Println(s.Name, s.Turns) }
 */
type SessionListing struct {
	Name    string
	Path    string
	Source  string
	Model   string
	Turns   int
	Ended   bool
	ModTime time.Time
}

/** ListSessions lists the sessions in dir, newest first. A log and the Fountain
 * file rendered from it are one session; a Fountain file with no log is an old
 * session and is listed from the file. A log that cannot be read is skipped.
 *
 * Parameters:
 *   dir (string) — the sessions directory; a missing one lists nothing.
 *
 * Returns:
 *   []SessionListing — one per session.
 *   error            — if the directory cannot be read.
 *
 * Example:
 *   sessions, err := ListSessions(a.SessionsDir)
 */
func ListSessions(dir string) ([]SessionListing, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	logs := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			logs[strings.TrimSuffix(e.Name(), ".jsonl")] = true
		}
	}
	mtime := func(name string) time.Time {
		if fi, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return fi.ModTime()
		}
		return time.Time{}
	}
	var out []SessionListing
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		if seen[stem] || (ext != ".jsonl" && ext != ".spmd" && ext != ".fountain") {
			continue
		}
		seen[stem] = true
		path := filepath.Join(dir, name)
		if logs[stem] {
			path = filepath.Join(dir, stem+".jsonl")
		}
		info, err := LoadSessionInfo(path)
		if err != nil {
			continue
		}
		t := mtime(stem + ".jsonl")
		for _, x := range []string{".spmd", ".fountain"} {
			if m := mtime(stem + x); m.After(t) {
				t = m
			}
		}
		out = append(out, SessionListing{
			Name: stem, Path: info.Path, Source: info.Source, Model: info.Model,
			Turns: len(info.Turns), Ended: info.Ended, ModTime: t,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

/** ContinueFromSession loads a past session's turns into the conversation
 * history, user and model messages in order, so a conversation can carry on
 * where it stopped. It reads the session's log, or the Fountain file of an old
 * session.
 *
 * Parameters:
 *   path (string) — the session's log or Fountain file, or its name.
 *
 * Returns:
 *   int   — the number of turns loaded.
 *   error — if the session cannot be read.
 *
 * Example:
 *   n, err := agent.ContinueFromSession("agents/sessions/s1.jsonl")
 */
func (a *Agent) ContinueFromSession(path string) (int, error) {
	info, err := LoadSessionInfo(path)
	if err != nil {
		return 0, err
	}
	for _, t := range info.Turns {
		if t.UserInput != "" {
			a.addPrompt("user", "resume", t.UserInput)
		}
		if t.ModelReply != "" {
			a.addPrompt("assistant", "resume", t.ModelReply)
		}
	}
	return len(info.Turns), nil
}
