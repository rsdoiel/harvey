package harvey

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// hailoPullSizeNote is what the confirmation tells the user about the cost of
// a pull; the Hailo models measured so far are 1.7 and 3.4 GB.
const hailoPullSizeNote = "1 to 4 GB"

// pullLine is one line of hailo-ollama's /api/pull answer, which is a stream of
// JSON objects. An unknown model is HTTP 200 with only {"error": "..."}.
type pullLine struct {
	Status    string `json:"status"`
	Digest    string `json:"digest"`
	Total     int64  `json:"total"`
	Completed int64  `json:"completed"`
	Error     string `json:"error"`
}

/** pullHailoModel pulls a model from the Hailo catalog onto the hailo-ollama
 * server and shows its progress, a line for each tenth of a file. hailo-ollama
 * reads the request's "model" key (not Ollama's older "name") and reports
 * failures inside an HTTP 200 stream, so both are handled here.
 *
 * Parameters:
 *   url  (string)    — hailo-ollama base URL.
 *   name (string)    — catalog model name, e.g. "qwen2.5-coder:1.5b".
 *   out  (io.Writer) — destination for progress.
 *
 * Returns:
 *   error — nil once the stream reports success; class negative for a model the
 *           server does not know; class unavailable when the server cannot be
 *           reached or answers an error status; class io when the stream ends
 *           before success.
 *
 * Example:
 *   err := pullHailoModel("http://localhost:8000", "qwen2:1.5b", os.Stdout)
 */
func pullHailoModel(url, name string, out io.Writer) error {
	body, _ := json.Marshal(map[string]any{"model": name, "stream": true})
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(url, "/")+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{}).Do(req) // no timeout: a pull takes minutes
	if err != nil {
		return Unavailablef("hailo-ollama is not reachable at %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return Unavailablef("hailo-ollama answered HTTP %d to the pull: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	dec := json.NewDecoder(resp.Body)
	lastStatus := ""
	lastPct := map[string]int{}
	for {
		var l pullLine
		if err := dec.Decode(&l); err != nil {
			if err == io.EOF {
				break
			}
			return IOf("pull of %s: reading the answer: %v", name, err)
		}
		if l.Error != "" {
			if strings.Contains(strings.ToLower(l.Error), "not found") {
				return NotFoundf("hailo-ollama: %s", l.Error)
			}
			return IOf("hailo-ollama: %s", l.Error)
		}
		if l.Total > 0 {
			pct := int(l.Completed * 100 / l.Total)
			if p, seen := lastPct[l.Digest]; !seen || pct >= p+10 || pct == 100 && p != 100 {
				lastPct[l.Digest] = pct
				fmt.Fprintf(out, "  pulling %s… %d%%\n", name, pct)
			}
			continue
		}
		if l.Status != "" && l.Status != lastStatus {
			lastStatus = l.Status
			fmt.Fprintf(out, "  %s\n", l.Status)
		}
		if l.Status == "success" {
			return nil
		}
	}
	return IOf("pull of %s ended before it finished", name)
}

/** confirmHailoPull asks before a pull, which downloads gigabytes. yes skips
 * the question (--yes). With nobody at a terminal and no yes, the pull is
 * refused rather than started.
 *
 * Parameters:
 *   name (string)    — the model to pull.
 *   yes  (bool)      — the user has already agreed (--yes).
 *   out  (io.Writer) — destination for the question.
 *
 * Returns:
 *   error — nil to go ahead; class negative when declined or unattended.
 *
 * Example:
 *   if err := a.confirmHailoPull("qwen2:1.5b", false, out); err != nil { return err }
 */
func (a *Agent) confirmHailoPull(name string, yes bool, out io.Writer) error {
	if yes {
		return nil
	}
	if !a.attended() {
		return Negativef("%s is not pulled; pulling downloads %s, so confirm at a terminal or run /model pull --yes %s", name, hailoPullSizeNote, name)
	}
	fmt.Fprintf(out, "  Pull %s from the Hailo catalog? It downloads %s. [y/N] ", name, hailoPullSizeNote)
	switch strings.ToLower(strings.TrimSpace(readLineFrom(a.In))) {
	case "y", "yes":
		return nil
	}
	return Negativef("%s was not pulled", name)
}

/** cmdModelPull implements /model pull [--yes] NAME: it pulls a model from the
 * Hailo catalog. Ollama models are pulled with the ollama CLI (DR-0022), so an
 * ollama/NAME is refused with the command to run.
 *
 * Parameters:
 *   a    (*Agent)    — the running agent.
 *   args ([]string)  — arguments after "pull".
 *   out  (io.Writer) — destination for output.
 *
 * Returns:
 *   error — usage for a bad command line; negative for an unknown, declined or
 *           unconfirmed pull; unavailable when no Hailo server is running.
 *
 * Example:
 *   err := cmdModelPull(a, []string{"--yes", "qwen2:1.5b"}, os.Stdout)
 */
func cmdModelPull(a *Agent, args []string, out io.Writer) error {
	yes := false
	var names []string
	for _, arg := range args {
		if arg == "--yes" || arg == "-y" {
			yes = true
		} else {
			names = append(names, arg)
		}
	}
	if len(names) != 1 {
		return Usagef("usage: /model pull [--yes] NAME   (NAME is a model in the Hailo catalog; see /model list)")
	}
	engine, model := parseQualifiedModel(names[0])
	switch engine {
	case "ollama":
		return Usagef("Ollama models are pulled with the ollama CLI: ! ollama pull %s", model)
	case "llamafile", "llamacpp":
		return Usagef("%s models are files you download; /model pull is for the Hailo catalog", engine)
	}

	f := a.ollamaFamily()
	if f.HailoURL == "" {
		return Unavailablef("no hailo-ollama server found; start it with: systemctl --user start hailo-ollama")
	}
	var entry *ModelSummary
	for _, m := range a.listFamilyModels() {
		if m.Engine == "hailo" && strings.EqualFold(m.Name, model) {
			m := m
			entry = &m
			break
		}
	}
	if entry == nil {
		return Negativef("%q is not in the Hailo catalog (see /model list)", model)
	}
	if !entry.NotPulled {
		fmt.Fprintf(out, "  %s is already pulled.\n", entry.Name)
		return nil
	}
	if err := a.confirmHailoPull(entry.Name, yes, out); err != nil {
		return err
	}
	if err := pullHailoModel(f.HailoURL, entry.Name, out); err != nil {
		return err
	}
	fmt.Fprintf(out, "  %s Pulled %s; use it with /model use hailo/%s\n", green("✓"), entry.Name, entry.Name)
	return nil
}

/** pullIfNeeded pulls a catalog model that is not pulled yet, after the usual
 * confirmation, so choosing it in a picker or with /model use ends with it
 * ready to use. A pulled model is left alone.
 *
 * Parameters:
 *   m   (ModelSummary) — the chosen model.
 *   out (io.Writer)    — destination for the question and progress.
 *
 * Returns:
 *   error — see confirmHailoPull and pullHailoModel; nil when nothing was needed.
 *
 * Example:
 *   if err := a.pullIfNeeded(selected, out); err != nil { return err }
 */
func (a *Agent) pullIfNeeded(m ModelSummary, out io.Writer) error {
	if !m.NotPulled {
		return nil
	}
	url := a.ollamaFamily().HailoURL
	if url == "" {
		return Unavailablef("no hailo-ollama server found; start it with: systemctl --user start hailo-ollama")
	}
	if err := a.confirmHailoPull(m.Name, false, out); err != nil {
		return err
	}
	return pullHailoModel(url, m.Name, out)
}
