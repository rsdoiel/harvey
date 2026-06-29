package harvey

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
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
			"Path must be relative to the workspace root. "+
			"PDF files (.pdf) are automatically extracted to plain text using poppler utilities — "+
			"no manual conversion is needed. Use the optional 'pages' parameter to read a subset "+
			"of a PDF (e.g. \"1-10\" or \"5\"). Text, Markdown, source code, and other plain-text "+
			"formats are returned as-is.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path to the file within the workspace",
				},
				"pages": map[string]any{
					"type":        "string",
					"description": "PDF only: page range to extract, e.g. \"1-10\" or \"5\". Omit to read the entire document.",
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
			if !a.CheckReadPermission(filepath.Clean(p)) {
				if a.AuditBuffer != nil {
					a.AuditBuffer.Log(ActionFileRead, p, StatusDenied)
				}
				return "", fmt.Errorf("read_file: read permission denied for %q", p)
			}
			if strings.ToLower(filepath.Ext(resolved)) == ".pdf" {
				pages, _ := args["pages"].(string)
				result, err := pdfExtract(resolved, pages)
				if err != nil {
					if a.AuditBuffer != nil {
						a.AuditBuffer.Log(ActionFileRead, p, StatusError)
					}
					return "", fmt.Errorf("read_file: %w", err)
				}
				if a.AuditBuffer != nil {
					a.AuditBuffer.Log(ActionFileRead, p, StatusSuccess)
				}
				var sb strings.Builder
				if result.Info.Title != "" {
					fmt.Fprintf(&sb, "Title: %s\n", result.Info.Title)
				}
				if result.Info.Author != "" {
					fmt.Fprintf(&sb, "Author: %s\n", result.Info.Author)
				}
				if result.Info.Pages > 0 {
					fmt.Fprintf(&sb, "Pages: %d\n", result.Info.Pages)
				}
				if len(result.DiagramPages) > 0 {
					fmt.Fprintf(&sb, "Diagram-only pages (text extraction incomplete): %v\n", result.DiagramPages)
				}
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(result.Text)
				return capOutput(sb.String(), maxBytes), nil
			}
			// ── Chunking pre-read guard ─────────────────────────────────────────
			if a.Config.Chunking.Enabled && a.Client != nil {
				if rem := remainingContext(a); rem > 0 {
					budget := int(float64(rem) * a.Config.Chunking.Threshold)
					if exceeded, size, statErr := fileExceedsBudget(resolved, budget); statErr == nil && exceeded {
						lastMsg := lastUserMessage(a)
						instruction, cancelled := promptChunkInstruction(a.In, os.Stdout, p, int(size/4), budget, lastMsg)
						if cancelled {
							return "File read cancelled by user.", nil
						}
						// Parse @mention: extract model label, strip from instruction.
						model := a.Client.Name()
						if ac, ok := a.Client.(*AnyLLMClient); ok {
							model = ac.ModelName()
						}
						if mentionName, rest, mentionOK := ParseAtMention(instruction); mentionOK {
							model = mentionName
							instruction = rest
						}
						// Read the full file for chunking.
						chunkData, readErr := os.ReadFile(resolved)
						if readErr != nil {
							if a.AuditBuffer != nil {
								a.AuditBuffer.Log(ActionFileRead, p, StatusError)
							}
							return "", fmt.Errorf("read_file: %w", readErr)
						}
						docType := DetectDocType(resolved)
						chunks := ChunkDocument(string(chunkData), a.Config.Chunking, docType)
						if len(chunks) > a.Config.Chunking.MaxChunks {
							fmt.Fprintf(os.Stdout, "Warning: document split into %d chunks (max %d); proceeding.\n",
								len(chunks), a.Config.Chunking.MaxChunks)
						}
						params := ChunkAnalysisParams{
							Filename:    filepath.Base(p),
							Chunks:      chunks,
							Instruction: instruction,
							Model:       model,
							DocType:     docType,
							Config:      a.Config.Chunking,
						}
						synthesis, synthErr := RunChunkedAnalysis(ctx, a.Client, a.Recorder, params, os.Stdout)
						if synthErr != nil {
							return "", fmt.Errorf("read_file: chunked analysis: %w", synthErr)
						}
						if a.AuditBuffer != nil {
							a.AuditBuffer.Log(ActionFileRead, p, StatusSuccess)
						}
						return synthesis, nil
					}
				}
			}
			// ── Normal read ─────────────────────────────────────────────────────
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
				return "", fmt.Errorf("write_file: 'path' is required and must be a non-empty string — " +
					"provide the destination path relative to the workspace root " +
					"(e.g. {\"path\": \"output.md\", \"content\": \"...\"})")
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
			if !a.CheckWritePermission(filepath.Clean(p)) {
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
			note := ""
			if a.Config.AutoFormat {
				note = applyAutoFormat(a, p, content)
			}
			msg := fmt.Sprintf("wrote %d bytes to %s", len(content), p)
			if note != "" {
				msg += " (" + note + ")"
			}
			return msg, nil
		},
	)

	// ── create_dir ───────────────────────────────────────────────────────────
	r.RegisterTool(
		"create_dir",
		"Create a directory (and any missing parent directories) in the workspace. "+
			"Path must be relative to the workspace root. "+
			"Use this instead of run_command mkdir.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path of the directory to create within the workspace",
				},
			},
			"required": []string{"path"},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			p, ok := args["path"].(string)
			if !ok || p == "" {
				return "", fmt.Errorf("create_dir: 'path' is required and must be a non-empty string")
			}
			if len(p) > maxInputPath {
				return "", fmt.Errorf("create_dir: path exceeds maximum length of %d bytes", maxInputPath)
			}
			if _, err := resolveWorkspacePath(root, p); err != nil {
				return "", fmt.Errorf("create_dir: %w", err)
			}
			if !a.CheckWritePermission(filepath.Clean(p)) {
				if a.AuditBuffer != nil {
					a.AuditBuffer.Log(ActionFileWrite, p, StatusDenied)
				}
				return "", fmt.Errorf("create_dir: write permission denied for %q", p)
			}
			if err := a.Workspace.MkdirAll(p); err != nil {
				if a.AuditBuffer != nil {
					a.AuditBuffer.Log(ActionFileWrite, p, StatusError)
				}
				return "", fmt.Errorf("create_dir: %w", err)
			}
			if a.AuditBuffer != nil {
				a.AuditBuffer.Log(ActionFileWrite, p, StatusSuccess)
			}
			return fmt.Sprintf("created directory %s", p), nil
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

	// ── file_tree ────────────────────────────────────────────────────────────
	r.RegisterTool(
		"file_tree",
		"Display a recursive tree listing of the workspace (or a subdirectory), "+
			"skipping hidden files and directories. "+
			"Path must be relative to the workspace root; defaults to the root.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path to the directory to display (default: workspace root)",
				},
			},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			p := "."
			if v, ok := args["path"].(string); ok && v != "" {
				if len(v) > maxInputPath {
					return "", fmt.Errorf("file_tree: path exceeds maximum length of %d bytes", maxInputPath)
				}
				p = v
			}
			absPath, err := resolveWorkspacePath(root, p)
			if err != nil {
				return "", fmt.Errorf("file_tree: %w", err)
			}
			rel, _ := filepath.Rel(root, absPath)
			var sb strings.Builder
			fmt.Fprintf(&sb, "%s\n", rel)
			printDirTree(absPath, "", &sb)
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
				if d.Type()&fs.ModeSymlink != 0 {
					return nil
				}
				if d.IsDir() {
					if strings.HasPrefix(d.Name(), ".") {
						return filepath.SkipDir
					}
					if path == filepath.Join(a.Workspace.Root, "agents") {
						return filepath.SkipDir
					}
					return nil
				}
				if isAgentsDir(a.Workspace.Root, path) {
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

			if a.Config.Security.SafeMode && !a.Config.IsCommandAllowed(program) {
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
			if a.Config.Security.RunTimeout > 0 {
				runCtx, cancel = context.WithTimeout(ctx, a.Config.Security.RunTimeout)
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

	// ── current_datetime ─────────────────────────────────────────────────────
	r.RegisterTool(
		"current_datetime",
		"Return the current date and time in the system's local timezone and UTC. "+
			"Use this whenever you need to know what time or date it is right now. "+
			"Optional 'format' argument: 'human' (default), 'rfc3339', or 'unix'.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"format": map[string]any{
					"type":        "string",
					"description": "Output format: 'human' (default), 'rfc3339', or 'unix'",
					"enum":        []string{"human", "rfc3339", "unix"},
				},
			},
		},
		func(_ context.Context, args map[string]any) (string, error) {
			now := time.Now()
			utc := now.UTC()
			format, _ := args["format"].(string)
			switch format {
			case "rfc3339":
				return fmt.Sprintf("local: %s\nutc:   %s",
					now.Format(time.RFC3339),
					utc.Format(time.RFC3339),
				), nil
			case "unix":
				return fmt.Sprintf("%d", now.Unix()), nil
			default:
				zone, offset := now.Zone()
				offsetH := offset / 3600
				offsetM := (offset % 3600) / 60
				sign := "+"
				if offset < 0 {
					sign = "-"
					offsetH = -offsetH
					offsetM = -offsetM
				}
				return fmt.Sprintf(
					"local: %s (%s, UTC%s%02d:%02d)\nutc:   %s\nday:   %s\nunix:  %d",
					now.Format("2006-01-02 15:04:05"),
					zone, sign, offsetH, offsetM,
					utc.Format("2006-01-02 15:04:05"),
					now.Weekday().String(),
					now.Unix(),
				), nil
			}
		},
	)

	// ── datetime_diff ─────────────────────────────────────────────────────────
	r.RegisterTool(
		"datetime_diff",
		"Compute the duration between two datetime strings. "+
			"'from' is required; 'to' defaults to now. "+
			"Accepted formats: RFC3339, '2006-01-02T15:04:05', '2006-01-02 15:04:05', '2006-01-02', 'Jan 2 2006', 'January 2 2006'.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"from": map[string]any{
					"type":        "string",
					"description": "Start datetime string",
				},
				"to": map[string]any{
					"type":        "string",
					"description": "End datetime string (default: now)",
				},
			},
			"required": []string{"from"},
		},
		func(_ context.Context, args map[string]any) (string, error) {
			fromStr, ok := args["from"].(string)
			if !ok || fromStr == "" {
				return "", fmt.Errorf("datetime_diff: 'from' must be a non-empty string")
			}
			fromT, err := parseDateTimeString(fromStr)
			if err != nil {
				return "", fmt.Errorf("datetime_diff: 'from': %w", err)
			}
			toT := time.Now()
			if toStr, ok := args["to"].(string); ok && toStr != "" {
				toT, err = parseDateTimeString(toStr)
				if err != nil {
					return "", fmt.Errorf("datetime_diff: 'to': %w", err)
				}
			}
			diff := toT.Sub(fromT)
			direction := "in the future"
			if diff < 0 {
				diff = -diff
				direction = "in the past"
			}
			return fmt.Sprintf("%s (%s)", formatDuration(diff), direction), nil
		},
	)

	// ── format_datetime ───────────────────────────────────────────────────────
	r.RegisterTool(
		"format_datetime",
		"Parse a datetime string and reformat it. "+
			"Accepted input formats: RFC3339, '2006-01-02T15:04:05', '2006-01-02 15:04:05', '2006-01-02', 'Jan 2 2006', 'January 2 2006'. "+
			"Output formats: 'rfc3339', 'human', 'unix', 'date' (YYYY-MM-DD), 'time' (HH:MM:SS).",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"datetime": map[string]any{
					"type":        "string",
					"description": "Input datetime string to reformat",
				},
				"format": map[string]any{
					"type":        "string",
					"description": "Output format: 'rfc3339', 'human', 'unix', 'date', or 'time'",
					"enum":        []string{"rfc3339", "human", "unix", "date", "time"},
				},
			},
			"required": []string{"datetime", "format"},
		},
		func(_ context.Context, args map[string]any) (string, error) {
			dtStr, ok := args["datetime"].(string)
			if !ok || dtStr == "" {
				return "", fmt.Errorf("format_datetime: 'datetime' must be a non-empty string")
			}
			fmtStr, ok := args["format"].(string)
			if !ok || fmtStr == "" {
				return "", fmt.Errorf("format_datetime: 'format' must be a non-empty string")
			}
			t, err := parseDateTimeString(dtStr)
			if err != nil {
				return "", fmt.Errorf("format_datetime: %w", err)
			}
			switch fmtStr {
			case "rfc3339":
				return t.Format(time.RFC3339), nil
			case "unix":
				return fmt.Sprintf("%d", t.Unix()), nil
			case "date":
				return t.Format("2006-01-02"), nil
			case "time":
				return t.Format("15:04:05"), nil
			default: // "human"
				return t.Format("2006-01-02 15:04:05"), nil
			}
		},
	)

	// ── retrieve_memory ───────────────────────────────────────────────────────
	r.RegisterTool(
		"retrieve_memory",
		"Search all memory silos (memory store, RAG store, knowledge base) for entries "+
			"relevant to a query and return the top-k most relevant results. "+
			"Use this mid-session to look up information that was not injected at startup, "+
			"such as past decisions, tool-use patterns, or workspace facts.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Natural-language retrieval query describing what to look for",
				},
				"top_k": map[string]any{
					"type":        "integer",
					"description": "Maximum number of results to return (default 3)",
				},
			},
			"required": []string{"query"},
		},
		func(_ context.Context, args map[string]any) (string, error) {
			if a.Memory == nil || a.Memory.Unified == nil {
				return "retrieve_memory: memory system is not available in this session.", nil
			}
			query, ok := args["query"].(string)
			if !ok || strings.TrimSpace(query) == "" {
				return "", fmt.Errorf("retrieve_memory: query must be a non-empty string")
			}
			topK := 3
			if v, ok := args["top_k"].(float64); ok && int(v) > 0 {
				topK = int(v)
			}

			var embedder Embedder
			if entry := a.Config.Memory.ActiveRagStore(); entry != nil {
				embedder = NewEmbedderForEntry(entry, a.Config.Ollama.URL)
			}

			budget := topK * 300
			if a.Config.Ollama.ContextLength > 0 && a.Config.Memory.BudgetPct > 0 {
				budget = int(float64(a.Config.Ollama.ContextLength) * a.Config.Memory.BudgetPct)
			}

			results, err := a.Memory.Unified.Recall(query, embedder, budget)
			if err != nil {
				return fmt.Sprintf("retrieve_memory: recall error: %v", err), nil
			}
			if len(results) > topK {
				results = results[:topK]
			}
			if len(results) == 0 {
				return "retrieve_memory: no matching memories found.", nil
			}
			return FormatContext(results), nil
		},
	)

	// ── update_memory ────────────────────────────────────────────────────────
	r.RegisterTool(
		"update_memory",
		"Update the content of an existing memory by ID. "+
			"Replaces the description, summary, and Fountain body with new content and refreshes the timestamp.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "ID of the memory to update (e.g. tool_use_a3f891)",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "New content to replace the existing description and summary",
				},
			},
			"required": []string{"id", "content"},
		},
		func(_ context.Context, args map[string]any) (string, error) {
			if a.Memory == nil || a.Memory.Store == nil {
				return "update_memory: memory store is not available in this session.", nil
			}
			id, ok := args["id"].(string)
			if !ok || strings.TrimSpace(id) == "" {
				return "", fmt.Errorf("update_memory: id must be a non-empty string")
			}
			content, ok := args["content"].(string)
			if !ok || strings.TrimSpace(content) == "" {
				return "", fmt.Errorf("update_memory: content must be a non-empty string")
			}

			doc, err := a.Memory.Store.ByID(id)
			if err != nil {
				return "", fmt.Errorf("update_memory: %w", err)
			}
			if doc == nil {
				return fmt.Sprintf("update_memory: memory %q not found.", id), nil
			}

			// Safe-mode guard: describe without modifying.
			if a.Config.Security.SafeMode {
				return fmt.Sprintf(
					"update_memory [safe mode]: would update %q with new content — disable safe mode to apply.",
					id,
				), nil
			}

			now := time.Now().UTC()
			doc.Meta.Description = content
			doc.Meta.Summary = content
			doc.Meta.UpdatedAt = now.Format(time.RFC3339)
			doc.FountainBody = BuildFountainBody(now.UTC().Format("2006-01-02 15:04:05"), [][2]string{{"HARVEY", content}})

			if err := a.Memory.Store.Save(doc, nil); err != nil {
				return "", fmt.Errorf("update_memory: %w", err)
			}

			if a.Recorder != nil {
				_ = a.Recorder.RecordAgentAction("update_memory", id, "auto", "ok")
			}

			return "Memory updated: " + id, nil
		},
	)

	// ── delete_memory ─────────────────────────────────────────────────────────
	r.RegisterTool(
		"delete_memory",
		"Archive a memory by ID, removing it from active retrieval. "+
			"Matches /memory forget semantics: the file is moved to the archive directory.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "ID of the memory to archive (e.g. tool_use_a3f891)",
				},
			},
			"required": []string{"id"},
		},
		func(_ context.Context, args map[string]any) (string, error) {
			if a.Memory == nil || a.Memory.Store == nil {
				return "delete_memory: memory store is not available in this session.", nil
			}
			id, ok := args["id"].(string)
			if !ok || strings.TrimSpace(id) == "" {
				return "", fmt.Errorf("delete_memory: id must be a non-empty string")
			}

			// Check existence before safe-mode so we can report "not found" early.
			doc, err := a.Memory.Store.ByID(id)
			if err != nil {
				return "", fmt.Errorf("delete_memory: %w", err)
			}
			if doc == nil {
				return fmt.Sprintf("delete_memory: memory %q not found.", id), nil
			}

			// Safe-mode guard: describe without archiving.
			if a.Config.Security.SafeMode {
				return fmt.Sprintf(
					"delete_memory [safe mode]: would archive %q — disable safe mode to apply.",
					id,
				), nil
			}

			if err := a.Memory.Store.Archive(id); err != nil {
				return "", fmt.Errorf("delete_memory: %w", err)
			}

			if a.Recorder != nil {
				_ = a.Recorder.RecordAgentAction("delete_memory", id, "auto", "archived")
			}

			return "Memory archived: " + id, nil
		},
	)

	// ── filter_context ───────────────────────────────────────────────────────
	// filterThreshold is the cosine similarity above which a message is
	// considered to match the filter criteria and is removed from history.
	const filterThreshold = 0.6

	r.RegisterTool(
		"filter_context",
		"Remove conversation history messages that match a given criteria. "+
			"When a RAG store is active, matching uses cosine similarity (threshold 0.6). "+
			"Otherwise falls back to case-insensitive keyword matching. "+
			"System messages are never removed.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"criteria": map[string]any{
					"type":        "string",
					"description": "Natural-language description or keyword of the content to remove from history",
				},
			},
			"required": []string{"criteria"},
		},
		func(_ context.Context, args map[string]any) (string, error) {
			criteria, ok := args["criteria"].(string)
			if !ok || strings.TrimSpace(criteria) == "" {
				return "", fmt.Errorf("filter_context: criteria must be a non-empty string")
			}

			if len(a.History) == 0 {
				return "filter_context: nothing to filter.", nil
			}

			// Try to get a vector embedder from the active RAG store.
			var embedder Embedder
			if entry := a.Config.Memory.ActiveRagStore(); entry != nil {
				embedder = NewEmbedderForEntry(entry, a.Config.Ollama.URL)
			}

			// Embed the criteria once (fails silently → keyword fallback).
			var criteriaVec []float64
			useEmbedding := false
			if embedder != nil {
				if vec, err := embedder.Embed(criteria); err == nil {
					criteriaVec = vec
					useEmbedding = true
				}
			}

			lowerCriteria := strings.ToLower(criteria)

			// Classify each message as keep or remove.
			var keep []Message
			removed := 0
			for _, m := range a.History {
				if m.Role == "system" {
					keep = append(keep, m)
					continue
				}
				var matched bool
				if useEmbedding {
					msgVec, err := embedder.Embed(m.Content)
					if err == nil {
						matched = cosineSimilarity(criteriaVec, msgVec) >= filterThreshold
					} else {
						matched = strings.Contains(strings.ToLower(m.Content), lowerCriteria)
					}
				} else {
					matched = strings.Contains(strings.ToLower(m.Content), lowerCriteria)
				}
				if matched {
					removed++
				} else {
					keep = append(keep, m)
				}
			}

			// Safe-mode guard: describe without modifying.
			if a.Config.Security.SafeMode {
				return fmt.Sprintf(
					"filter_context [safe mode]: would remove %d messages matching %q — disable safe mode to apply.",
					removed, criteria,
				), nil
			}

			a.History = keep

			if a.Recorder != nil {
				_ = a.Recorder.RecordAgentAction("filter_context",
					fmt.Sprintf("%d messages removed matching %q", removed, criteria), "auto", "ok")
			}

			return fmt.Sprintf("Filtered %d messages matching %q.", removed, criteria), nil
		},
	)

	// ── add_memory ───────────────────────────────────────────────────────────
	r.RegisterTool(
		"add_memory",
		"Save a new memory to the persistent memory store. "+
			"Use this to record important findings, decisions, preferences, or facts that should persist across sessions. "+
			"memory_type must be one of: tool_use, workflow, user_preference, workspace_profile, project_fact.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{
					"type":        "string",
					"description": "The memory content to save (used as description and summary)",
				},
				"memory_type": map[string]any{
					"type":        "string",
					"description": "Memory type: tool_use, workflow, user_preference, workspace_profile, or project_fact",
					"enum":        []string{"tool_use", "workflow", "user_preference", "workspace_profile", "project_fact"},
				},
				"tags": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional keyword tags for filtering",
				},
			},
			"required": []string{"content", "memory_type"},
		},
		func(_ context.Context, args map[string]any) (string, error) {
			if a.Memory == nil || a.Memory.Store == nil {
				return "add_memory: memory store is not available in this session.", nil
			}
			content, ok := args["content"].(string)
			if !ok || strings.TrimSpace(content) == "" {
				return "", fmt.Errorf("add_memory: content must be a non-empty string")
			}
			memTypeStr, ok := args["memory_type"].(string)
			if !ok || memTypeStr == "" {
				return "", fmt.Errorf("add_memory: memory_type must be a non-empty string")
			}
			if !isValidMemoryType(MemoryType(memTypeStr)) {
				valid := make([]string, len(ValidMemoryTypes))
				for i, vt := range ValidMemoryTypes {
					valid[i] = string(vt)
				}
				return fmt.Sprintf("add_memory: invalid memory_type %q; must be one of: %s",
					memTypeStr, strings.Join(valid, ", ")), nil
			}

			// Parse optional tags from the JSON array (arrives as []interface{}).
			var tags []string
			if raw, hasTag := args["tags"]; hasTag && raw != nil {
				if arr, ok := raw.([]interface{}); ok {
					for _, item := range arr {
						if s, ok := item.(string); ok {
							tags = append(tags, s)
						}
					}
				}
			}

			// Safe-mode guard: describe without writing.
			if a.Config.Security.SafeMode {
				return fmt.Sprintf(
					"add_memory [safe mode]: would save %q as %s — disable safe mode to apply.",
					content, memTypeStr,
				), nil
			}

			memType := MemoryType(memTypeStr)
			id := GenerateMemoryID(memType)
			doc := NewMemoryDoc(id, memType, content, content, tags)
			ts := time.Now().UTC().Format("2006-01-02 15:04:05")
			doc.FountainBody = BuildFountainBody(ts, [][2]string{{"HARVEY", content}})

			if err := a.Memory.Store.Save(doc, nil); err != nil {
				return "", fmt.Errorf("add_memory: %w", err)
			}

			if a.Recorder != nil {
				_ = a.Recorder.RecordAgentAction("add_memory", id+" — "+memTypeStr, "auto", "ok")
			}

			return "Memory saved: " + id, nil
		},
	)

	// ── summary_context ──────────────────────────────────────────────────────
	r.RegisterTool(
		"summary_context",
		"Compress a span of conversation history into a single summary entry to free up context. "+
			"'span' is \"all\" (summarise every non-system message) or an integer string like \"10\" "+
			"to summarise the oldest N non-system messages, leaving the most recent ones intact.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"span": map[string]any{
					"type":        "string",
					"description": "\"all\" or an integer N — oldest N non-system messages to summarise",
				},
			},
			"required": []string{"span"},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			if a.Client == nil {
				return "summary_context: no LLM client available in this session.", nil
			}
			span, _ := args["span"].(string)
			if span == "" {
				span = "all"
			}

			// Separate leading system prompt from conversation turns.
			var leadingSystem *Message
			turns := a.History
			if len(turns) > 0 && turns[0].Role == "system" {
				msg := turns[0]
				leadingSystem = &msg
				turns = turns[1:]
			}

			// Determine how many turns to summarise.
			n := len(turns)
			if strings.ToLower(span) != "all" {
				if parsed, err := strconv.Atoi(span); err == nil && parsed > 0 && parsed < n {
					n = parsed
				}
			}

			if n < 2 {
				return "summary_context: not enough history to summarise (need at least 2 non-system messages).", nil
			}

			toSummarise := turns[:n]
			keep := turns[n:]

			// Safe-mode guard: describe without modifying history.
			if a.Config.Security.SafeMode {
				return fmt.Sprintf(
					"summary_context [safe mode]: would summarise %d messages into 1 entry (%d messages remain); disable safe mode to apply.",
					n, len(keep),
				), nil
			}

			// Build the summarisation request from the selected turns.
			var textBuf strings.Builder
			for _, m := range toSummarise {
				if m.Content != "" {
					fmt.Fprintf(&textBuf, "%s: %s\n\n", m.Role, m.Content)
				}
			}

			request := []Message{
				{
					Role:    "system",
					Content: "You are a summariser. Summarise the following conversation concisely. Focus on key decisions, files changed, errors resolved, and context the user provided.",
				},
				{Role: "user", Content: textBuf.String()},
			}

			var chatBuf strings.Builder
			if _, err := a.Client.Chat(ctx, request, &chatBuf); err != nil {
				return "", fmt.Errorf("summary_context: %w", err)
			}
			summary := strings.TrimSpace(chatBuf.String())
			if summary == "" {
				return "summary_context: LLM returned an empty summary — history unchanged.", nil
			}

			tokensSaved := textBuf.Len() / 4

			// Rebuild history: [system_prompt?] + [summary] + [kept turns].
			summaryMsg := Message{Role: "system", Content: "[Summary] " + summary}
			newHistory := make([]Message, 0, 2+len(keep))
			if leadingSystem != nil {
				newHistory = append(newHistory, *leadingSystem)
			}
			newHistory = append(newHistory, summaryMsg)
			newHistory = append(newHistory, keep...)
			a.History = newHistory

			if a.Recorder != nil {
				_ = a.Recorder.RecordAgentAction("summary_context", fmt.Sprintf("%d messages compressed", n), "auto", "ok")
			}

			return fmt.Sprintf("Summarised %d turns into 1 entry (~%d tokens saved).", n, tokensSaved), nil
		},
	)

	// ── whoami ────────────────────────────────────────────────────────────────
	r.RegisterTool(
		"whoami",
		"Return the identity of the current user: OS username, git user name and email, and hostname. "+
			"Useful when authoring reviews, commit messages, or project documents that need the author's name.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		func(_ context.Context, _ map[string]any) (string, error) {
			osUser := os.Getenv("USER")
			if osUser == "" {
				osUser = os.Getenv("USERNAME")
			}
			hostname, _ := os.Hostname()

			gitName := gitConfigValue("user.name")
			gitEmail := gitConfigValue("user.email")

			var b strings.Builder
			if osUser != "" {
				fmt.Fprintf(&b, "OS user:    %s\n", osUser)
			}
			if hostname != "" {
				fmt.Fprintf(&b, "Hostname:   %s\n", hostname)
			}
			if gitName != "" {
				fmt.Fprintf(&b, "Git name:   %s\n", gitName)
			}
			if gitEmail != "" {
				fmt.Fprintf(&b, "Git email:  %s\n", gitEmail)
			}
			if b.Len() == 0 {
				return "No identity information available.", nil
			}
			return b.String(), nil
		},
	)
}

// gitConfigValue reads a single git config key from the global git config.
// Returns "" when the key is absent or git is unavailable.
func gitConfigValue(key string) string {
	out, err := exec.Command("git", "config", "--global", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// parseDateTimeString tries a sequence of common datetime layouts and returns
// the first successful parse. Returns an error when no layout matches.
func parseDateTimeString(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"Jan 2 2006",
		"January 2 2006",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse %q: try RFC3339 (e.g. 2006-01-02T15:04:05Z) or YYYY-MM-DD", s)
}

// formatDuration returns a human-readable duration string, e.g. "2 days, 3 hours, 14 minutes".
// Sub-minute durations are reported as seconds.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d day%s", days, plural(days)))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d hour%s", hours, plural(hours)))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d minute%s", minutes, plural(minutes)))
	}
	if len(parts) == 0 {
		return "less than a minute"
	}
	return strings.Join(parts, ", ")
}

// plural returns "s" when n != 1.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// applyAutoFormat runs the registered formatter for the file extension of p,
// rewrites the file on disk when the formatter produces different content, and
// returns a short status note ("formatted", "already formatted", or "").
// Errors are silently suppressed so that a missing or failing formatter never
// breaks a write_file call.
func applyAutoFormat(a *Agent, relPath string, original string) string {
	ext := filepath.Ext(relPath)
	if ext == "" {
		return ""
	}
	langID, ok := globalRegistry.DetectFromExtension(ext)
	if !ok {
		return ""
	}
	f := globalRegistry.GetFormatter(langID)
	if f == nil {
		return ""
	}
	// File-mode formatters require safe_mode=false.
	if f.Mode() == FileFormatter && a.Config.Security.SafeMode {
		return ""
	}
	var absPath string
	if a.Workspace != nil {
		p, err := a.Workspace.AbsPath(relPath)
		if err != nil {
			return ""
		}
		absPath = p
	} else {
		absPath = relPath
	}
	formatted, err := f.Format(original, absPath)
	if err != nil || formatted == original {
		if err != nil {
			return ""
		}
		return "already formatted"
	}
	// Rewrite only when something changed.
	if werr := os.WriteFile(absPath, []byte(formatted), 0o644); werr != nil {
		return ""
	}
	return "formatted"
}

/** promptChunkInstruction displays the context-overflow alert to out, reads a
 * chunk instruction from in, and returns it. When the user types "no" (or
 * presses Enter with no input and no suggestion), cancelled is true.
 * When the user presses Enter with an empty line and a non-empty suggestion,
 * the suggestion is accepted and returned as instruction.
 *
 * Parameters:
 *   in               (io.Reader) — source for user input (typically a.In).
 *   out              (io.Writer) — destination for the alert message.
 *   filename         (string)    — display name of the file that overflowed context.
 *   estimatedTokens  (int)       — estimated token count of the file (size/4).
 *   budget           (int)       — remaining context budget in tokens.
 *   suggestion       (string)    — pre-filled instruction (last user message); may be empty.
 *
 * Returns:
 *   instruction (string) — the user's chunk prompt, or suggestion when Enter is pressed.
 *   cancelled   (bool)   — true when the user typed "no" or accepted an empty suggestion.
 *
 * Example:
 *   instr, cancelled := promptChunkInstruction(a.In, os.Stdout, "doc.md", 8000, 3600, lastMsg)
 */
func promptChunkInstruction(in io.Reader, out io.Writer, filename string, estimatedTokens, budget int, suggestion string) (instruction string, cancelled bool) {
	fmt.Fprintf(out, "\nContext overflow: %s is approximately %d tokens; %d tokens remain in current context.\n",
		filename, estimatedTokens, budget)
	fmt.Fprintln(out, "Enter instructions to process each chunk in turn, or \"no\"/\"/exit\" to cancel.")
	if suggestion != "" {
		fmt.Fprintf(out, "[%s]\n", suggestion)
	}
	fmt.Fprint(out, "> ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		if suggestion != "" {
			return suggestion, false
		}
		return "", true
	}
	switch strings.ToLower(line) {
	case "no", "cancel", "q", "/exit", "/quit":
		return "", true
	}
	return line, false
}

// lastUserMessage returns the content of the most recent user-role message in
// the agent's history, or "" when history is empty or has no user messages.
func lastUserMessage(a *Agent) string {
	for i := len(a.History) - 1; i >= 0; i-- {
		if a.History[i].Role == "user" {
			return a.History[i].Content
		}
	}
	return ""
}
