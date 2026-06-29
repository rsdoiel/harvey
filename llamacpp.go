// Package harvey — llamacpp.go implements the /llamacpp command that manages
// the llama.cpp (llama-server) backend.
package harvey

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

/** cmdLlamaCpp is the entry point for the /llamacpp command.
 * Subcommands: status, list, start PATH, stop, drop NAME.
 *
 * Parameters:
 *   a    (*Agent)   — the running Harvey agent.
 *   args ([]string) — subcommand and optional arguments.
 *   out  (io.Writer) — output sink.
 *
 * Returns:
 *   error — on unexpected failures.
 *
 * Example:
 *   err := cmdLlamaCpp(a, []string{"status"}, os.Stdout)
 */
func cmdLlamaCpp(a *Agent, args []string, out io.Writer) error {
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch sub {
	case "", "status":
		return cmdLlamaCppStatus(a, out)
	case "list":
		return cmdLlamaCppList(a, out)
	case "start":
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		return cmdLlamaCppStart(a, name, out)
	case "stop":
		return cmdLlamaCppStop(a, out)
	case "drop":
		if len(args) < 2 {
			return fmt.Errorf("usage: /llamacpp drop NAME")
		}
		return cmdLlamaCppDrop(a, args[1], out)
	default:
		fmt.Fprintf(out, "  Unknown subcommand %q. Usage: /llamacpp <status|list|start PATH|stop|drop NAME>\n", sub)
		return nil
	}
}

// cmdLlamaCppStatus prints the current llama.cpp connection status.
func cmdLlamaCppStatus(a *Agent, out io.Writer) error {
	cfg := &a.Config.LlamaCpp
	url := cfg.URL
	if url == "" {
		url = "http://127.0.0.1:8081"
	}
	reachable := "no"
	if probeLlamaCpp(url) {
		reachable = "yes"
	}
	managed := "no"
	if a.Backend != nil && a.Backend.Name() == "llamacpp" && a.Backend.StartedByHarvey() {
		managed = "yes (started by Harvey)"
	}
	active := ""
	if a.Backend != nil && a.Backend.Name() == "llamacpp" {
		active = a.Backend.ActiveModel()
	}
	if active == "" {
		active = "(none)"
	}
	fmt.Fprintf(out, "  Active model:    %s\n", active)
	fmt.Fprintf(out, "  API URL:         %s\n", url)
	fmt.Fprintf(out, "  Reachable:       %s\n", reachable)
	fmt.Fprintf(out, "  Managed:         %s\n", managed)
	if cfg.GPULayers > 0 {
		fmt.Fprintf(out, "  GPU layers:      %d\n", cfg.GPULayers)
	}
	return nil
}

// cmdLlamaCppList scans the models directory and prints all *.gguf models.
func cmdLlamaCppList(a *Agent, out io.Writer) error {
	agentsDir := filepath.Join(a.Workspace.Root, "agents")
	b := NewLlamaCppBackend(a.Config, agentsDir)
	models, err := b.ListModels()
	if err != nil {
		return err
	}
	if len(models) == 0 {
		fmt.Fprintf(out, "  No *.gguf models found in %s\n", b.modelsDir)
		return nil
	}
	fmt.Fprintf(out, "  %-40s  %10s\n", "Name", "Size")
	fmt.Fprintln(out, "  "+strings.Repeat("-", 54))
	for _, m := range models {
		fmt.Fprintf(out, "  %-40s  %10s\n", m.Name, llamafileFormatBytes(m.SizeBytes))
	}
	return nil
}

// cmdLlamaCppStart starts llama-server for the given model path, or presents a
// picker when no path is supplied and *.gguf files are found in modelsDir.
func cmdLlamaCppStart(a *Agent, modelArg string, out io.Writer) error {
	agentsDir := filepath.Join(a.Workspace.Root, "agents")
	b := NewLlamaCppBackend(a.Config, agentsDir)

	modelPath := modelArg
	if modelPath == "" {
		models, err := b.ListModels()
		if err != nil || len(models) == 0 {
			return fmt.Errorf("no *.gguf models found in %s — supply a path", b.modelsDir)
		}
		if len(models) == 1 {
			modelPath = models[0].Path
		} else {
			fmt.Fprintln(out, "  Available models:")
			for i, m := range models {
				fmt.Fprintf(out, "  [%d] %s (%s)\n", i+1, m.Name, llamafileFormatBytes(m.SizeBytes))
			}
			return fmt.Errorf("multiple models found — supply a path: /llamacpp start PATH")
		}
	}

	// Resolve ~ and relative paths.
	if strings.HasPrefix(modelPath, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			modelPath = filepath.Join(home, modelPath[1:])
		}
	} else if !filepath.IsAbs(modelPath) {
		modelPath = filepath.Join(a.Workspace.Root, modelPath)
	}

	if a.Backend != nil && a.Backend.StartedByHarvey() {
		fmt.Fprintln(out, "  Stopping current backend...")
		_ = a.Backend.Stop()
	}

	if err := b.Start(context.Background(), modelPath, out); err != nil {
		return err
	}

	client, err := b.NewClient()
	if err != nil {
		return err
	}
	a.Client = client
	a.Backend = b
	fmt.Fprintf(out, "  %s Using llama-server model: %s\n", green("✓"), cyan(b.ActiveModel()))
	return nil
}

// cmdLlamaCppStop stops the llama-server Harvey manages.
func cmdLlamaCppStop(a *Agent, out io.Writer) error {
	if a.Backend == nil || a.Backend.Name() != "llamacpp" {
		fmt.Fprintln(out, "  No llama-server backend is active.")
		return nil
	}
	if !a.Backend.StartedByHarvey() {
		fmt.Fprintln(out, "  The llama-server was not started by Harvey — not stopping.")
		return nil
	}
	if err := a.Backend.Stop(); err != nil {
		return err
	}
	a.Backend = nil
	fmt.Fprintln(out, "  llama-server stopped.")
	return nil
}

// cmdLlamaCppDrop removes a model from the models directory index (does not
// delete the file). When name matches the active model, the backend is cleared.
func cmdLlamaCppDrop(a *Agent, name string, out io.Writer) error {
	if a.Backend != nil && a.Backend.Name() == "llamacpp" &&
		strings.EqualFold(a.Backend.ActiveModel(), name) {
		if a.Backend.StartedByHarvey() {
			_ = a.Backend.Stop()
		}
		a.Backend = nil
		a.Client = nil
		fmt.Fprintf(out, "  Dropped active llamacpp model %q and cleared backend.\n", name)
		return nil
	}
	fmt.Fprintf(out, "  Model %q dropped from active session (file not deleted).\n", name)
	return nil
}
