package harvey

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

/** ServerFamily says which Ollama-shaped servers Harvey can use this session
 * and where. Ollama and hailo-ollama speak the same API, so a Hailo server
 * can be sitting at ollama.url (harvey.local's hand-edited setup); it is then
 * relabelled "hailo", and there is no Ollama server.
 *
 * Fields:
 *   OllamaURL  (string) — Ollama base URL; "" when ollama.url is really a Hailo server.
 *   HailoURL   (string) — hailo-ollama base URL; "" when no Hailo server answered.
 *   Relabelled (bool)   — the Hailo server was found at ollama.url.
 *   HailoCatalog ([]string) — the models the Hailo server can run, pulled or not.
 *
 * Example:
 *   f := a.ollamaFamily()
 *   if f.HailoURL != "" { fmt.Println("Hailo at", f.HailoURL) }
 */
type ServerFamily struct {
	OllamaURL    string
	HailoURL     string
	Relabelled   bool
	HailoCatalog []string // what the Hailo server can run, pulled or not
}

// sameServerURL reports whether two base URLs name the same server.
func sameServerURL(a, b string) bool {
	norm := func(s string) string { return strings.ToLower(strings.TrimRight(strings.TrimSpace(s), "/")) }
	return a != "" && norm(a) == norm(b)
}

/** ollamaFamily detects the Ollama-shaped servers for this session. It makes
 * no request at all when there is no Hailo card and hailo.url is not set, so a
 * machine without the HAT behaves exactly as before. Otherwise it asks the
 * Hailo URL for /hailo/v1/list and, if nothing Hailo answers there, asks
 * ollama.url the same question, which relabels a Hailo server found there.
 *
 * Returns:
 *   ServerFamily — where Ollama and hailo-ollama are; see its fields.
 *
 * Example:
 *   f := a.ollamaFamily()
 */
func (a *Agent) ollamaFamily() ServerFamily {
	f := ServerFamily{OllamaURL: a.Config.Ollama.URL}
	st := DetectHailo(a.Config.Hailo, a.hailoDevicePath)
	if !st.Probed {
		return f
	}
	if st.ServerUp {
		f.HailoURL = st.URL
		f.HailoCatalog = st.Catalog
	} else if cat := fetchHailoCatalog(a.Config.Ollama.URL); cat != nil {
		f.HailoURL = a.Config.Ollama.URL
		f.HailoCatalog = cat
	}
	if sameServerURL(f.HailoURL, a.Config.Ollama.URL) {
		f.Relabelled = true
		f.OllamaURL = ""
	}
	return f
}

/** chatURL returns the base URL of the server the active model talks to: the
 * Hailo server when the active backend is hailo, otherwise ollama.url.
 * Embeddings do not use it; they always use ollama.url.
 *
 * Returns:
 *   string — base URL.
 *
 * Example:
 *   tokens, _ := CountTokens(ctx, a.chatURL(), model, text)
 */
func (a *Agent) chatURL() string {
	if a.Backend != nil && a.Backend.Name() == "hailo" {
		return a.Backend.BaseURL()
	}
	return a.Config.Ollama.URL
}

/** listFamilyModels returns the models on the Ollama and Hailo servers, each
 * labelled with its engine. A server that is not reachable contributes
 * nothing and no error.
 *
 * Returns:
 *   []ModelSummary — Ollama models labelled "ollama", Hailo models labelled "hailo".
 *
 * Example:
 *   all = append(all, a.listFamilyModels()...)
 */
func (a *Agent) listFamilyModels() []ModelSummary {
	f := a.ollamaFamily()
	var out []ModelSummary
	add := func(url, engine string) {
		if url == "" || !ProbeOllama(url) {
			return
		}
		summaries, err := NewOllamaClientFor(engine, url, "").ModelSummaries(context.Background())
		if err != nil {
			return
		}
		for _, s := range summaries {
			out = append(out, ModelSummary{Name: s.Name, Engine: engine, SizeBytes: s.SizeBytes})
		}
	}
	add(f.OllamaURL, "ollama")
	add(f.HailoURL, "hailo")
	// The Hailo catalog: what the card can run but has not been pulled.
	have := map[string]bool{}
	for _, m := range out {
		if m.Engine == "hailo" {
			have[strings.ToLower(m.Name)] = true
		}
	}
	for _, name := range f.HailoCatalog {
		if !have[strings.ToLower(name)] {
			out = append(out, ModelSummary{Name: name, Engine: "hailo", NotPulled: true})
		}
	}
	return out
}

/** setHailoModel makes model on the hailo-ollama server the active model:
 * client, backend and capability row, as setOllamaModel does for Ollama.
 *
 * Parameters:
 *   model (string) — model name, e.g. "llama3.2:3b".
 *
 * Returns:
 *   error — class unavailable when no Hailo server is reachable.
 *
 * Example:
 *   err := a.setHailoModel("llama3.2:3b")
 */
func (a *Agent) setHailoModel(model string) error {
	url := a.ollamaFamily().HailoURL
	if url == "" {
		return Unavailablef("no hailo-ollama server found; start it with: systemctl --user start hailo-ollama")
	}
	a.useModelAt("hailo", url, model)
	b := NewHailoBackend(url, a.Config.Ollama.Timeout, filepath.Join(a.Workspace.Root, "agents"))
	b.SetActiveModel(model)
	b.running = true
	a.Backend = b
	a.probeModelAndCache(url, model)
	return nil
}

/** migrateRelabelledAliases moves aliases that name the engine "ollama" to
 * "hailo". It is called when the Hailo server was found at ollama.url: those
 * aliases were always about that server. Aliases for other engines, and legacy
 * ones with no engine, are left alone. The change is saved and one line says so.
 *
 * Parameters:
 *   a   (*Agent)    — the running agent.
 *   out (io.Writer) — destination for the one-line notice.
 *
 * Returns:
 *   int — the number of aliases changed.
 *
 * Example:
 *   if f.Relabelled { migrateRelabelledAliases(a, out) }
 */
func migrateRelabelledAliases(a *Agent, out io.Writer) int {
	n := 0
	for k, e := range a.Config.ModelAliases {
		if strings.EqualFold(e.Engine, "ollama") {
			e.Engine = "hailo"
			a.Config.ModelAliases[k] = e
			n++
		}
	}
	if n == 0 {
		return 0
	}
	if a.Workspace != nil {
		_ = SaveModelAliases(a.Workspace, a.Config)
	}
	fmt.Fprintf(out, dim("  %d model alias(es) moved from engine ollama to hailo: the server at ollama.url is hailo-ollama.\n    Move the URL to hailo.url in harvey.yaml to free ollama.url for embeddings.\n"), n)
	return n
}

/** noteRelabelledServer tells the user, when the server at ollama.url is
 * hailo-ollama, that Harvey treats it as the hailo engine, and moves their
 * "ollama" aliases to "hailo". It prints nothing for a plain Ollama server.
 *
 * Parameters:
 *   out (io.Writer) — destination for the notice.
 *
 * Example:
 *   a.noteRelabelledServer(out)
 */
func (a *Agent) noteRelabelledServer(out io.Writer) {
	if !a.ollamaFamily().Relabelled {
		return
	}
	fmt.Fprintf(out, dim("  AI HAT+ 2: the server at %s is hailo-ollama; Harvey uses it as engine hailo.\n"), a.Config.Ollama.URL)
	migrateRelabelledAliases(a, out)
}

// hasEngine reports whether any of models is served by engine.
func hasEngine(models []ModelSummary, engine string) bool {
	for _, m := range models {
		if m.Engine == engine {
			return true
		}
	}
	return false
}

/** pickFamilyModel is the start-up model picker for when a Hailo server is in
 * play: the models of Ollama and hailo-ollama in one numbered list, each
 * labelled with its engine. A single model is used outright. A session model
 * that exists on exactly one engine is used automatically; one that exists on
 * more than one is not guessed at, and the list is shown with a note.
 *
 * Parameters:
 *   reader         (*bufio.Reader)  — reads the user's selection.
 *   out            (io.Writer)      — destination for the list and prompt.
 *   preferredModel (string)         — model name from a resumed session; "" for none.
 *   models         ([]ModelSummary) — the models on both servers.
 *
 * Returns:
 *   error — when the chosen Hailo model cannot be wired.
 *
 * Example:
 *   err := a.pickFamilyModel(reader, os.Stdout, "", a.listFamilyModels())
 */
func (a *Agent) pickFamilyModel(reader *bufio.Reader, out io.Writer, preferredModel string, models []ModelSummary) error {
	use := func(m ModelSummary, note string) error {
		if m.Engine == "hailo" {
			if err := a.pullIfNeeded(m, out); err != nil {
				return err
			}
			if err := a.setHailoModel(m.Name); err != nil {
				return err
			}
		} else {
			a.setOllamaModel(m.Name)
		}
		fmt.Fprintf(out, "  Using model: %s %s%s\n", cyan(m.Name), dim("["+m.Engine+"]"), note)
		return nil
	}

	if len(models) == 1 {
		return use(models[0], "")
	}

	if preferredModel != "" {
		var matches []ModelSummary
		for _, m := range models {
			if m.NotPulled {
				continue // a session's model is never pulled unasked
			}
			if strings.EqualFold(extractModelName(m.Name), preferredModel) || strings.EqualFold(m.Name, preferredModel) {
				matches = append(matches, m)
			}
		}
		switch len(matches) {
		case 1:
			return use(matches[0], " "+dim("(from session)"))
		case 0:
			fmt.Fprintf(out, dim("  Session model %q not found; select from available:\n"), preferredModel)
		default:
			fmt.Fprintf(out, dim("  Session model %q exists on more than one engine; choose:\n"), preferredModel)
		}
	}

	fmt.Fprintln(out, "  Available models:")
	for i, m := range models {
		fmt.Fprintf(out, "  [%d] %-40s %s\n", i+1, m.Name, dim("["+m.EngineLabel()+"]"))
	}
	fmt.Fprintf(out, "    Select model [1-%d, default=1]: ", len(models))
	line, _ := reader.ReadString('\n')
	idx := 1
	if trimmed := strings.TrimSpace(line); trimmed != "" {
		fmt.Sscanf(trimmed, "%d", &idx)
	}
	if idx < 1 || idx > len(models) {
		idx = 1
	}
	return use(models[idx-1], "")
}

// explainingEmbedder wraps an Ollama embedder so that a failure on a machine
// where Hailo is in play says why: Hailo cannot embed, and Ollama is missing.
type explainingEmbedder struct {
	Embedder
	a *Agent
}

// Embed embeds text with the wrapped embedder; a failure is explained by
// explainEmbedError.
func (e explainingEmbedder) Embed(text string) ([]float64, error) {
	vec, err := e.Embedder.Embed(text)
	if err != nil {
		return nil, e.a.explainEmbedError(err)
	}
	return vec, nil
}

/** wrapEmbedder makes an Ollama embedder explain its failures when Hailo is in
 * play. Embeddings always use Ollama (hailo-ollama has no /api/embed), so a
 * failure on a Hailo machine usually means Ollama is not there.
 *
 * Parameters:
 *   e (Embedder) — the embedder to wrap.
 *
 * Returns:
 *   Embedder — e, with failures explained.
 *
 * Example:
 *   embedder := a.wrapEmbedder(NewOllamaEmbedder(a.Config.Ollama.URL, model))
 */
func (a *Agent) wrapEmbedder(e Embedder) Embedder { return explainingEmbedder{e, a} }

/** embedderFor is NewEmbedderForEntry for the agent: an Ollama-kind embedder
 * explains its failures when Hailo is in play (see wrapEmbedder); an
 * encoderfile embedder is returned as is.
 *
 * Parameters:
 *   entry (*RagStoreEntry) — store configuration entry.
 *
 * Returns:
 *   Embedder — the embedder for the entry.
 *
 * Example:
 *   embedder := a.embedderFor(a.Config.Memory.ActiveRagStore())
 */
func (a *Agent) embedderFor(entry *RagStoreEntry) Embedder {
	e := NewEmbedderForEntry(entry, a.Config.Ollama.URL)
	if entry.EmbedderKind == "encoderfile" {
		return e
	}
	return a.wrapEmbedder(e)
}

/** explainEmbedError turns an embedding failure into a plain message when the
 * cause is that Hailo is in use and Ollama is not there. It looks for Hailo
 * only now, after a failure, so an embedding that works costs nothing, and a
 * machine with no card and no hailo.url is never asked about Hailo. Any other
 * failure is returned unchanged.
 *
 * Parameters:
 *   cause (error) — the embedder's error.
 *
 * Returns:
 *   error — class unavailable when Hailo is in play and Ollama is missing (wrapping
 *           cause when Ollama is simply unreachable; the relabelled case drops the
 *           server's 404 body as noise); otherwise cause.
 *
 * Example:
 *   return nil, a.explainEmbedError(err)
 */
func (a *Agent) explainEmbedError(cause error) error {
	f := a.ollamaFamily()
	if f.HailoURL == "" {
		return cause
	}
	if f.Relabelled {
		return Unavailablef("Hailo cannot embed; ollama.url (%s) is hailo-ollama, so there is no Ollama server. "+
			"Run Ollama and set ollama.url to it (default http://localhost:11434), and set hailo.url to %s",
			a.Config.Ollama.URL, f.HailoURL)
	}
	if !ProbeOllama(f.OllamaURL) {
		return Unavailablef("Hailo cannot embed; no Ollama at %s: %w", f.OllamaURL, cause)
	}
	return cause
}

// hailoUnitDefault is where the user's systemd unit for hailo-ollama lives,
// relative to the home directory.
const hailoUnitDefault = ".config/systemd/user/hailo-ollama.service"

// hailoUnitExists reports whether the user's hailo-ollama systemd unit exists.
func (a *Agent) hailoUnitExists() bool {
	p := a.hailoUnitPath
	if p == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		p = filepath.Join(home, hailoUnitDefault)
	}
	_, err := os.Stat(p)
	return err == nil
}

// isLoopbackURL reports whether rawURL names this machine.
func isLoopbackURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

/** hailoHintLine returns the one-line start-up hint for a Hailo card whose
 * server is not answering, or "" when there is nothing to say. The systemctl
 * command is named only when the user's unit exists and the server is meant to
 * be on this machine.
 *
 * Parameters:
 *   st         (HailoStatus) — what DetectHailo found.
 *   cfg        (HailoConfig) — the hailo: section.
 *   unitExists (bool)        — whether ~/.config/systemd/user/hailo-ollama.service exists.
 *
 * Returns:
 *   string — the hint, without a trailing newline; "" for none.
 *
 * Example:
 *   line := hailoHintLine(st, cfg.Hailo, true)
 */
func hailoHintLine(st HailoStatus, cfg HailoConfig, unitExists bool) string {
	if !st.CardPresent || !st.Probed || st.ServerUp {
		return ""
	}
	line := fmt.Sprintf("AI HAT+ 2 found, hailo-ollama is not running at %s", st.URL)
	if unitExists && isLoopbackURL(st.URL) {
		line += " (start it: systemctl --user start hailo-ollama)"
	}
	return line
}

/** hailoHint prints the stopped-service hint, if there is one, as a single dim
 * line. Harvey does not start hailo-ollama itself; it says how.
 *
 * Parameters:
 *   out (io.Writer) — destination for the hint.
 *
 * Example:
 *   a.hailoHint(out)
 */
func (a *Agent) hailoHint(out io.Writer) {
	st := DetectHailo(a.Config.Hailo, a.hailoDevicePath)
	if line := hailoHintLine(st, a.Config.Hailo, a.hailoUnitExists()); line != "" {
		fmt.Fprintln(out, dim("  "+line))
	}
}
