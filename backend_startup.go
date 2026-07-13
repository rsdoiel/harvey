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
	if a.Config.Llamafile.Active != "" {
		return ProbeLlamafile(a.Config.Llamafile.URL)
	}
	if a.Config.Ollama.Model != "" {
		return ProbeOllama(a.Config.Ollama.URL)
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
	client := newLlamafileLLMClient(a.Config.Llamafile.URL+"/v1", name, a.Config.Ollama.Timeout)
	if a.Config.Llamafile.MaxTokens > 0 {
		client.SetMaxTokens(a.Config.Llamafile.MaxTokens)
	}
	a.Client = client
	if ac, ok := a.Client.(*AnyLLMClient); ok && a.DebugLog != nil {
		ac.DebugLog = a.DebugLog
	}
	// Keep Active in sync so activeModelLabel and effectiveContextLimit are correct.
	a.Config.Llamafile.Active = name
	fmt.Fprintf(out, "  Using model: %s\n", cyan(name))
	if a.Recorder != nil {
		_ = a.Recorder.RecordModelSwitch(name, "llamafile")
	}
	probeLlamaCppAndCache(a, name, a.Config.Llamafile.URL)
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
	// Case 0: llama.cpp models dir has *.gguf files — probe server, offer to start.
	if len(a.Config.LlamaCpp.ModelsDir) > 0 || a.Config.LlamaCpp.URL != "" {
		agentsDir := filepath.Join(a.Workspace.Root, "agents")
		lb := NewLlamaCppBackend(a.Config, agentsDir)
		if lb.Detect() {
			a.Backend = lb
			// Wire a client using the first model name we can detect.
			if models, err := lb.ListModels(); err == nil && len(models) > 0 {
				lb.activeModel = models[0].Name
			}
			if lb.activeModel != "" {
				client, err := lb.NewClient()
				if err == nil {
					a.Client = client
					fmt.Fprintf(out, "  Using llama-server at %s (model: %s)\n", lb.BaseURL(), cyan(lb.ActiveModel()))
					return nil
				}
			}
		}
	}

	// Case 2: Registered llamafiles exist — show combined picker
	// with llamafiles first, Ollama models second.
	if len(a.Config.Llamafile.Models) > 0 {
		return a.pickBackend(reader, out, preferredModel)
	}

	// Case 3: No llamafiles registered — try Ollama.
	fmt.Fprintf(out, "\n  Checking Ollama at %s...\n", a.Config.Ollama.URL)

	if ProbeOllama(a.Config.Ollama.URL) {
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
		for i := range a.Config.Llamafile.Models {
			e := &a.Config.Llamafile.Models[i]
			if strings.EqualFold(e.Name, preferredModel) {
				return a.startAndUseLlamafile(e, out)
			}
		}
	}

	type option struct {
		label         string
		kind          string // "llamafile", "llamacpp", or "ollama"
		name          string
		path          string // resolved absolute path; llamafile/llamacpp options only
		contextLength int
	}
	var opts []option
	seenPaths := make(map[string]bool)

	for _, e := range a.Config.Llamafile.Models {
		absPath := resolveLlamafilePath(e.Path, a.Workspace.Root)
		size := ""
		if absPath != "" {
			if info, err := os.Stat(absPath); err == nil {
				size = " (" + llamafileFormatBytes(info.Size()) + ")"
			}
			seenPaths[absPath] = true
		}
		opts = append(opts, option{
			label:         e.Name + size + dim(" (llamafile)"),
			kind:          "llamafile",
			name:          e.Name,
			path:          absPath,
			contextLength: e.ContextLength,
		})
	}

	// Llamafiles found on disk but not yet registered — the startup picker
	// must reflect everything in the models directory, not just the subset
	// a prior session happened to register.
	agentsDir := ""
	if a.Workspace != nil {
		agentsDir = filepath.Join(a.Workspace.Root, "agents")
	}
	lb := NewLlamafileBackend(a.Config, agentsDir, a.Workspace.Root)
	if diskModels, err := lb.ListModels(); err == nil {
		for _, m := range diskModels {
			if seenPaths[m.Path] {
				continue
			}
			seenPaths[m.Path] = true
			size := ""
			if m.SizeBytes > 0 {
				size = " (" + llamafileFormatBytes(m.SizeBytes) + ")"
			}
			opts = append(opts, option{
				label: m.Name + size + dim(" (llamafile)"),
				kind:  "llamafile",
				name:  m.Name,
				path:  m.Path,
			})
		}
	}

	// llama.cpp *.gguf models found on disk. Unlike llamafile, .gguf files are
	// never pre-registered — this disk scan is the only source for them, so
	// without it they never appear in the startup picker at all (the bug this
	// fixes: TODO.md, "gguf models are not listed as an option").
	cb := NewLlamaCppBackend(a.Config, agentsDir)
	if ggufModels, err := cb.ListModels(); err == nil {
		for _, m := range ggufModels {
			if seenPaths[m.Path] {
				continue
			}
			seenPaths[m.Path] = true
			size := ""
			if m.SizeBytes > 0 {
				size = " (" + llamafileFormatBytes(m.SizeBytes) + ")"
			}
			opts = append(opts, option{
				label: m.Name + size + dim(" (llamacpp)"),
				kind:  "llamacpp",
				name:  m.Name,
				path:  m.Path,
			})
		}
	}

	if ProbeOllama(a.Config.Ollama.URL) {
		if summaries, err := NewOllamaClient(a.Config.Ollama.URL, "").ModelSummaries(context.Background()); err == nil {
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
	switch chosen.kind {
	case "llamafile":
		// Use the exact path tied to the option the user picked — never a
		// name-based registry relookup, which can resolve to a different
		// file when two on-disk models share a display name.
		entry := &LlamafileEntry{Name: chosen.name, Path: chosen.path, ContextLength: chosen.contextLength}
		return a.startAndUseLlamafile(entry, out)
	case "llamacpp":
		return startLlamaCppModelPath(a, chosen.path, out)
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
	if ProbeLlamafile(a.Config.Llamafile.URL) {
		// A server is already running — probe which model it is actually serving.
		detectedName := probeRunningLlamafileName(a.Config.Llamafile.URL)
		useName := entry.Name
		if detectedName != "" && !strings.EqualFold(detectedName, entry.Name) {
			fmt.Fprintf(out, "  Server at %s is serving %q (configured: %q) — adopting detected model.\n",
				a.Config.Llamafile.URL, detectedName, entry.Name)
			useName = detectedName
		} else {
			fmt.Fprintf(out, "  Connecting to %s (llamafile)… %s\n", useName, green("✓"))
		}
		if err := a.useLlamafileEntry(useName, out); err != nil {
			return err
		}
		// Register a matching entry for an adopted server when none exists yet
		// — same gap class as the already-fixed adoptExternalServer
		// (DECISIONS.md 2026-07-05): without this, effectiveContextLimit()
		// has no ContextLength to fall back on for the rest of the session.
		// Path is left empty, matching adoptExternalServer's own precedent —
		// the adopted server's actual model file path is unknown.
		if a.Config.LlamafileEntryByName(useName) == nil {
			newEntry := LlamafileEntry{Name: useName}
			if ctx := ProbeLlamafileContextLength(a.Config.Llamafile.URL); ctx > 0 {
				newEntry.ContextLength = ctx
			}
			a.Config.AddOrUpdateLlamafileEntry(newEntry)
			if err := SaveLlamafileConfig(a.Workspace, a.Config); err != nil {
				fmt.Fprintf(out, yellow("  ⚠ Could not save config: %v\n"), err)
			}
		}
		return nil
	}
	absPath := resolveLlamafilePath(entry.Path, a.Workspace.Root)
	fmt.Fprintf(out, "  Connecting to %s (llamafile)…\n", entry.Name)
	proc, err := StartLlamafileService(absPath, a.Config.Llamafile.URL, "", a.Config.Llamafile.StartupTimeout, a.Config.Llamafile.GPULayers, a.Config.ActiveLlamafileContextLength(), out)
	if err != nil {
		fmt.Fprintf(out, red("  ✗ Failed: ")+"%v\n", err)
		return err
	}
	if a.Backend != nil {
		_ = a.Backend.Stop()
	}
	a.wireLlamafileBackend(proc, entry.Name)
	fmt.Fprintf(out, "  %s Ready\n", green("✓"))
	if err := a.useLlamafileEntry(entry.Name, out); err != nil {
		return err
	}
	// Register a model picked straight from a disk scan (no registry entry
	// yet) so it persists across sessions and gets a real context-length
	// once probed, same as switchLlamafileModel does for /model use.
	if a.Config.LlamafileEntryByName(entry.Name) == nil {
		ctxLen := entry.ContextLength
		if ctx := ProbeLlamafileContextLength(a.Config.Llamafile.URL); ctx > 0 {
			ctxLen = ctx
		}
		a.Config.AddOrUpdateLlamafileEntry(LlamafileEntry{Name: entry.Name, Path: absPath, ContextLength: ctxLen})
		if err := SaveLlamafileConfig(a.Workspace, a.Config); err != nil {
			fmt.Fprintf(out, yellow("  ⚠ Could not save config: %v\n"), err)
		}
	}
	return nil
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
	if a.Config.Ollama.Model != "" {
		a.setOllamaModel(a.Config.Ollama.Model)
		fmt.Fprintf(out, "  Using model: %s\n", cyan(a.Config.Ollama.Model))
		return nil
	}

	summaries, err := NewOllamaClient(a.Config.Ollama.URL, "").ModelSummaries(context.Background())
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

/** tryAdoptPriorBackend reads the PID file left by a previous Harvey session.
 * If the recorded process is still alive and the backend URL is reachable, it
 * reconstructs the appropriate ManagedBackend, wires a.Backend and a.Client,
 * and returns true (the caller should skip selectBackend). If the process is
 * dead or the URL is unreachable, the stale PID file is deleted and the method
 * returns false.
 *
 * Parameters:
 *   agentsDir (string)   — directory where the PID file lives.
 *   out       (io.Writer) — status messages sink.
 *
 * Returns:
 *   bool — true when adoption succeeded and selectBackend should be skipped.
 *
 * Example:
 *   if adopted := a.tryAdoptPriorBackend(agentsDir, out); !adopted {
 *       _ = a.selectBackend(reader, out, "")
 *   }
 */
func (a *Agent) tryAdoptPriorBackend(agentsDir string, out io.Writer) bool {
	pid, err := readPIDFile(agentsDir)
	if err != nil {
		return false // no PID file, nothing to adopt
	}

	if !probeOwnedProcess(pid) {
		fmt.Fprintf(out, dim("  Prior %s server (PID %d) is gone — cleaning up.\n"), pid.Backend, pid.PID)
		_ = deletePIDFile(agentsDir)
		return false
	}

	// Process is alive — check if the URL is still reachable.
	reachable := false
	switch pid.Backend {
	case "llamafile":
		reachable = ProbeLlamafile(pid.URL)
	case "llamacpp":
		reachable = probeLlamaCpp(pid.URL)
	case "ollama":
		reachable = ProbeOllama(pid.URL)
	}

	if !reachable {
		fmt.Fprintf(out, dim("  Prior %s server (PID %d) is no longer reachable — cleaning up.\n"), pid.Backend, pid.PID)
		_ = deletePIDFile(agentsDir)
		return false
	}

	// Reconstruct the backend and wire it without restarting the server.
	switch pid.Backend {
	case "llamafile":
		lb := NewLlamafileBackend(a.Config, agentsDir, a.Workspace.Root)
		lb.activeModel = pid.Model
		lb.running = true
		a.Backend = lb
		client := newLlamafileLLMClient(pid.URL+"/v1", pid.Model, a.Config.Ollama.Timeout)
		a.Client = client
	case "llamacpp":
		lb := NewLlamaCppBackend(a.Config, agentsDir)
		lb.activeModel = pid.Model
		lb.running = true
		a.Backend = lb
		client := newLlamafileLLMClient(pid.URL+"/v1", pid.Model, 0)
		a.Client = client
	case "ollama":
		a.setOllamaModel(pid.Model)
	default:
		_ = deletePIDFile(agentsDir)
		return false
	}

	fmt.Fprintf(out, green("✓")+" Re-adopted prior %s session (PID %d, model: %s)\n",
		pid.Backend, pid.PID, cyan(pid.Model))
	return true
}

// setOllamaModel wires Config.Ollama.Model, Client, and Backend for the given Ollama model name.
func (a *Agent) setOllamaModel(model string) {
	agentsDir := filepath.Join(a.Workspace.Root, "agents")
	a.Config.Ollama.Model = model
	a.Client = newOllamaLLMClient(a.Config.Ollama.URL, model, a.Config.Ollama.Timeout)
	b := NewOllamaBackend(a.Config.Ollama.URL, a.Config.Ollama.Timeout, agentsDir)
	b.SetActiveModel(model)
	b.running = true // we know Ollama is reachable at this point
	a.Backend = b
}
