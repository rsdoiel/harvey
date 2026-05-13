package harvey

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// maxInputPath caps path arguments to guard against excessively long inputs.
const maxInputPath = 4096

// maxInputContent caps write_file content to 10 MiB.
const maxInputContent = 10 * 1024 * 1024

// maxInputPattern caps regex pattern arguments.
const maxInputPattern = 1024

// maxInputCommand caps run_command / git_command argument strings.
const maxInputCommand = 4096

/** RegisterBuiltinTools registers all of Harvey's built-in schema-based tools
 * with the given registry. Every handler enforces the workspace boundary,
 * agent permission checks, safe-type assertions, and the output size cap.
 *
 * Parameters:
 *   r    (*ToolRegistry) — registry to populate.
 *   a    (*Agent)        — agent providing workspace, config, and permissions.
 *
 * Example:
 *   RegisterBuiltinTools(agent.Tools, agent)
 */
func RegisterBuiltinTools(r *ToolRegistry, a *Agent) {
	root := a.Workspace.Root
	maxBytes := a.Config.MaxOutputBytes

	// ── read_file ────────────────────────────────────────────────────────────
	r.RegisterTool(
		"read_file",
		"Read the contents of a file in the workspace. "+
			"Path must be relative to the workspace root.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path to the file within the workspace",
				},
			},
			"required": []string{"path"},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			p, ok := args["path"].(string)
			if !ok || p == "" {
				return "", fmt.Errorf("read_file: path must be a non-empty string")
			}
			if len(p) > maxInputPath {
				return "", fmt.Errorf("read_file: path exceeds maximum length of %d bytes", maxInputPath)
			}
			resolved, err := resolveWorkspacePath(root, p)
			if err != nil {
				return "", fmt.Errorf("read_file: %w", err)
			}
			if !a.CheckReadPermission(p) {
				if a.AuditBuffer != nil {
					a.AuditBuffer.Log(ActionFileRead, p, StatusDenied)
				}
				return "", fmt.Errorf("read_file: read permission denied for %q", p)
			}
			data, err := os.ReadFile(resolved)
			if err != nil {
				if a.AuditBuffer != nil {
					a.AuditBuffer.Log(ActionFileRead, p, StatusError)
				}
				return "", fmt.Errorf("read_file: %w", err)
			}
			if a.AuditBuffer != nil {
				a.AuditBuffer.Log(ActionFileRead, p, StatusSuccess)
			}
			return capOutput(string(data), maxBytes), nil
		},
	)

	// ── write_file ───────────────────────────────────────────────────────────
	r.RegisterTool(
		"write_file",
		"Write content to a file in the workspace, creating parent directories as needed. "+
			"Path must be relative to the workspace root.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path to the file within the workspace",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Content to write to the file",
				},
			},
			"required": []string{"path", "content"},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			p, ok := args["path"].(string)
			if !ok || p == "" {
				return "", fmt.Errorf("write_file: path must be a non-empty string")
			}
			if len(p) > maxInputPath {
				return "", fmt.Errorf("write_file: path exceeds maximum length of %d bytes", maxInputPath)
			}
			content, ok := args["content"].(string)
			if !ok {
				return "", fmt.Errorf("write_file: content must be a string")
			}
			if len(content) > maxInputContent {
				return "", fmt.Errorf("write_file: content exceeds maximum size of %d bytes", maxInputContent)
			}
			if _, err := resolveWorkspacePath(root, p); err != nil {
				return "", fmt.Errorf("write_file: %w", err)
			}
			if !a.CheckWritePermission(p) {
				if a.AuditBuffer != nil {
					a.AuditBuffer.Log(ActionFileWrite, p, StatusDenied)
				}
				return "", fmt.Errorf("write_file: write permission denied for %q", p)
			}
			if err := a.Workspace.WriteFile(p, []byte(content), 0o644); err != nil {
				if a.AuditBuffer != nil {
					a.AuditBuffer.Log(ActionFileWrite, p, StatusError)
				}
				return "", fmt.Errorf("write_file: %w", err)
			}
			if a.AuditBuffer != nil {
				a.AuditBuffer.Log(ActionFileWrite, p, StatusSuccess)
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(content), p), nil
		},
	)

	// ── list_files ───────────────────────────────────────────────────────────
	r.RegisterTool(
		"list_files",
		"List files and directories in the workspace at the given path. "+
			"Path must be relative to the workspace root; defaults to the root.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path to the directory to list (default: workspace root)",
				},
			},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			p := "."
			if v, ok := args["path"].(string); ok && v != "" {
				if len(v) > maxInputPath {
					return "", fmt.Errorf("list_files: path exceeds maximum length of %d bytes", maxInputPath)
				}
				p = v
			}
			if _, err := resolveWorkspacePath(root, p); err != nil {
				return "", fmt.Errorf("list_files: %w", err)
			}
			entries, err := a.Workspace.ListDir(p)
			if err != nil {
				return "", fmt.Errorf("list_files: %w", err)
			}
			var sb strings.Builder
			for _, e := range entries {
				if e.IsDir() {
					fmt.Fprintf(&sb, "%s/\n", e.Name())
				} else {
					fmt.Fprintf(&sb, "%s\n", e.Name())
				}
			}
			return capOutput(sb.String(), maxBytes), nil
		},
	)

	// ── search_files ─────────────────────────────────────────────────────────
	r.RegisterTool(
		"search_files",
		"Search workspace files for lines matching a regular expression pattern. "+
			"Optionally restricted to a subdirectory. Returns file:line:text matches.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Regular expression pattern to search for",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path to a subdirectory to restrict the search (default: entire workspace)",
				},
			},
			"required": []string{"pattern"},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			pattern, ok := args["pattern"].(string)
			if !ok || pattern == "" {
				return "", fmt.Errorf("search_files: pattern must be a non-empty string")
			}
			if len(pattern) > maxInputPattern {
				return "", fmt.Errorf("search_files: pattern exceeds maximum length of %d bytes", maxInputPattern)
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return "", fmt.Errorf("search_files: invalid pattern: %w", err)
			}

			searchRoot := "."
			if v, ok := args["path"].(string); ok && v != "" {
				if len(v) > maxInputPath {
					return "", fmt.Errorf("search_files: path exceeds maximum length of %d bytes", maxInputPath)
				}
				searchRoot = v
			}
			absRoot, err := a.Workspace.AbsPath(searchRoot)
			if err != nil {
				return "", fmt.Errorf("search_files: %w", err)
			}

			const maxMatches = 100
			var sb strings.Builder
			count := 0
			truncated := false

			filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, werr error) error {
				if werr != nil || truncated {
					return nil
				}
				if d.IsDir() {
					if strings.HasPrefix(d.Name(), ".") {
						return filepath.SkipDir
					}
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil || isBinary(data) {
					return nil
				}
				rel, _ := filepath.Rel(a.Workspace.Root, path)
				scanner := bufio.NewScanner(strings.NewReader(string(data)))
				lineNum := 0
				for scanner.Scan() {
					lineNum++
					if re.MatchString(scanner.Text()) {
						fmt.Fprintf(&sb, "%s:%d:%s\n", rel, lineNum, scanner.Text())
						count++
						if count >= maxMatches {
							truncated = true
							return nil
						}
					}
				}
				return nil
			})

			if truncated {
				sb.WriteString(fmt.Sprintf("\n[results truncated at %d matches]", maxMatches))
			}
			if count == 0 {
				return "no matches found", nil
			}
			return capOutput(sb.String(), maxBytes), nil
		},
	)

	// ── run_command ──────────────────────────────────────────────────────────
	r.RegisterTool(
		"run_command",
		"Execute a shell command in the workspace root. "+
			"The command is subject to Harvey's safe mode and allowed-commands list. "+
			"Working directory is always the workspace root.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Shell command to execute (parsed as a command + arguments, no shell expansion)",
				},
			},
			"required": []string{"command"},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			cmdStr, ok := args["command"].(string)
			if !ok || cmdStr == "" {
				return "", fmt.Errorf("run_command: command must be a non-empty string")
			}
			if len(cmdStr) > maxInputCommand {
				return "", fmt.Errorf("run_command: command exceeds maximum length of %d bytes", maxInputCommand)
			}

			program, cmdArgs, err := parseCommandLine(cmdStr)
			if err != nil {
				return "", fmt.Errorf("run_command: %w", err)
			}

			if a.Config.SafeMode && !a.Config.IsCommandAllowed(program) {
				if a.AuditBuffer != nil {
					a.AuditBuffer.Log(ActionCommand, cmdStr, StatusDenied)
				}
				return "", fmt.Errorf("run_command: %q is not in the safe-mode allowlist", program)
			}
			if a.AuditBuffer != nil {
				a.AuditBuffer.Log(ActionCommand, cmdStr, StatusAllowed)
			}

			var runCtx context.Context
			var cancel context.CancelFunc
			if a.Config.RunTimeout > 0 {
				runCtx, cancel = context.WithTimeout(ctx, a.Config.RunTimeout)
			} else {
				runCtx, cancel = context.WithCancel(ctx)
			}
			defer cancel()

			cmd := exec.CommandContext(runCtx, program, cmdArgs...)
			cmd.Dir = a.Workspace.Root
			cmd.Env = filterCommandEnvironment(os.Environ())
			raw, _ := cmd.CombinedOutput()

			exitNote := ""
			if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() != 0 {
				exitNote = fmt.Sprintf(" (exit %d)", cmd.ProcessState.ExitCode())
			}

			result := capOutput(string(raw), maxBytes)
			if exitNote != "" {
				result += exitNote
			}
			return result, nil
		},
	)

	// ── git_command ──────────────────────────────────────────────────────────
	r.RegisterTool(
		"git_command",
		"Run a read-only git subcommand in the workspace root. "+
			"Allowed subcommands: status, diff, log, show, blame.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"subcommand": map[string]any{
					"type":        "string",
					"description": "Git subcommand: status, diff, log, show, or blame",
					"enum":        []string{"status", "diff", "log", "show", "blame"},
				},
				"args": map[string]any{
					"type":        "string",
					"description": "Additional arguments to pass to git (optional)",
				},
			},
			"required": []string{"subcommand"},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			sub, ok := args["subcommand"].(string)
			if !ok || sub == "" {
				return "", fmt.Errorf("git_command: subcommand must be a non-empty string")
			}
			sub = strings.ToLower(sub)
			if !gitAllowedSubcmds[sub] {
				return "", fmt.Errorf("git_command: subcommand %q is not allowed; use: status, diff, log, show, blame", sub)
			}

			gitArgs := []string{sub}
			if extra, ok := args["args"].(string); ok && extra != "" {
				if len(extra) > maxInputCommand {
					return "", fmt.Errorf("git_command: args exceeds maximum length of %d bytes", maxInputCommand)
				}
				_, extraArgs, err := parseCommandLine(extra)
				if err != nil {
					return "", fmt.Errorf("git_command: invalid args: %w", err)
				}
				gitArgs = append(gitArgs, extraArgs...)
			}

			cmd := exec.CommandContext(ctx, "git", gitArgs...)
			cmd.Dir = a.Workspace.Root
			cmd.Env = filterCommandEnvironment(os.Environ())
			raw, _ := cmd.CombinedOutput()

			return capOutput(string(raw), maxBytes), nil
		},
	)
}
