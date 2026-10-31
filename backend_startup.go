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
	"time"
)

/** probeActiveBackend returns true if the currently configured backend server
 * is reachable. Probes OllamaURL when OllamaModel is set; returns false when neither is configured.
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
	if a.Config.Ollama.Model != "" {
		return ProbeOllama(a.Config.Ollama.URL)
	}
	return false
}

/** selectBackend runs the interactive startup sequence to choose a backend.
 * Ollama (or hailo-ollama) is probed and offered. preferredModel hints at which
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
	// A Hailo card whose server is stopped: say so once, before choosing a backend.
	a.hailoHint(out)

	family := a.ollamaFamily()
	if family.Relabelled {
		fmt.Fprintf(out, "\n  Checking hailo-ollama at %s...\n", a.Config.Ollama.URL)
	} else {
		fmt.Fprintf(out, "\n  Checking Ollama at %s...\n", a.Config.Ollama.URL)
	}

	if ProbeOllama(a.Config.Ollama.URL) {
		if family.Relabelled {
			fmt.Fprintln(out, green("  ✓")+" hailo-ollama is running")
		} else {
			fmt.Fprintln(out, green("  ✓")+" Ollama is running")
			if m := os.Getenv("OLLAMA_MODELS"); m != "" {
				fmt.Fprintf(out, dim("  ⚠ Ollama was already running — OLLAMA_MODELS=%s may not be in effect.\n"), m)
				fmt.Fprintln(out, dim("    Stop Ollama, then restart Harvey to apply ollama.env settings."))
			}
		}
		a.noteRelabelledServer(out)
		return a.pickOllamaModel(reader, out, preferredModel)
	}

	fmt.Fprintln(out, yellow("  ✗")+" Ollama is not running")

	// Ollama is down but a Hailo server answers: offer its models instead of
	// asking to start Ollama.
	if family.HailoURL != "" {
		fmt.Fprintf(out, green("  ✓")+" hailo-ollama is running at %s\n", family.HailoURL)
		return a.pickOllamaModel(reader, out, preferredModel)
	}

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
		fmt.Fprintln(out, dim("  No backend connected — use /model use once inside."))
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
	if flag := a.Config.Ollama.Model; flag != "" {
		if err := a.useStartupModel(flag, out); err != nil {
			return err
		}
		fmt.Fprintf(out, "  Using model: %s\n", cyan(a.Config.Ollama.Model))
		return nil
	}

	// With a Hailo server in play the models of both engines are offered together.
	if family := a.listFamilyModels(); hasEngine(family, "hailo") {
		return a.pickFamilyModel(reader, out, preferredModel, family)
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

// useOllamaClient makes model the active Ollama model: it sets
// Config.Ollama.Model, installs a new client carrying the debug log, and
// records the switch in the session transcript. Every path that changes the
// Ollama model goes through here, so none of them can drop the debug log or
// leave the recorder naming the previous model. At startup the debug log and
// recorder are not open yet, so both are no-ops there.
func (a *Agent) useOllamaClient(model string) {
	a.useModelAt("ollama", a.Config.Ollama.URL, model)
}

// useModelAt is useOllamaClient for any Ollama-shaped server: url is the
// server the client talks to, engine the label the switch is recorded under.
func (a *Agent) useModelAt(engine, url, model string) {
	a.Config.Ollama.Model = model
	client := newOllamaLLMClient(url, model, a.Config.Ollama.Timeout)
	client.SetEngine(engine)
	client.SetWindow(a.effectiveContextLimit)
	a.Client = a.wireSessionLog(client)
	a.logModelSwitch(model, engine)
}

// setOllamaModel wires Config.Ollama.Model, Client, and Backend for the given Ollama model name.
func (a *Agent) setOllamaModel(model string) {
	// A Hailo server sitting at ollama.url is hailo-ollama, not Ollama (DR-0030).
	if a.ollamaFamily().Relabelled {
		if err := a.setHailoModel(model); err == nil {
			return
		}
	}
	agentsDir := filepath.Join(a.Workspace.Root, "agents")
	a.useOllamaClient(model)
	b := NewOllamaBackend(a.Config.Ollama.URL, a.Config.Ollama.Timeout, agentsDir)
	b.SetActiveModel(model)
	b.running = true // we know Ollama is reachable at this point
	a.Backend = b
	a.probeOllamaModelAndCache(model)
}

/** probeOllamaModelAndCache runs FastProbeModel on an Ollama model and stores
 * the result in the model cache. Every path that selects an Ollama model
 * reaches it through setOllamaModel, so the model's capabilities (tool support,
 * embedding support, context length) are known whether or not the user saved
 * an alias, and an entry left stale by an Ollama update is refreshed. A tool
 * mode the user set with /model mode is carried over: the probe itself always
 * reports ToolModeAuto. A failed probe (server unreachable, model unknown)
 * changes nothing. It is a no-op when there is no model cache.
 *
 * Parameters:
 *   model (string) — the Ollama model name, e.g. "llama3.2:latest".
 *
 * Example:
 *   a.probeOllamaModelAndCache("llama3.2:latest")
 */
func (a *Agent) probeOllamaModelAndCache(model string) {
	a.probeModelAndCache(a.Config.Ollama.URL, model)
}

// probeModelAndCache is probeOllamaModelAndCache for the server at url; the
// cache row is keyed by the active backend's engine.
func (a *Agent) probeModelAndCache(url, model string) {
	if a.ModelCache == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cap, engine, err := fastProbe(ctx, url, model)
	if err != nil {
		return
	}
	// A model in Hailo's format means the server is hailo-ollama even though
	// Harvey was not told so (no card, no hailo.url, ollama.url pointed at it):
	// the client takes the engine's capability row from here on.
	if engine == "hailo" {
		if ac, ok := a.Client.(*OllamaChatClient); ok && ac.ModelName() == model {
			ac.SetEngine(engine)
		}
	}
	cap.Name = a.modelKey(model)
	if existing, _ := a.ModelCache.Get(cap.Name); existing != nil {
		cap.ToolMode = existing.ToolMode
		// A limit the user set or one learned from a failure outranks the
		// probe's seed; the seed only fills an empty one.
		if existing.MaxPromptTokens > 0 {
			cap.MaxPromptTokens = existing.MaxPromptTokens
		}
	}
	_ = a.ModelCache.Set(cap)
}
