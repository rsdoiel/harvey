// Package harvey — model_picker.go implements the unified /model use picker
// that aggregates all locally available models across llamafile, llama.cpp,
// and Ollama backends.
package harvey

import (
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
		fmt.Fprintln(out, "  No models found. Install a llamafile, *.gguf, or an Ollama model.")
		return nil
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
	selected := models[idx-1]

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
	alias, _ := promptLazyRegister(a, selected, out)
	displayName := selected.Name
	if alias != "" {
		displayName = alias
	}

	// Wire the backend.
	switch selected.Engine {
	case "ollama":
		a.setOllamaModel(selected.Name)
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

	// Auto-probe Ollama models on alias creation so capability data is cached
	// immediately. This replaces the separate /ollama probe command.
	if item.Engine == "ollama" && a.ModelCache != nil {
		ctx := context.Background()
		if cap, err := FastProbeModel(ctx, a.Config.Ollama.URL, item.Name); err == nil {
			_ = a.ModelCache.Set(cap)
			fmt.Fprintf(out, "  Probed: tools=%s  embed=%s  ctx=%d\n",
				cap.SupportsTools, cap.SupportsEmbed, cap.ContextLength)
		}
	}

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
