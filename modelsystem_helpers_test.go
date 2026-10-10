package harvey

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
)

/** modelSystemAvailable reports whether a model system is present on this
 * machine, and when it is not, why and what to do about it. It is the single
 * place that decides, so a live model test and its skip message cannot drift.
 *
 * Parameters:
 *   system (string)  — "ollama", "hailo", "llamacpp" or "llamafile".
 *   cfg    (*Config) — supplies the URLs, server binary and models directory.
 *
 * Returns:
 *   bool   — true when the system can be used by a test.
 *   string — when false, one sentence for the person running the tests.
 *
 * Example:
 *   if ok, why := modelSystemAvailable("ollama", DefaultConfig()); !ok { t.Skip(why) }
 */
func modelSystemAvailable(system string, cfg *Config) (bool, string) {
	switch system {
	case "ollama":
		if !ProbeOllama(cfg.Ollama.URL) {
			return false, fmt.Sprintf("Ollama is not reachable at %s; start it (`ollama serve`) to run this test", cfg.Ollama.URL)
		}
	case "hailo":
		if !ProbeOllama(cfg.Hailo.URL) {
			return false, fmt.Sprintf("hailo-ollama is not reachable at %s; start it (`systemctl --user start hailo-ollama`) to run this test", cfg.Hailo.URL)
		}
	case "llamacpp":
		bin := cfg.LlamaCpp.ServerBin
		if bin == "" {
			bin = "llama-server"
		}
		if _, err := exec.LookPath(bin); err != nil {
			return false, fmt.Sprintf("%s is not on PATH; install llama.cpp to run this test", bin)
		}
	case "llamafile":
		dir := cfg.Llamafile.ModelsDir
		if m, _ := filepath.Glob(filepath.Join(dir, "*.llamafile")); len(m) == 0 {
			return false, fmt.Sprintf("no *.llamafile in %s; put a llamafile there to run this test", dir)
		}
	default:
		return false, fmt.Sprintf("unknown model system %q", system)
	}
	return true, ""
}

/** requireModelSystem skips the calling test, with the reason shown, when the
 * model system is not on this machine. Use it first in any test that talks to
 * a real Ollama, hailo-ollama, llama-server or llamafile. Skip reasons print
 * with `go test -v`, which `make test-models` uses.
 *
 * Parameters:
 *   t      (*testing.T) — the test to skip.
 *   system (string)     — "ollama", "hailo", "llamacpp" or "llamafile".
 *
 * Returns:
 *   *Config — default configuration, with this machine's real URLs and directories.
 *
 * Example:
 *   cfg := requireModelSystem(t, "ollama")
 */
func requireModelSystem(t *testing.T, system string) *Config {
	t.Helper()
	cfg := DefaultConfig()
	if ok, why := modelSystemAvailable(system, cfg); !ok {
		t.Skipf("no %s on this machine: %s", system, why)
	}
	return cfg
}

