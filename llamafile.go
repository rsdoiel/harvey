// Package harvey — llamafile.go implements the /llamafile slash command family
// for registering, switching, and managing llamafile model backends.
package harvey

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// llamafileModelName derives a display/registry name from a llamafile binary
// path by stripping the directory and known llamafile suffixes. Suffixes are
// stripped longest-first so ".llamafile.exe" collapses fully to the stem.
func llamafileModelName(path string) string {
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".llamafile.exe")
	name = strings.TrimSuffix(name, ".exe")
	name = strings.TrimSuffix(name, ".llamafile")
	return name
}

/** DefaultLlamafileModelsDir returns the default discovery directory ($HOME/Models).
 * Exported so cmd/harvey/main.go can detect whether the user has overridden it
 * before applying the HARVEY_LLAMAFILE_DIR environment variable.
 *
 * Returns:
 *   string — default models directory path.
 *
 * Example:
 *   if cfg.Llamafile.ModelsDir == harvey.DefaultLlamafileModelsDir() { ... }
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

// scanLlamafileModels returns the absolute paths of all llamafile binaries
// found directly inside dir. Matches .llamafile (all platforms),
// .llamafile.exe (all platforms), and plain .exe (Windows only).
// Returns nil when dir does not exist or cannot be read.
func scanLlamafileModels(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".llamafile") ||
			strings.HasSuffix(name, ".llamafile.exe") ||
			(runtime.GOOS == "windows" && strings.HasSuffix(name, ".exe")) {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	return paths
}

// resolveLlamafilePath returns an absolute path for p, resolving workspace-
// relative paths against root. Absolute paths are returned unchanged.
func resolveLlamafilePath(p, root string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}

/** probeRunningLlamafileName queries the /v1/models endpoint of a running
 * llamafile server and returns the first model's name with the ".gguf" suffix
 * stripped. Returns "" when the server is unreachable, returns an error, or
 * the data array is empty.
 *
 * Parameters:
 *   url (string) — base URL of the llamafile server (e.g. "http://localhost:8080").
 *
 * Returns:
 *   string — model name without ".gguf", or "" if unavailable.
 *
 * Example:
 *   name := probeRunningLlamafileName("http://localhost:8080")
 *   // name == "Qwen3.5-4B-Q5_K_S"
 */
func probeRunningLlamafileName(url string) string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url + "/v1/models")
	if err != nil || resp.StatusCode != http.StatusOK {
		return ""
	}
	defer resp.Body.Close()
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || len(payload.Data) == 0 {
		return ""
	}
	name := payload.Data[0].ID
	name = strings.TrimSuffix(name, ".gguf")
	return name
}

/** adoptExternalServer offers to register a llamafile server that is already
 * running at a.Config.Llamafile.URL but was not started by this Harvey session.
 * It probes /v1/models to identify the running model, shows an adoption prompt,
 * and — if the user accepts — registers the model and wires up the LLM client.
 *
 * Parameters:
 *   a   (*Agent)   — the running Harvey agent.
 *   out (io.Writer) — destination for status messages and the adoption prompt.
 *
 * Returns:
 *   error — non-nil only on unexpected failures; user declining is not an error.
 *
 * Example:
 *   if ProbeLlamafile(a.Config.Llamafile.URL) && !a.Backend.StartedByHarvey() {
 *       adoptExternalServer(a, os.Stdout)
 *   }
 */
func adoptExternalServer(a *Agent, out io.Writer) error {
	name := probeRunningLlamafileName(a.Config.Llamafile.URL)
	if name == "" {
		name = "external"
	}
	fmt.Fprintf(out, "  A llamafile server is already running at %s\n", a.Config.Llamafile.URL)
	fmt.Fprintf(out, "  Detected model: %s\n", name)
	fmt.Fprint(out, "  Adopt as active model? [Y/n]: ")
	line, _ := bufio.NewReader(a.In).ReadString('\n')
	if strings.HasPrefix(strings.TrimSpace(strings.ToLower(line)), "n") {
		fmt.Fprintln(out, "  Not adopted — stop the server manually to start a different model.")
		return nil
	}
	a.Config.AddOrUpdateLlamafileEntry(LlamafileEntry{Name: name, Path: ""})
	a.Config.Llamafile.Active = name
	if err := a.useLlamafileEntry(name, out); err != nil {
		return err
	}
	if err := SaveLlamafileConfig(a.Workspace, a.Config); err != nil {
		fmt.Fprintf(out, yellow("  ⚠ Could not save config: %v\n"), err)
	}
	fmt.Fprintln(out, green("  ✓")+" Adopted "+name)
	return nil
}

// switchLlamafileModel starts the named registered llamafile model, stopping
// any Harvey-managed backend first. Saves the updated active model to config.
func switchLlamafileModel(a *Agent, name string, out io.Writer) error {
	entry := a.Config.LlamafileEntryByName(name)
	if entry == nil {
		return fmt.Errorf("no llamafile registered as %q", name)
	}
	absPath := resolveLlamafilePath(entry.Path, a.Workspace.Root)
	if a.Backend != nil && a.Backend.StartedByHarvey() {
		fmt.Fprintf(out, "  Stopping %s...\n", a.Config.Llamafile.Active)
		_ = a.Backend.Stop()
	}
	fmt.Fprintf(out, "  Starting %s...\n", name)
	proc, err := StartLlamafileService(absPath, a.Config.Llamafile.URL, "", a.Config.Llamafile.StartupTimeout, a.Config.Llamafile.GPULayers, a.Config.ActiveLlamafileContextLength(), out)
	if err != nil {
		return fmt.Errorf("failed to start llamafile: %w", err)
	}
	a.wireLlamafileBackend(proc, name)
	if err := a.useLlamafileEntry(name, out); err != nil {
		return err
	}
	a.Config.Llamafile.Active = name
	if entry.ContextLength == 0 {
		if ctx := ProbeLlamafileContextLength(a.Config.Llamafile.URL); ctx > 0 {
			entry.ContextLength = ctx
			a.Config.AddOrUpdateLlamafileEntry(*entry)
		}
	}
	if err := SaveLlamafileConfig(a.Workspace, a.Config); err != nil {
		fmt.Fprintf(out, yellow("  ⚠ Could not save config: %v\n"), err)
	}
	return nil
}

// addAndStartLlamafile registers a llamafile binary by path and starts it.
// Used by the first-run wizard. Derives a model name from the filename.
func addAndStartLlamafile(a *Agent, path string, out io.Writer) error {
	name := llamafileModelName(path)
	absPath := resolveLlamafilePath(path, a.Workspace.Root)
	if _, err := os.Stat(absPath); err != nil {
		return fmt.Errorf("llamafile not found: %s", absPath)
	}
	if a.Backend != nil && a.Backend.StartedByHarvey() {
		fmt.Fprintln(out, "  Stopping current llamafile...")
		_ = a.Backend.Stop()
	}
	fmt.Fprintln(out, "  Starting llamafile...")
	proc, err := StartLlamafileService(absPath, a.Config.Llamafile.URL, "", a.Config.Llamafile.StartupTimeout, a.Config.Llamafile.GPULayers, a.Config.ActiveLlamafileContextLength(), out)
	if err != nil {
		return fmt.Errorf("failed to start llamafile: %w", err)
	}
	a.wireLlamafileBackend(proc, name)
	fmt.Fprintln(out, green("  ✓")+" Llamafile started")
	if err := a.useLlamafileEntry(name, out); err != nil {
		return err
	}
	entry := LlamafileEntry{Name: name, Path: path}
	if ctx := ProbeLlamafileContextLength(a.Config.Llamafile.URL); ctx > 0 {
		entry.ContextLength = ctx
	}
	a.Config.AddOrUpdateLlamafileEntry(entry)
	a.Config.Llamafile.Active = name
	if err := SaveLlamafileConfig(a.Workspace, a.Config); err != nil {
		fmt.Fprintf(out, yellow("  ⚠ Could not save config: %v\n"), err)
	} else {
		fmt.Fprintln(out, dim("  Saved to agents/harvey.yaml — Harvey will connect automatically on next start."))
	}
	return nil
}

// llamafilePickFromDir scans LlamafileModelsDir, shows a numbered picker,
// and returns the selected path. Returns ("", nil) if the user declines.
// Non-numeric input is returned as-is so the caller can treat it as a path.
func llamafilePickFromDir(a *Agent, out io.Writer) (string, error) {
	dir := a.Config.Llamafile.ModelsDir
	paths := scanLlamafileModels(dir)

	if len(paths) == 0 {
		fmt.Fprintf(out, "  No llamafiles found in %s.\n", dir)
		result, err := SelectFromStrings(nil, "Enter a path to a llamafile (or Enter to cancel): ", a.In, out)
		if err != nil || result == "" {
			if err == nil {
				fmt.Fprintln(out, "  No path given — use /model use to select a model.")
			}
			return "", err
		}
		return result, nil
	}

	fmt.Fprintf(out, "  Llamafiles found in %s:\n", dir)
	items := make([]SelectItem, len(paths))
	for i, p := range paths {
		info, err := os.Stat(p)
		size := ""
		if err == nil {
			size = fmt.Sprintf(" (%s)", llamafileFormatBytes(info.Size()))
		}
		label := filepath.Base(p) + size
		for _, e := range a.Config.Llamafile.Models {
			if e.Path == p || e.Path == filepath.Base(p) {
				label += fmt.Sprintf("  (registered as %s)", e.Name)
				break
			}
		}
		items[i] = SelectItem{Value: p, Label: label}
	}
	result, err := SelectFrom(items, fmt.Sprintf("Select [1-%d] or enter a path: ", len(items)), a.In, out)
	if err != nil {
		return "", err
	}
	if result == "" {
		fmt.Fprintln(out, "  No selection — use /model use to select a model.")
	}
	return result, nil
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
