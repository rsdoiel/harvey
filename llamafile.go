// Package harvey — llamafile.go implements the /llamafile slash command family
// for registering, switching, and managing llamafile model backends.
package harvey

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// llamafileModelName derives a display/registry name from a llamafile binary
// path by stripping the directory and the ".llamafile" suffix.
func llamafileModelName(path string) string {
	name := filepath.Base(path)
	return strings.TrimSuffix(name, ".llamafile")
}

/** DefaultLlamafileModelsDir returns the default discovery directory ($HOME/Models).
 * Exported so cmd/harvey/main.go can detect whether the user has overridden it
 * before applying the HARVEY_LLAMAFILE_DIR environment variable.
 *
 * Returns:
 *   string — default models directory path.
 *
 * Example:
 *   if cfg.LlamafileModelsDir == harvey.DefaultLlamafileModelsDir() { ... }
 */
func DefaultLlamafileModelsDir() string {
	return llamafileDefaultModelsDir()
}

/** LlamafileModelNameFromPath is the exported form of llamafileModelName,
 * used by cmd/harvey/main.go when building a session-only registry entry
 * from the --llamafile CLI flag.
 *
 * Parameters:
 *   path (string) — path to a llamafile binary.
 *
 * Returns:
 *   string — name derived from the filename with ".llamafile" stripped.
 *
 * Example:
 *   name := harvey.LlamafileModelNameFromPath("/home/user/Models/Qwen3.5-4B.llamafile")
 *   // name == "Qwen3.5-4B"
 */
func LlamafileModelNameFromPath(path string) string {
	return llamafileModelName(path)
}

// scanLlamafileModels returns the absolute paths of all *.llamafile files
// found directly inside dir. Returns nil when dir does not exist or cannot
// be read.
func scanLlamafileModels(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".llamafile") {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	return paths
}

/** cmdLlamafile dispatches /llamafile subcommands: add, use, list, start, status.
 * With no subcommand, or an unrecognised one, the help text is printed.
 *
 * Parameters:
 *   a    (*Agent)   — the running Harvey agent.
 *   args ([]string) — subcommand and its arguments.
 *   out  (io.Writer) — destination for output.
 *
 * Returns:
 *   error — non-nil on unexpected failures.
 *
 * Example:
 *   cmdLlamafile(agent, []string{"list"}, os.Stdout)
 */
func cmdLlamafile(a *Agent, args []string, out io.Writer) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "add":
		return cmdLlamafileAdd(a, args[1:], out)
	case "use":
		return cmdLlamafileUse(a, args[1:], out)
	case "list":
		return cmdLlamafileList(a, out)
	case "start":
		return cmdLlamafileStart(a, args[1:], out)
	case "status":
		return cmdLlamafileStatus(a, out)
	default:
		fmt.Fprint(out, LlamafileHelpText)
		return nil
	}
}

// cmdLlamafileAdd registers a llamafile model and connects to it. When no
// path argument is given, it scans LlamafileModelsDir and shows a picker.
func cmdLlamafileAdd(a *Agent, args []string, out io.Writer) error {
	var path, name string

	if len(args) > 0 {
		path = args[0]
		if len(args) > 1 {
			name = args[1]
		}
	} else {
		// No path given — try the models directory picker.
		selected, err := llamafilePickFromDir(a, out)
		if err != nil || selected == "" {
			return err
		}
		path = selected
	}

	// Derive name from filename if not supplied or prompted.
	if name == "" {
		defaultName := llamafileModelName(path)
		fmt.Fprintf(out, "  Name [%s]: ", defaultName)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			name = defaultName
		} else {
			name = line
		}
	}

	// Resolve workspace-relative paths.
	absPath := resolveLlamafilePath(path, a.Workspace.Root)

	// Validate the file exists.
	if _, err := os.Stat(absPath); err != nil {
		return fmt.Errorf("llamafile not found: %s", absPath)
	}

	// Stop whatever is currently running so the new model can bind the port.
	if a.llamafileProc != nil {
		fmt.Fprintln(out, "  Stopping current llamafile...")
		a.stopLlamafileProc()
	} else if ProbeLlamafile(a.Config.LlamafileURL) {
		fmt.Fprintf(out, yellow("  ⚠ A llamafile server is already running at %s but was not started by this session.\n"), a.Config.LlamafileURL)
		fmt.Fprintln(out, "  Stop it manually (e.g. via htop), then run /llamafile use to start the new model.")
		return nil
	}
	fmt.Fprintln(out, "  Starting llamafile...")
	proc, err := StartLlamafileService(absPath, a.Config.LlamafileURL, "", a.Config.LlamafileStartupTimeout, a.Config.LlamafileGPULayers, out)
	if err != nil {
		return fmt.Errorf("failed to start llamafile: %w", err)
	}
	a.llamafileProc = proc
	fmt.Fprintln(out, green("  ✓")+" Llamafile started")

	if err := a.useLlamafileEntry(name, out); err != nil {
		return err
	}

	a.Config.AddOrUpdateLlamafileEntry(LlamafileEntry{Name: name, Path: path})
	a.Config.LlamafileActive = name
	if err := SaveLlamafileConfig(a.Workspace, a.Config); err != nil {
		fmt.Fprintf(out, yellow("  ⚠ Could not save config: %v\n"), err)
	} else {
		fmt.Fprintln(out, dim("  Saved to agents/harvey.yaml — Harvey will connect automatically on next start."))
	}
	return nil
}

// llamafilePickFromDir scans LlamafileModelsDir, shows a numbered picker,
// and returns the selected path. Returns ("", nil) if the user declines.
func llamafilePickFromDir(a *Agent, out io.Writer) (string, error) {
	dir := a.Config.LlamafileModelsDir
	paths := scanLlamafileModels(dir)

	if len(paths) == 0 {
		fmt.Fprintf(out, "  No llamafiles found in %s.\n", dir)
		fmt.Fprint(out, "  Enter a path to a llamafile: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			fmt.Fprintln(out, "  No path given — use /llamafile add PATH to register a model.")
			return "", nil
		}
		return line, nil
	}

	fmt.Fprintf(out, "  Llamafiles found in %s:\n", dir)
	for i, p := range paths {
		info, err := os.Stat(p)
		size := ""
		if err == nil {
			size = fmt.Sprintf(" (%s)", llamafileFormatBytes(info.Size()))
		}
		// Mark already-registered models.
		registered := ""
		name := llamafileModelName(p)
		for _, e := range a.Config.LlamafileModels {
			if e.Path == p || e.Path == filepath.Base(p) {
				registered = fmt.Sprintf("  (registered as %s)", e.Name)
				name = e.Name
				break
			}
		}
		_ = name
		fmt.Fprintf(out, "  [%d] %s%s%s\n", i+1, filepath.Base(p), size, registered)
	}
	fmt.Fprintf(out, "  Select [1-%d] or enter a path: ", len(paths))

	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)

	// If the user typed a number, use that index.
	idx := 0
	fmt.Sscanf(line, "%d", &idx)
	if idx >= 1 && idx <= len(paths) {
		return paths[idx-1], nil
	}
	// Otherwise treat as a direct path.
	if line != "" {
		return line, nil
	}
	fmt.Fprintln(out, "  No selection — use /llamafile add PATH to register a model.")
	return "", nil
}

// llamafilePickFromRegistered shows a numbered picker of registered models
// and returns the selected name. Returns ("", nil) if the user cancels.
func llamafilePickFromRegistered(a *Agent, out io.Writer) (string, error) {
	models := a.Config.LlamafileModels
	if len(models) == 0 {
		fmt.Fprintln(out, "  No llamafile models registered. Use /llamafile add first.")
		return "", nil
	}
	fmt.Fprintln(out, "  Registered llamafile models:")
	for i, e := range models {
		arrow := "  "
		if e.Name == a.Config.LlamafileActive {
			arrow = "→ "
		}
		fmt.Fprintf(out, "  %s[%d] %s\n", arrow, i+1, e.Name)
	}
	fmt.Fprintf(out, "  Select [1-%d] or enter a name: ", len(models))
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		fmt.Fprintln(out, "  No selection.")
		return "", nil
	}
	idx := 0
	fmt.Sscanf(line, "%d", &idx)
	if idx >= 1 && idx <= len(models) {
		return models[idx-1].Name, nil
	}
	return line, nil
}

// cmdLlamafileUse switches to a named registered model.
// When no NAME is given, shows a numbered picker of registered models.
func cmdLlamafileUse(a *Agent, args []string, out io.Writer) error {
	name := ""
	if len(args) > 0 {
		name = args[0]
	} else {
		selected, err := llamafilePickFromRegistered(a, out)
		if err != nil || selected == "" {
			return err
		}
		name = selected
	}
	entry := a.Config.LlamafileEntryByName(name)
	if entry == nil {
		return fmt.Errorf("no llamafile registered as %q — use /llamafile list to see registered models", name)
	}

	absPath := resolveLlamafilePath(entry.Path, a.Workspace.Root)

	// Stop the current server if Harvey started it.
	if a.llamafileProc != nil {
		fmt.Fprintf(out, "  Stopping %s...\n", a.Config.LlamafileActive)
		a.stopLlamafileProc()
	}

	fmt.Fprintf(out, "  Starting %s...\n", name)
	proc, err := StartLlamafileService(absPath, a.Config.LlamafileURL, "", a.Config.LlamafileStartupTimeout, a.Config.LlamafileGPULayers, out)
	if err != nil {
		return fmt.Errorf("failed to start llamafile: %w", err)
	}
	a.llamafileProc = proc

	if err := a.useLlamafileEntry(name, out); err != nil {
		return err
	}
	a.Config.LlamafileActive = name
	if err := SaveLlamafileConfig(a.Workspace, a.Config); err != nil {
		fmt.Fprintf(out, yellow("  ⚠ Could not save config: %v\n"), err)
	}
	return nil
}

// cmdLlamafileList prints the registered llamafile models.
func cmdLlamafileList(a *Agent, out io.Writer) error {
	if len(a.Config.LlamafileModels) == 0 {
		fmt.Fprintln(out, "  No llamafile models registered.")
		fmt.Fprintln(out, "  Use /llamafile add to register one.")
	} else {
		fmt.Fprintln(out, "  Registered llamafile models:")
		for _, e := range a.Config.LlamafileModels {
			arrow := "  "
			if e.Name == a.Config.LlamafileActive {
				arrow = "→ "
			}
			size := ""
			if info, err := os.Stat(resolveLlamafilePath(e.Path, a.Workspace.Root)); err == nil {
				size = fmt.Sprintf(" (%s)", llamafileFormatBytes(info.Size()))
			}
			fmt.Fprintf(out, "  %s%-20s %s%s\n", arrow, e.Name, e.Path, size)
		}
	}
	fmt.Fprintf(out, "  Discovery directory: %s\n", a.Config.LlamafileModelsDir)
	return nil
}

// cmdLlamafileStart starts the active (or named) llamafile server.
func cmdLlamafileStart(a *Agent, args []string, out io.Writer) error {
	name := a.Config.LlamafileActive
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		fmt.Fprintln(out, "  No active llamafile. Use /llamafile add or /llamafile use NAME first.")
		return nil
	}
	entry := a.Config.LlamafileEntryByName(name)
	if entry == nil {
		return fmt.Errorf("no llamafile registered as %q", name)
	}

	if ProbeLlamafile(a.Config.LlamafileURL) {
		fmt.Fprintf(out, "  Llamafile (%s) is already running at %s\n", name, a.Config.LlamafileURL)
		return nil
	}

	absPath := resolveLlamafilePath(entry.Path, a.Workspace.Root)
	fmt.Fprintf(out, "  Starting %s...\n", name)
	proc, err := StartLlamafileService(absPath, a.Config.LlamafileURL, "", a.Config.LlamafileStartupTimeout, a.Config.LlamafileGPULayers, out)
	if err != nil {
		return fmt.Errorf("failed to start llamafile: %w", err)
	}
	a.stopLlamafileProc()
	a.llamafileProc = proc
	return a.useLlamafileEntry(name, out)
}

// cmdLlamafileStatus prints the current llamafile connection status.
func cmdLlamafileStatus(a *Agent, out io.Writer) error {
	active := a.Config.LlamafileActive
	if active == "" {
		active = "(none)"
	}
	reachable := "no"
	if ProbeLlamafile(a.Config.LlamafileURL) {
		reachable = "yes"
	}
	managed := "no"
	if a.llamafileProc != nil {
		managed = "yes (started by Harvey)"
	}
	fmt.Fprintf(out, "  Active model:    %s\n", active)
	fmt.Fprintf(out, "  API URL:         %s\n", a.Config.LlamafileURL)
	fmt.Fprintf(out, "  Reachable:       %s\n", reachable)
	fmt.Fprintf(out, "  Process managed: %s\n", managed)
	fmt.Fprintf(out, "  Models dir:      %s\n", a.Config.LlamafileModelsDir)
	fmt.Fprintf(out, "  Registered:      %d model(s)\n", len(a.Config.LlamafileModels))
	return nil
}

// llamafileFormatBytes returns a human-readable file size string for llamafile listings.
func llamafileFormatBytes(b int64) string {
	const gb = 1 << 30
	const mb = 1 << 20
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.0f MB", float64(b)/float64(mb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
