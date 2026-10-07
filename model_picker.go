// Package harvey — model_picker.go implements the unified /model use picker
// that aggregates all locally available models across llamafile, llama.cpp,
// and Ollama backends.
package harvey

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

/** aggregateModels collects all locally available models across all backends.
 * Llamafile and llama.cpp models are discovered by scanning their model
 * directories. Ollama models are queried live only when Ollama is reachable;
 * when Ollama is down no error is returned and its models are simply absent.
 *
 * Parameters:
 *   a (*Agent) — the running Harvey agent.
 *
 * Returns:
 *   []ModelSummary — all models found, backends in order: llamafile, llamacpp, ollama.
 *   error          — non-nil only on unexpected filesystem errors.
 *
 * Example:
 *   models, err := aggregateModels(a)
 *   for _, m := range models { fmt.Println(m.Name, m.Engine) }
 */
func aggregateModels(a *Agent) ([]ModelSummary, error) {
	var all []ModelSummary

	agentsDir := ""
	if a.Workspace != nil {
		agentsDir = a.Workspace.Root + "/agents"
	}

	workspaceRoot := ""
	if a.Workspace != nil {
		workspaceRoot = a.Workspace.Root
	}

	// Llamafile models — disk scan of ModelsDir (default ~/Models) for *.llamafile files.
	lb := NewLlamafileBackend(a.Config, agentsDir, workspaceRoot)
	if models, err := lb.ListModels(); err == nil {
		all = append(all, models...)
	}

	// llama.cpp *.gguf models — disk scan of ModelsDir (default ~/Models).
	cb := NewLlamaCppBackend(a.Config, agentsDir)
	if models, err := cb.ListModels(); err == nil {
		all = append(all, models...)
	}

	// Ollama — live query, silent if unreachable.
	if ProbeOllama(a.Config.Ollama.URL) {
		if summaries, err := NewOllamaClient(a.Config.Ollama.URL, "").ModelSummaries(context.Background()); err == nil {
			for _, s := range summaries {
				all = append(all, ModelSummary{
					Name:   s.Name,
					Engine: "ollama",
				})
			}
		}
	}

	return all, nil
}

/** pickAndUseModel presents a combined numbered list of all locally available
 * models across llamafile, llama.cpp, and Ollama. When the user picks an
 * unaliased model, promptLazyRegister offers to save a short alias. If a
 * different backend is currently active, a warn-and-switch message is printed
 * and the outgoing server is stopped when Harvey owns it.
 *
 * Parameters:
 *   a   (*Agent)   — the running Harvey agent.
 *   out (io.Writer) — output sink.
 *
 * Returns:
 *   error — on unexpected failures.
 *
 * Example:
 *   err := pickAndUseModel(a, os.Stdout)
 */
func pickAndUseModel(a *Agent, out io.Writer) error {
	models, err := aggregateModels(a)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return Negativef("no models found. Install a llamafile, *.gguf, or an Ollama model")
	}

	// Build display items.
	items := make([]SelectItem, len(models))
	activeEngine := ""
	activeModel := ""
	if a.Backend != nil {
		activeEngine = a.Backend.Name()
		activeModel = a.Backend.ActiveModel()
	}

	for i, m := range models {
		label := fmt.Sprintf("%-40s [%s]", m.Name, m.Engine)
		if m.Path != "" {
			label = fmt.Sprintf("%-40s %-36s [%s]", m.Name, shortenPath(m.Path), m.Engine)
		}
		active := m.Engine == activeEngine && strings.EqualFold(m.Name, activeModel)
		// Values are 1-based so "0" remains the unambiguous cancel sentinel.
		items[i] = SelectItem{Value: fmt.Sprintf("%d", i+1), Label: label, Active: active}
	}

	chosen, err := SelectFrom(items, fmt.Sprintf("Select model [1-%d, 0=cancel]: ", len(items)), a.In, out)
	if err != nil || chosen == "" || chosen == "0" {
		return err
	}

	// Map chosen back to a model (1-based index).
	idx := -1
	fmt.Sscanf(chosen, "%d", &idx)
	if idx < 1 || idx > len(models) {
		return nil
	}
	return useSelectedModel(a, models[idx-1], out, true)
}

/** listLocalModels is the source of the local-model list that /model use NAME
 * searches. It is a variable so a test can stand in for the disk scan and the
 * live Ollama query.
 */
var listLocalModels = aggregateModels

/** matchModel finds the local model a user meant by name. An exact name
 * (ignoring case) wins over a prefix; a name that matches exactly one model on
 * one engine, or is a prefix of exactly one, selects it. A name that matches
 * several (the same name on two engines, or a prefix shared by two models) selects
 * nothing and returns the candidates, so the caller can list them.
 *
 * Parameters:
 *   models ([]ModelSummary) — the local models, as aggregateModels returns them.
 *   query  (string)          — what the user typed after /model use.
 *
 * Returns:
 *   ModelSummary   — the selected model when ok is true.
 *   []ModelSummary — the candidates when the query was ambiguous, else nil.
 *   bool           — true when exactly one model was selected.
 *
 * Example:
 *   m, ambiguous, ok := matchModel(models, "apert")
 */
func matchModel(models []ModelSummary, query string) (ModelSummary, []ModelSummary, bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return ModelSummary{}, nil, false
	}
	var exact, prefix []ModelSummary
	for _, m := range models {
		name := strings.ToLower(m.Name)
		switch {
		case name == q:
			exact = append(exact, m)
		case strings.HasPrefix(name, q):
			prefix = append(prefix, m)
		}
	}
	for _, group := range [][]ModelSummary{exact, prefix} {
		switch len(group) {
		case 0:
			continue
		case 1:
			return group[0], nil, true
		default:
			return ModelSummary{}, group, false
		}
	}
	return ModelSummary{}, nil, false
}

/** useSelectedModel switches Harvey to one model from the local list: it warns
 * and stops the outgoing server when the engine changes, optionally offers to save
 * a short alias, wires the backend, and confirms. The picker and /model use NAME
 * share it.
 *
 * Parameters:
 *   a          (*Agent)       — the running Harvey agent.
 *   selected   (ModelSummary) — the model to use.
 *   out        (io.Writer)    — output sink.
 *   offerAlias (bool)         — prompt to save an alias for an unaliased model; the
 *                               picker does, a model named directly does not.
 *
 * Returns:
 *   error — when the backend cannot be started or the engine is unknown.
 *
 * Example:
 *   err := useSelectedModel(a, ModelSummary{Name: "llama3.2:3b", Engine: "ollama"}, out, false)
 */
func useSelectedModel(a *Agent, selected ModelSummary, out io.Writer, offerAlias bool) error {
	// Warn and switch when engines differ.
	if a.Backend != nil && a.Backend.Name() != selected.Engine {
		fmt.Fprintf(out, "  Switching from %s (%s) → %s (%s)\n",
			a.Backend.Name(), a.Backend.ActiveModel(), selected.Engine, selected.Name)
		if a.Backend.StartedByHarvey() {
			_ = a.Backend.Stop()
		}
		a.Backend = nil
	}

	// Lazy alias registration.
	displayName := selected.Name
	if offerAlias {
		if alias, _ := promptLazyRegister(a, selected, out); alias != "" {
			displayName = alias
		}
	}

	// Wire the backend.
	switch selected.Engine {
	case "ollama":
		a.setOllamaModel(selected.Name) // probes and caches the model's capabilities
		if a.ModelCache != nil {
			if cap, _ := a.ModelCache.Get(modelKey(selected.Engine, selected.Name)); cap != nil && cap.ProbeLevel == "fast" {
				fmt.Fprintf(out, "  Probed: tools=%s  embed=%s  ctx=%d\n",
					cap.SupportsTools, cap.SupportsEmbed, cap.ContextLength)
			}
		}
	case "llamafile":
		if err := switchLlamafileModel(a, selected.Name, selected.Path, out); err != nil {
			return err
		}
	case "llamacpp":
		if err := startLlamaCppModelPath(a, selected.Path, out); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown engine %q", selected.Engine)
	}

	fmt.Fprintf(out, "  %s Using model: %s\n", green("✓"), cyan(displayName))
	return nil
}

/** promptLazyRegister checks whether item is already aliased in ModelAliases.
 * If not, it prompts the user for a short alias name and optional
 * comma-separated tags, then saves the alias. Pressing Enter with no input
 * skips registration.
 *
 * Parameters:
 *   a    (*Agent)     — the running Harvey agent.
 *   item (ModelSummary) — the model being registered.
 *   out  (io.Writer)  — output sink.
 *
 * Returns:
 *   string — the alias name used (existing or newly created), or "" when skipped.
 *   error  — on save failure.
 *
 * Example:
 *   alias, err := promptLazyRegister(a, selected, os.Stdout)
 */
func promptLazyRegister(a *Agent, item ModelSummary, out io.Writer) (string, error) {
	// Check if already aliased. A legacy alias (Engine=="") matches any engine
	// for backward compatibility. An alias with an explicit engine only matches
	// when both the model name and engine agree — preventing same-named models
	// on different backends from sharing an alias.
	for name, entry := range a.Config.ModelAliases {
		if strings.EqualFold(entry.Model, item.Name) {
			if entry.Engine == "" || strings.EqualFold(entry.Engine, item.Engine) {
				return name, nil
			}
		}
	}

	// Prompt for optional alias.
	fmt.Fprintf(out, "  Save alias for %q? Enter short name (or press Enter to skip): ", item.Name)
	line := readLineFrom(a.In)
	alias := strings.TrimSpace(line)
	if alias == "" {
		return "", nil
	}
	alias = strings.ToLower(alias)

	fmt.Fprint(out, "  Tags (comma-separated, or Enter to skip): ")
	tagLine := readLineFrom(a.In)
	var tags []string
	for _, t := range strings.Split(tagLine, ",") {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			tags = append(tags, t)
		}
	}

	if a.Config.ModelAliases == nil {
		a.Config.ModelAliases = make(map[string]ModelAlias)
	}
	a.Config.ModelAliases[alias] = ModelAlias{Model: item.Name, Engine: item.Engine, Tags: tags}
	if a.Workspace != nil {
		_ = SaveModelAliases(a.Workspace, a.Config)
	}
	tagStr := ""
	if len(tags) > 0 {
		tagStr = " [" + strings.Join(tags, ", ") + "]"
	}
	fmt.Fprintf(out, "  Alias saved: %s → %s%s\n", alias, item.Name, tagStr)

	return alias, nil
}

// readLineFrom reads one line from r, returning the content without the newline.
// Returns "" on EOF or error.
func readLineFrom(r io.Reader) string {
	if r == nil {
		return ""
	}
	buf := make([]byte, 1)
	var sb strings.Builder
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			sb.WriteByte(buf[0])
		}
		if err != nil {
			break
		}
	}
	return sb.String()
}

// shortenPath replaces the home directory prefix with "~" for display.
func shortenPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

// oneByteReader hands out at most one byte per Read, however large the caller's
// buffer, so a bufio.Reader wrapped around it never reads ahead.
type oneByteReader struct{ r io.Reader }

func (o oneByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return o.r.Read(p[:1])
}

/** newLineReader returns a bufio.Reader over r that consumes exactly the bytes it is
 * asked for and no more. bufio's smallest buffer is 16 bytes, so bufio.NewReaderSize(r, 1)
 * still reads ahead, and after a prompt it swallowed the start of the next line of piped
 * input, which the REPL's line editor then never saw. Here the source returns one byte per
 * Read, so the buffer can only ever hold what one Read returned.
 *
 * Parameters:
 *   r (io.Reader) — the input, normally os.Stdin.
 *
 * Returns:
 *   *bufio.Reader — a reader for startup yes/no prompts and confirmations.
 *
 * Example:
 *   reader := newLineReader(os.Stdin)
 *   answer, _ := reader.ReadString('\n')
 */
func newLineReader(r io.Reader) *bufio.Reader {
	return bufio.NewReaderSize(oneByteReader{r}, 1)
}
