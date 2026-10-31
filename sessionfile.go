package harvey

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// sessionfile.go writes the Fountain file of a session from its log, and
// recovers the files of sessions that ended without closing (plan item L4).

/** RenderSessionFile renders the session log at logPath as Fountain and writes
 * it to outPath. The write is atomic: the text goes to a temporary file in the
 * same directory and is renamed over outPath, so a failure leaves whatever was
 * there as it was.
 *
 * Parameters:
 *   logPath (string)         — the .jsonl session log.
 *   outPath (string)         — where the Fountain file goes.
 *   zone    (*time.Location) — the zone event times are shown in; nil is the local zone.
 *
 * Returns:
 *   error — if the log cannot be read or has no session_start, or the file cannot be written.
 *
 * Example:
 *   err := RenderSessionFile("agents/sessions/s1.jsonl", "agents/sessions/s1.spmd", time.Local)
 */
func RenderSessionFile(logPath, outPath string, zone *time.Location) error {
	events, _, err := ReadSessionLog(logPath)
	if err != nil {
		return err
	}
	data := RenderFountain(events, RenderOptions{Zone: zone, Blobs: BlobDir(logPath)})
	if data == nil {
		return Dataf("%s: no session_start, nothing to render", logPath)
	}
	tmp, err := os.CreateTemp(filepath.Dir(outPath), ".render-*")
	if err != nil {
		return ClassedAs(ClassCantCreate, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
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
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return ClassedAs(ClassIO, err)
	}
	return ClassedAs(ClassIO, os.Rename(tmp.Name(), outPath))
}

/** RecoverSessionFiles renders the Fountain file of every session log in dir
 * that ended without a session_end (the process died). A file with the log's
 * name is replaced when it is older than the log, which is what a partial live
 * recording is; a file newer than the log, from an earlier recovery, is left
 * alone. A session that closed cleanly is never touched, and no file is made
 * for one, since it may have been renamed. The names are returned, sorted.
 *
 * Parameters:
 *   dir  (string)         — the sessions directory.
 *   zone (*time.Location) — the zone event times are shown in; nil is the local zone.
 *
 * Returns:
 *   []string — the session names recovered, without extension.
 *   error    — if the directory cannot be read.
 *
 * Example:
 *   names, _ := RecoverSessionFiles("agents/sessions", time.Local)
 */
func RecoverSessionFiles(dir string, zone *time.Location) ([]string, error) {
	logs, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(logs)
	var recovered []string
	for _, lp := range logs {
		events, torn, err := ReadSessionLog(lp)
		if err != nil || len(events) == 0 {
			continue // not a session log this version can read; leave it
		}
		if !torn && events[len(events)-1].Kind == KindSessionEnd {
			continue
		}
		stem := strings.TrimSuffix(filepath.Base(lp), ".jsonl")
		out := filepath.Join(dir, stem+".spmd")
		li, lerr := os.Stat(lp)
		if fi, serr := os.Stat(out); serr == nil && lerr == nil && !fi.ModTime().Before(li.ModTime()) {
			continue // already recovered
		}
		if err := RenderSessionFile(lp, out, zone); err != nil {
			continue
		}
		recovered = append(recovered, stem)
	}
	return recovered, nil
}

// renderAtClose says whether finishSession renders the Fountain file from the
// log. It is true; the tests that compare the rendering with the live
// recorder's output turn it off so the live file is still there to compare.
var renderAtClose = true

/** finishSession ends the session's records: the log gets its session_end, the
 * recorder is closed, and the Fountain file is rendered from the log over the
 * live recording. If the rendering fails the live recording is kept and a
 * warning is printed. It does nothing for a part that is already closed.
 *
 * Parameters:
 *   out (io.Writer) — where the notices go.
 *
 * Example:
 *   defer a.finishSession(out)
 */
func (a *Agent) finishSession(out interface{ Write([]byte) (int, error) }) {
	logPath := ""
	if a.SessionLog != nil {
		logPath = a.SessionLog.Path()
	}
	a.closeSessionLog("quit")
	if a.Recorder == nil {
		return
	}
	path := a.Recorder.Path()
	a.Recorder.Close()
	a.Recorder = nil
	if logPath != "" && renderAtClose {
		if err := RenderSessionFile(logPath, path, time.Local); err != nil {
			fmt.Fprintf(out, yellow("  ⚠")+" Could not render %s from the session log (%v); the live recording is kept.\n", path, err)
		}
	}
	fmt.Fprintf(out, dim("  Session saved to %s\n"), path)
}
