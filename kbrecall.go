package harvey

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

/** KBRecallSource is a work a recalled row cites, as `kb -json concept recall`
 * reports it (DR-0037 item 4). RetractionNote is set only when Retracted is.
 *
 * Example:
 *   if s.Retracted { fmt.Println(s.Title, "was retracted:", s.RetractionNote) }
 */
type KBRecallSource struct {
	ID              int    `json:"id"`
	Title           string `json:"title"`
	IdentifierType  string `json:"identifier_type"`
	IdentifierValue string `json:"identifier_value"`
	Retracted       bool   `json:"retracted"`
	RetractionNote  string `json:"retraction_note"`
	Relationship    string `json:"relationship"`
}

/** KBRecallMaterial names the file a recalled row was ingested from. It is nil
 * for a row that was never a file, such as an observation.
 *
 * Example:
 *   if h.Material != nil { fmt.Println(h.Material.Path) }
 */
type KBRecallMaterial struct {
	Path       string `json:"path"`
	Checksum   string `json:"checksum"`
	Format     string `json:"format"`
	IngestedAt string `json:"ingested_at"`
}

/** KBRecallHit is one row of `kb -json concept recall`. Kind is observation,
 * record, document_gist or document_section. Excerpt is kb's own, shortened
 * text; it is empty for a document row that is not reviewed, which is kb's rule
 * and which this package keeps (an empty excerpt is not injected).
 *
 * Example:
 *   for _, h := range res.Hits { fmt.Println(h.Ref(), h.Excerpt) }
 */
type KBRecallHit struct {
	Kind      string            `json:"kind"`
	ID        int               `json:"id"`
	Project   string            `json:"project"`
	Concepts  []string          `json:"concepts"`
	Title     string            `json:"title"`
	Excerpt   string            `json:"excerpt"`
	Sources   []KBRecallSource  `json:"sources"`
	Material  *KBRecallMaterial `json:"material"`
	Retracted bool              `json:"retracted"`
}

/** KBRecallResult is the decoded output of `kb -json concept recall`.
 *
 * Example:
 *   res, err := ParseKBRecall(raw)
 */
type KBRecallResult struct {
	Hits     []KBRecallHit `json:"hits"`
	Matched  []string      `json:"matched"`
	Projects []string      `json:"projects"`
}

/** kbErrorObject is the error object kb prints with --json: the message, the
 * class name from the workspace exit-code table, and the numeric code.
 */
type kbErrorObject struct {
	Error string `json:"error"`
	Class string `json:"class"`
	Code  int    `json:"code"`
}

/** Ref returns the identifier harvey uses for a recalled row elsewhere (pins,
 * the manifest): kb:<kind>:<id>, with a document row's kind kept whole.
 *
 * Returns:
 *   string — for example "kb:observation:2"
 *
 * Example:
 *   h := KBRecallHit{Kind: "record", ID: 120}
 *   _ = h.Ref() // "kb:record:120"
 */
func (h KBRecallHit) Ref() string {
	return "kb:" + h.Kind + ":" + strconv.Itoa(h.ID)
}

/** ParseKBRecall decodes the output of `kb -json concept recall`. An error
 * object with class "negative" (no concept matched) is not a failure: it
 * returns an empty result and nil. Any other error object is returned as an
 * error carrying kb's message; text that is not JSON of either shape is an
 * error.
 *
 * Parameters:
 *   raw ([]byte) — kb's standard output, or its standard error when it printed an error object
 *
 * Returns:
 *   KBRecallResult — the hits; empty when nothing matched
 *   error — non-nil for a kb error other than "nothing matched", or bad JSON
 *
 * Example:
 *   res, err := ParseKBRecall(out)
 *   if err == nil && len(res.Hits) == 0 { fmt.Println("nothing to recall") }
 */
func ParseKBRecall(raw []byte) (KBRecallResult, error) {
	var res KBRecallResult
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return res, fmt.Errorf("kb concept recall: empty output")
	}
	// kb may print notices ("kb: using the workspace at ...") ahead of the JSON.
	if i := bytes.Index(trimmed, []byte("{")); i > 0 && !bytes.HasPrefix(trimmed, []byte("[")) {
		trimmed = trimmed[i:]
	}
	var probe kbErrorObject
	if err := json.Unmarshal(trimmed, &probe); err == nil && probe.Error != "" {
		if probe.Class == "negative" {
			return res, nil
		}
		return res, fmt.Errorf("kb concept recall: %s (%s)", probe.Error, probe.Class)
	}
	if err := json.Unmarshal(trimmed, &res); err != nil {
		return KBRecallResult{}, fmt.Errorf("kb concept recall: unreadable output: %w", err)
	}
	return res, nil
}

/** FormatKBRecallHit renders a recalled row as injectable text, or "" when
 * the row has nothing to say (an unreviewed document row, whose excerpt kb
 * leaves empty). A cited work is listed under "Cited:"; a retracted one is
 * named as RETRACTED with kb's retraction note, so the model does not lean on
 * it. A row whose own Retracted flag is set but whose sources are not listed is
 * still marked.
 *
 * Parameters:
 *   h (KBRecallHit) — the row
 *
 * Returns:
 *   string — the text to inject, or "" to skip the row
 *
 * Example:
 *   if s := FormatKBRecallHit(h); s != "" { block = append(block, s) }
 */
func FormatKBRecallHit(h KBRecallHit) string {
	if strings.TrimSpace(h.Excerpt) == "" {
		return ""
	}
	var b strings.Builder
	switch h.Kind {
	case "observation":
		b.WriteString("observation: " + h.Excerpt)
	case "record":
		b.WriteString("record: " + h.Title + "\n\n" + h.Excerpt)
	case "document_gist", "document_section":
		b.WriteString("document: " + h.Title + "\n\n" + h.Excerpt)
	default:
		return ""
	}
	if len(h.Sources) > 0 {
		b.WriteString("\n\nCited:")
		for _, s := range h.Sources {
			b.WriteString("\n- " + s.Title)
			if s.IdentifierValue != "" {
				b.WriteString(" (" + s.IdentifierType + ": " + s.IdentifierValue + ")")
			}
			if s.Retracted {
				b.WriteString(" — RETRACTED")
				if s.RetractionNote != "" {
					b.WriteString(": " + s.RetractionNote)
				}
			}
		}
	} else if h.Retracted {
		b.WriteString("\n\nRETRACTED")
	}
	return b.String()
}

// kbRecallTimeout bounds a recall that has no configured run timeout.
const kbRecallTimeout = 15 * time.Second

/** runKBJSON runs the installed `kb -db DB -json ARGS...` in dir with no stdin
 * and returns what it printed. kb prints a result on standard output and an
 * error object on standard error; both are returned.
 *
 * Parameters:
 *   dir (string) — working directory (the workspace root)
 *   db (string) — path of the knowledge database
 *   timeout (time.Duration) — kill kb after this long; 0 means kbRecallTimeout
 *   args (...string) — the verb and its arguments
 *
 * Returns:
 *   stdout, stderr ([]byte) — what kb printed
 *   exit (int) — kb's exit code
 *   error — Unavailable if kb is not installed, or the start failure; nil
 *           when kb ran, whatever its exit code
 *
 * Example:
 *   out, _, code, err := runKBJSON(root, db, 0, "project", "list")
 */
func runKBJSON(dir, db string, timeout time.Duration, args ...string) (stdout, stderr []byte, exit int, err error) {
	bin, lerr := lookKB()
	if lerr != nil {
		return nil, nil, 0, lerr
	}
	if timeout <= 0 {
		timeout = kbRecallTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, append([]string{"-db", db, "-json"}, args...)...)
	cmd.Dir = dir
	cmd.Env = filterCommandEnvironment(os.Environ())
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	runErr := cmd.Run()
	if cmd.ProcessState == nil {
		return nil, nil, 0, startFailure("kb", runErr)
	}
	if ctx.Err() != nil {
		return so.Bytes(), se.Bytes(), cmd.ProcessState.ExitCode(), kbExitError(cmd.ProcessState.ExitCode(), true)
	}
	return so.Bytes(), se.Bytes(), cmd.ProcessState.ExitCode(), nil
}

/** kbRecallAt runs `kb -json concept recall TEXT --limit N` against db and
 * decodes it.
 *
 * Parameters:
 *   dir (string) — working directory (the workspace root)
 *   db (string) — path of the knowledge database
 *   timeout (time.Duration) — 0 means kbRecallTimeout
 *   text (string) — what to recall for; kb matches concept names in it
 *   limit (int) — most rows to return; 0 leaves kb's default
 *
 * Returns:
 *   KBRecallResult — the hits; empty when no concept matched
 *   error — Unavailable if kb is not installed; otherwise the failure
 *
 * Example:
 *   res, err := kbRecallAt(root, db, 0, "ollama context window", 5)
 */
func kbRecallAt(dir, db string, timeout time.Duration, text string, limit int) (KBRecallResult, error) {
	args := []string{"concept", "recall"}
	if limit > 0 {
		args = append(args, "--limit", strconv.Itoa(limit))
	}
	args = append(args, "--", text)
	so, se, code, err := runKBJSON(dir, db, timeout, args...)
	if err != nil {
		return KBRecallResult{}, err
	}
	raw := so
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = se
	}
	res, perr := ParseKBRecall(raw)
	if perr != nil && code != 0 && code != 1 {
		return KBRecallResult{}, kbExitError(code, false)
	}
	return res, perr
}

/** kbRecall is kbRecallAt for the agent's workspace and knowledge database.
 *
 * Parameters:
 *   text (string) — what to recall for
 *   limit (int) — most rows to return; 0 leaves kb's default
 *
 * Returns:
 *   KBRecallResult, error — as kbRecallAt
 *
 * Example:
 *   res, err := a.kbRecall("ollama context window", 5)
 */
func (a *Agent) kbRecall(text string, limit int) (KBRecallResult, error) {
	return kbRecallAt(a.Workspace.Root, a.knowledgeDBPath(), a.Config.Security.RunTimeout, text, limit)
}
