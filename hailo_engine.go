package harvey

import (
	"bufio"
	"context"
	"fmt"
	"io"
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
 *
 * Example:
 *   f := a.ollamaFamily()
 *   if f.HailoURL != "" { fmt.Println("Hailo at", f.HailoURL) }
 */
type ServerFamily struct {
	OllamaURL  string
	HailoURL   string
	Relabelled bool
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
	} else if fetchHailoCatalog(a.Config.Ollama.URL) != nil {
		f.HailoURL = a.Config.Ollama.URL
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
		fmt.Fprintf(out, "  [%d] %-40s %s\n", i+1, m.Name, dim("["+m.Engine+"]"))
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
