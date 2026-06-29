// Package harvey — workspace_init.go implements the /workspace init command
// and the --init-from CLI flag. Both copy model aliases from a source workspace
// or YAML file into the current workspace without any runtime inheritance.
package harvey

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

/** ImportAliasesFrom reads model aliases from sourcePath and merges them into
 * destCfg, then persists the result to destWS. sourcePath may be:
 *   - a directory containing agents/harvey.yaml  (another Harvey workspace)
 *   - a YAML file with a model_aliases map at the top level
 *
 * Existing aliases in destCfg are never overwritten; source aliases fill gaps
 * only. The number of aliases copied and skipped are returned.
 *
 * Parameters:
 *   sourcePath (string)    — path to a workspace directory or a harvey.yaml file.
 *   destWS     (*Workspace) — workspace receiving the aliases.
 *   destCfg    (*Config)   — config receiving the aliases (mutated in place).
 *   out        (io.Writer)  — progress output.
 *
 * Returns:
 *   copied  (int)   — number of aliases added to destCfg.
 *   skipped (int)   — number of aliases skipped (name already existed).
 *   err     (error) — non-nil on read or parse failure.
 *
 * Example:
 *   copied, skipped, err := ImportAliasesFrom("/other/project", ws, cfg, os.Stdout)
 *   // "  Imported 3 aliases (2 skipped — already defined)\n"
 */
func ImportAliasesFrom(sourcePath string, destWS *Workspace, destCfg *Config, out io.Writer) (copied, skipped int, err error) {
	yamlPath, err := resolveSourceYAML(sourcePath)
	if err != nil {
		return 0, 0, err
	}

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return 0, 0, fmt.Errorf("read %s: %w", yamlPath, err)
	}

	var y struct {
		ModelAliases map[string]modelAliasYAML `yaml:"model_aliases"`
	}
	if err := yaml.Unmarshal(data, &y); err != nil {
		return 0, 0, fmt.Errorf("parse %s: %w", yamlPath, err)
	}

	if len(y.ModelAliases) == 0 {
		fmt.Fprintln(out, "  Source contains no model aliases.")
		return 0, 0, nil
	}

	if destCfg.ModelAliases == nil {
		destCfg.ModelAliases = make(map[string]ModelAlias)
	}

	for name, src := range y.ModelAliases {
		if _, exists := destCfg.ModelAliases[name]; exists {
			skipped++
			continue
		}
		destCfg.ModelAliases[name] = ModelAlias{Model: src.Model, Tags: src.Tags}
		copied++
	}

	if err := SaveModelAliases(destWS, destCfg); err != nil {
		return copied, skipped, fmt.Errorf("save aliases: %w", err)
	}

	fmt.Fprintf(out, "  Imported %d %s", copied, pluralAlias(copied))
	if skipped > 0 {
		fmt.Fprintf(out, " (%d skipped — already defined)", skipped)
	}
	fmt.Fprintln(out)
	return copied, skipped, nil
}

// resolveSourceYAML returns the path to the harvey.yaml file for the given
// source. If sourcePath is a directory it looks for agents/harvey.yaml inside
// it. Otherwise it returns sourcePath directly.
func resolveSourceYAML(sourcePath string) (string, error) {
	fi, err := os.Stat(sourcePath)
	if err != nil {
		return "", fmt.Errorf("source path %q: %w", sourcePath, err)
	}
	if fi.IsDir() {
		candidate := filepath.Join(sourcePath, harveySubdir, "harvey.yaml")
		if _, err := os.Stat(candidate); err != nil {
			return "", fmt.Errorf("no agents/harvey.yaml in %q", sourcePath)
		}
		return candidate, nil
	}
	if !strings.HasSuffix(sourcePath, ".yaml") && !strings.HasSuffix(sourcePath, ".yml") {
		return "", fmt.Errorf("source %q must be a workspace directory or a .yaml file", sourcePath)
	}
	return sourcePath, nil
}

func pluralAlias(n int) string {
	if n == 1 {
		return "alias"
	}
	return "aliases"
}
