// Package harvey — backend_startup.go handles backend probing and the
// interactive model-selection flow that runs at session start.
package harvey

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

/** probeActiveBackend returns true if the currently configured backend server
 * is reachable. Probes the LlamafileURL when LlamafileActive is set; probes
 * OllamaURL when OllamaModel is set; returns false when neither is configured.
 *
 * Parameters:
 *   a (*Agent) — the running Harvey agent.
 *
 * Returns:
 *   bool — true if the backend responds to a health probe.
 *
 * Example:
 *   if !probeActiveBackend(a) {
 *       fmt.Fprintln(out, "Backend is not reachable.")
 *   }
 */
func probeActiveBackend(a *Agent) bool {
	if a.Config.LlamafileActive != "" {
		return ProbeLlamafile(a.Config.LlamafileURL)
	}
	if a.Config.OllamaModel != "" {
		return ProbeOllama(a.Config.OllamaURL)
	}
	return false
}

/** useLlamafileEntry wires a.Client to the running llamafile server using
 * the registry entry name as the model identifier.
 *
 * Parameters:
 *   name (string)    — registry entry name; used as the model name in API calls.
 *   out  (io.Writer) — destination for the "Using model:" status line.
 *
 * Returns:
 *   error — always nil; present for call-site uniformity with selectBackend.
 *
 * Example:
 *   return a.useLlamafileEntry("qwen-coding", out)
 */
func (a *Agent) useLlamafileEntry(name string, out io.Writer) error {
	client := newLlamafileLLMClient(a.Config.LlamafileURL+"/v1", name, a.Config.OllamaTimeout)
	if a.Config.LlamafileMaxTokens > 0 {
		client.SetMaxTokens(a.Config.LlamafileMaxTokens)
	}
	a.Client = client
	if ac, ok := a.Client.(*AnyLLMClient); ok && a.DebugLog != nil {
		ac.DebugLog = a.DebugLog
	}
	fmt.Fprintf(out, "  Using model: %s\n", cyan(name))
	if a.Recorder != nil {
		_ = a.Recorder.RecordModelSwitch(name, "llamafile")
	}
	return nil
}

/** selectBackend runs the interactive startup sequence to choose a backend.
 * When an active llamafile entry is configured it is probed first; Ollama is
 * the fallback when no active llamafile is set. preferredModel hints at which
 * Ollama model to pre-select from a prior session; pass "" when unknown.
 *
 * Parameters:
 *   reader         (*bufio.Reader) — reads user input.
 *   out            (io.Writer)     — destination for prompt and status messages.
 *   preferredModel (string)        — ALL-CAPS model name from the resumed session, or "".
 *
 * Returns:
 *   error — on unexpected read failures.
 *
 * Example:
 *   err := agent.selectBackend(reader, os.Stdout, "GEMMA4")
 */
func (a *Agent) selectBackend(reader *bufio.Reader, out io.Writer, preferredModel string) error {
	// Case 1: Active llamafile configured — probe it, offer to start if down.
	if entry := a.Config.ActiveLlamafileEntry(); entry != nil {
		absPath := resolveLlamafilePath(entry.Path, a.Workspace.Root)
		fmt.Fprintf(out, "\n  Connecting to %s (llamafile)…", entry.Name)
		if ProbeLlamafile(a.Config.LlamafileURL) {
			fmt.Fprintln(out, " "+green("✓"))
			return a.useLlamafileEntry(entry.Name, out)
		}
		fmt.Fprintln(out, " "+yellow("✗")+" not running")
		if askYesNo(reader, out, fmt.Sprintf("    Start %s now? [Y/n] ", entry.Name), true) {
			fmt.Fprintf(out, "  Connecting to %s (llamafile)…\n", entry.Name)
			proc, err := StartLlamafileService(absPath, a.Config.LlamafileURL, "", a.Config.LlamafileStartupTimeout, a.Config.LlamafileGPULayers, a.Config.ActiveLlamafileContextLength(), out)
			if err != nil {
				fmt.Fprintf(out, red("  ✗ Failed: ")+"%v\n", err)
			} else {
				a.wireLlamafileBackend(proc, entry.Name)
				fmt.Fprintf(out, "  %s Ready\n", green("✓"))
				return a.useLlamafileEntry(entry.Name, out)
			}
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, dim("  No backend selected."))
		fmt.Fprintln(out, dim("  → If Ollama is installed, use /ollama start once inside."))
		return nil
	}

	// Case 2: Registered llamafiles exist but none is active — show combined picker
	// with llamafiles first, Ollama models second.
	if len(a.Config.LlamafileModels) > 0 {
		return a.pickBackend(reader, out, preferredModel)
	}

	// Case 3: No llamafiles registered — try Ollama.
	fmt.Fprintf(out, "\n  Checking Ollama at %s...\n", a.Config.OllamaURL)

	if ProbeOllama(a.Config.OllamaURL) {
		fmt.Fprintln(out, green("  ✓")+" Ollama is running")
		if m := os.Getenv("OLLAMA_MODELS"); m != "" {
			fmt.Fprintf(out, dim("  ⚠ Ollama was already running — OLLAMA_MODELS=%s may not be in effect.\n"), m)
			fmt.Fprintln(out, dim("    Stop Ollama, then restart Harvey to apply ollama.env settings."))
		}
		return a.pickOllamaModel(reader, out, preferredModel)
	}

	fmt.Fprintln(out, yellow("  ✗")+" Ollama is not running")

	if askYesNo(reader, out, "    Start Ollama now? [Y/n] ", true) {
		PrintOllamaEnv(out)
		fmt.Fprintln(out, "  Starting Ollama...")
		if err := StartOllamaService(""); err != nil {
			fmt.Fprintf(out, red("  Failed: ")+"%v\n", err)
		} else {
			fmt.Fprintln(out, green("  ✓")+" Ollama started")
			return a.pickOllamaModel(reader, out, preferredModel)
		}
	}

	// Case 4: Nothing reachable — guide new users.
	fmt.Fprintln(out)
	if err := runFirstRunWizard(a, reader, out); err != nil {
		fmt.Fprintln(out, dim("  No backend connected — use /llamafile start or /ollama start once inside."))
	}
	return nil
}

/** pickBackend presents a combined numbered list of registered llamafile models
 * (first) and available Ollama models (second), and lets the user choose one.
 * If preferredModel matches a registered llamafile name (case-insensitive), it
 * is selected automatically without showing the picker.
 *
 * Parameters:
 *   reader         (*bufio.Reader) — reads the user's selection.
 *   out            (io.Writer)     — destination for the picker display.
 *   preferredModel (string)        — model name hint from a resumed session; "" for none.
 *
 * Returns:
 *   error — on unexpected failures starting a llamafile or listing Ollama models.
 *
 * Example:
 *   err := agent.pickBackend(reader, os.Stdout, "qwen-coding")
 */
func (a *Agent) pickBackend(reader *bufio.Reader, out io.Writer, preferredModel string) error {
	// Auto-select when preferredModel matches a registered llamafile.
	if preferredModel != "" {
		for i := range a.Config.LlamafileModels {
			e := &a.Config.LlamafileModels[i]
			if strings.EqualFold(e.Name, preferredModel) {
				return a.startAndUseLlamafile(e, out)
			}
		}
	}

	type option struct {
		label string
		kind  string // "llamafile" or "ollama"
		name  string
	}
	var opts []option

	for _, e := range a.Config.LlamafileModels {
		size := ""
		if absPath := resolveLlamafilePath(e.Path, a.Workspace.Root); absPath != "" {
			if info, err := os.Stat(absPath); err == nil {
				size = " (" + llamafileFormatBytes(info.Size()) + ")"
			}
		}
		opts = append(opts, option{
			label: e.Name + size + dim(" (llamafile)"),
			kind:  "llamafile",
			name:  e.Name,
		})
	}

	if ProbeOllama(a.Config.OllamaURL) {
		if summaries, err := NewOllamaClient(a.Config.OllamaURL, "").ModelSummaries(context.Background()); err == nil {
			for _, s := range summaries {
				opts = append(opts, option{
					label: s.Name + dim(" (ollama)"),
					kind:  "ollama",
					name:  s.Name,
				})
			}
		}
	}

	if len(opts) == 0 {
		fmt.Fprintln(out)
		return runFirstRunWizard(a, reader, out)
	}

	fmt.Fprintln(out, "\n  Available models:")
	for i, o := range opts {
		marker := "  "
		if preferredModel != "" && strings.EqualFold(o.name, preferredModel) {
			marker = "* "
		}
		fmt.Fprintf(out, "  %s[%d] %s\n", marker, i+1, o.label)
	}
	fmt.Fprintf(out, "    Select model [1-%d, 0=none, default=1]: ", len(opts))

	line, _ := reader.ReadString('\n')
	idx := 1
	if trimmed := strings.TrimSpace(line); trimmed != "" {
		fmt.Sscanf(trimmed, "%d", &idx)
	}
	if idx < 1 || idx > len(opts) {
		fmt.Fprintln(out, dim("  No backend selected."))
		return nil
	}

	chosen := opts[idx-1]
	if chosen.kind == "llamafile" {
		entry := a.Config.LlamafileEntryByName(chosen.name)
		if entry == nil {
			return fmt.Errorf("pickBackend: entry %q not found", chosen.name)
		}
		return a.startAndUseLlamafile(entry, out)
	}
	// Ollama model.
	a.setOllamaModel(chosen.name)
	fmt.Fprintf(out, "  Using model: %s\n", cyan(chosen.name))
	return nil
}

// startAndUseLlamafile starts the llamafile server for entry (if not already
// running) and wires it as the active client. When a server is already running
// at LlamafileURL but was not started by Harvey, it probes /v1/models to
// identify the actual model and adopts it — using the detected name rather
// than entry.Name when they differ. On start failure the error is printed and
// returned.
func (a *Agent) startAndUseLlamafile(entry *LlamafileEntry, out io.Writer) error {
	if ProbeLlamafile(a.Config.LlamafileURL) {
		// A server is already running — probe which model it is actually serving.
		detectedName := probeRunningLlamafileName(a.Config.LlamafileURL)
		useName := entry.Name
		if detectedName != "" && !strings.EqualFold(detectedName, entry.Name) {
			fmt.Fprintf(out, "  Server at %s is serving %q (configured: %q) — adopting detected model.\n",
				a.Config.LlamafileURL, detectedName, entry.Name)
			useName = detectedName
		} else {
			fmt.Fprintf(out, "  Connecting to %s (llamafile)… %s\n", useName, green("✓"))
		}
		return a.useLlamafileEntry(useName, out)
	}
	absPath := resolveLlamafilePath(entry.Path, a.Workspace.Root)
	fmt.Fprintf(out, "  Connecting to %s (llamafile)…\n", entry.Name)
	proc, err := StartLlamafileService(absPath, a.Config.LlamafileURL, "", a.Config.LlamafileStartupTimeout, a.Config.LlamafileGPULayers, a.Config.ActiveLlamafileContextLength(), out)
	if err != nil {
		fmt.Fprintf(out, red("  ✗ Failed: ")+"%v\n", err)
		return err
	}
	if a.Backend != nil {
		_ = a.Backend.Stop()
	}
	a.wireLlamafileBackend(proc, entry.Name)
	fmt.Fprintf(out, "  %s Ready\n", green("✓"))
	return a.useLlamafileEntry(entry.Name, out)
}

/** pickOllamaModel selects a model from the running Ollama server.
 * If preferredModel is non-empty and matches an installed model (case-insensitive
 * prefix match against the ALL-CAPS form), that model is used automatically.
 * If preferredModel is not available, the full list is shown with the preferred
 * name noted. A command-line --model flag always takes precedence.
 *
 * Parameters:
 *   reader         (*bufio.Reader) — reads the user's model selection.
 *   out            (io.Writer)     — destination for the model list prompt.
 *   preferredModel (string)        — ALL-CAPS model name hint; "" for no preference.
 *
 * Returns:
 *   error — on unexpected failures listing models.
 *
 * Example:
 *   err := agent.pickOllamaModel(reader, os.Stdout, "GEMMA4")
 */
func (a *Agent) pickOllamaModel(reader *bufio.Reader, out io.Writer, preferredModel string) error {
	// Command-line --model flag always wins.
	if a.Config.OllamaModel != "" {
		a.setOllamaModel(a.Config.OllamaModel)
		fmt.Fprintf(out, "  Using model: %s\n", cyan(a.Config.OllamaModel))
		return nil
	}

	summaries, err := NewOllamaClient(a.Config.OllamaURL, "").ModelSummaries(context.Background())
	if err != nil || len(summaries) == 0 {
		fmt.Fprintln(out, yellow("  ✗")+" No models installed. Run: ollama pull <model>")
		return nil
	}

	models := make([]string, len(summaries))
	for i, s := range summaries {
		models[i] = s.Name
	}

	// If only one model is available, use it regardless of preference.
	if len(models) == 1 {
		a.setOllamaModel(models[0])
		fmt.Fprintf(out, "  Using model: %s\n", cyan(models[0]))
		return nil
	}

	// Try to match the preferred model against the available list.
	if preferredModel != "" {
		for _, m := range models {
			if strings.EqualFold(extractModelName(m), preferredModel) ||
				strings.EqualFold(m, preferredModel) {
				a.setOllamaModel(m)
				fmt.Fprintf(out, "  Using model: %s %s\n", cyan(m), dim("(from session)"))
				return nil
			}
		}
		// Preferred model not found — fall through to interactive picker with a note.
		fmt.Fprintf(out, dim("  Session model %q not found; select from available:\n"), preferredModel)
	}

	fmt.Fprintln(out, "  Available models:")
	ollamaModelTable(a, summaries, out, true)
	fmt.Fprintf(out, "    Select model [1-%d, default=1]: ", len(models))
	line, _ := reader.ReadString('\n')
	chosen := models[0]
	idx := 0
	fmt.Sscanf(strings.TrimSpace(line), "%d", &idx)
	if idx >= 1 && idx <= len(models) {
		chosen = models[idx-1]
	}

	a.setOllamaModel(chosen)
	fmt.Fprintf(out, "  Using model: %s\n", cyan(chosen))
	return nil
}

// setOllamaModel wires Config.OllamaModel, Client, and Backend for the given Ollama model name.
func (a *Agent) setOllamaModel(model string) {
	agentsDir := filepath.Join(a.Workspace.Root, "agents")
	a.Config.OllamaModel = model
	a.Client = newOllamaLLMClient(a.Config.OllamaURL, model, a.Config.OllamaTimeout)
	b := NewOllamaBackend(a.Config.OllamaURL, a.Config.OllamaTimeout, agentsDir)
	b.SetActiveModel(model)
	b.running = true // we know Ollama is reachable at this point
	a.Backend = b
}
