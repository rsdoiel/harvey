package harvey

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/rsdoiel/termlib"
)

// ANSI escape codes for terminal styling.
const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiCyan    = "\033[36m"
	ansiRed     = "\033[31m"
	ansiMagenta = "\033[35m"
	ansiBlue    = "\033[34m"
	sep         = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
)

// sensitiveEnvPrefixes contains environment variable name prefixes that
// should be EXCLUDED to prevent accidental exposure of sensitive data (e.g., API keys).
// Variables matching these prefixes will NOT be passed to child processes.
var sensitiveEnvPrefixes = []string{
	"ANTHROPIC_API_KEY",
	"DEEPSEEK_API_KEY",
	"GEMINI_API_KEY",
	"GOOGLE_API_KEY",
	"MISTRAL_API_KEY",
	"OPENAI_API_KEY",
}

// safeEnvPrefixes contains environment variable name prefixes that are
// SAFE to pass to child processes. All other variables are filtered out.
var safeEnvPrefixes = []string{
	"PATH",
	"HOME",
	"USER",
	"USERNAME",
	"SHELL",
	"TERM",
	"LANG",
	"LC_",
	"PWD",
	"OLLAMA",
	"HARVEY",
}

/** filterEnvironment returns a filtered copy of the environment safe to pass to
 * child processes. Delegates to filterCommandEnvironment which is the canonical
 * implementation shared with the /run command.
 *
 * Parameters:
 *   env ([]string) — the original environment in "KEY=VALUE" format.
 *
 * Returns:
 *   []string — filtered environment with only safe variables.
 */
func filterEnvironment(env []string) []string {
	return filterCommandEnvironment(env)
}

/** parseCommandLine splits a command line string into a program name and
 * arguments, handling quoted strings. This provides basic shell-like parsing
 * without supporting shell metacharacters (|, >, <, &, ;, etc.) for security.
 *
 * Supported features:
 *   - Single and double quotes
 *   - Escaped quotes with backslash (\' and \")
 *   - Basic whitespace splitting
 *
 * Not supported (intentionally for security):
 *   - Pipes (|)
 *   - Redirects (>, <, >>, 2>, etc.)
 *   - Command substitution ($(), backticks)
 *   - Globbing (*)
 *   - Semicolons (;)
 *   - Background processes (&)
 *
 * Parameters:
 *   line (string) — the command line to parse.
 *
 * Returns:
 *   program (string) — the command/program name.
 *   args ([]string)   — the arguments (not including the program).
 *   error            — if parsing fails (e.g., unclosed quotes).
 *
 * Example:
 *   prog, args, _ := parseCommandLine("grep -r 'hello world' .")
 *   // prog = "grep", args = ["-r", "hello world", "."]
 */
func parseCommandLine(line string) (program string, args []string, err error) {
	var tokens []string
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	escapeNext := false

	for _, ch := range line {
		if escapeNext {
			current.WriteRune(ch)
			escapeNext = false
			continue
		}

		switch ch {
		case '\\':
			escapeNext = true
		case '\'':
			if inDoubleQuote {
				current.WriteRune(ch)
			} else {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if inSingleQuote {
				current.WriteRune(ch)
			} else {
				inDoubleQuote = !inDoubleQuote
			}
		case ' ', '\t':
			if !inSingleQuote && !inDoubleQuote {
				if current.Len() > 0 {
					tokens = append(tokens, current.String())
					current.Reset()
				}
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	// Handle the last token
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	// Check for unclosed quotes
	if inSingleQuote || inDoubleQuote {
		return "", nil, fmt.Errorf("unclosed quote in command line")
	}

	if len(tokens) == 0 {
		return "", nil, fmt.Errorf("empty command")
	}

	return tokens[0], tokens[1:], nil
}

func bold(s string) string    { return ansiBold + s + ansiReset }
func dim(s string) string     { return ansiDim + s + ansiReset }
func green(s string) string   { return ansiGreen + s + ansiReset }
func yellow(s string) string  { return ansiYellow + s + ansiReset }
func red(s string) string     { return ansiRed + s + ansiReset }
func cyan(s string) string    { return ansiCyan + s + ansiReset }
func magenta(s string) string { return ansiMagenta + s + ansiReset }
func blue(s string) string    { return ansiBlue + s + ansiReset }

// prompt returns the input prompt string reflecting the current backend state.
func (a *Agent) prompt() string {
	if a.Client == nil {
		return "harvey (no backend) > "
	}
	return "harvey > "
}

/** Run prints the startup banner, initialises the workspace and knowledge base,
 * runs the backend selection sequence, then starts the interactive REPL. It
 * reads from os.Stdin and writes to out.
 *
 * Parameters:
 *   out (io.Writer) — destination for all REPL output.
 *
 * Returns:
 *   error — only on fatal startup errors; normal exit returns nil.
 *
 * Example:
 *   agent := NewAgent(DefaultConfig(), ws)
 *   if err := agent.Run(os.Stdout); err != nil {
 *       log.Fatal(err)
 *   }
 */
func (a *Agent) Run(out io.Writer) error {
	a.registerCommands()
	if v := os.Getenv("OLLAMA_CONTEXT_LENGTH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			a.Config.OllamaContextLength = n
		}
	}
	defer func() {
		if a.Recorder != nil {
			path := a.Recorder.Path()
			a.Recorder.Close()
			a.Recorder = nil
			fmt.Fprintf(out, dim("  Session saved to %s\n"), path)
		}
	}()
	// reader is used only for startup yes/no prompts. A 1-byte buffer prevents
	// it from consuming bytes that the LineEditor needs for the REPL loop.
	reader := bufio.NewReaderSize(os.Stdin, 1)
	le := termlib.NewLineEditor(os.Stdin, out)

	// Banner
	fmt.Fprintln(out, cyan(bold(sep)))
	fmt.Fprintf(out, "  %s  %s\n", bold("Harvey"), dim(Version))
	fmt.Fprintln(out, cyan(bold(sep)))

	// Workspace
	if err := a.initWorkspace(out); err != nil {
		return err
	}

	// harvey/harvey.yaml — apply path overrides before any path-dependent init.
	if err := LoadHarveyYAML(a.Workspace, a.Config); err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" harvey.yaml: %v\n", err)
	}

	// Knowledge base
	a.initKnowledgeBase(out)

	// Model capability cache
	a.initModelCache(out)

	// RAG store (optional — only when configured in harvey.yaml)
	a.initRag(out)

	// Sessions directory
	sessDir, err := ResolveSessionsDir(a.Workspace, a.Config.SessionsDir)
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" Sessions dir: %v\n", err)
	} else {
		a.SessionsDir = sessDir
		fmt.Fprintf(out, green("✓")+" Sessions: %s\n", sessDir)
	}

	// Session resume — offer before backend selection so the chosen session's
	// model can pre-select the Ollama model below.
	var resumePath string
	var sessionModel string
	if a.Config.ContinuePath == "" { // --continue flag bypasses the picker
		resumePath, sessionModel = a.pickSession(reader, out, sessDir)
	}

	// System prompt
	if a.Config.SystemPrompt != "" {
		expanded := ExpandDynamicSections(a.Config.SystemPrompt, a.Workspace)
		fmt.Fprintln(out, green("✓")+" Loaded HARVEY.md as system prompt")
		a.AddMessage("system", expanded)
	} else {
		fmt.Fprintln(out, dim("  No HARVEY.md found in current directory"))
	}

	// Skills — scan and inject catalog into system prompt
	a.loadSkills(out)

	// Backend selection — use sessionModel as the preferred model hint.
	if err := a.selectBackend(reader, out, sessionModel); err != nil {
		return err
	}

	// Resume history from chosen session file.
	if resumePath != "" {
		n, contErr := a.ContinueFromFountain(resumePath)
		if contErr != nil {
			fmt.Fprintf(out, yellow("  ✗")+" Resume failed: %v\n", contErr)
		} else {
			fmt.Fprintf(out, green("✓")+" Resumed %d turns from %s\n", n, resumePath)
		}
	}

	// Auto-start recording.
	if a.Config.AutoRecord {
		recPath := a.Config.RecordPath
		if recPath == "" {
			recPath = DefaultSessionPath(a.SessionsDir)
		}
		model := "none"
		if a.Client != nil {
			model = a.Client.Name()
		}
		if rec, err := NewRecorder(recPath, model, a.Workspace.Root); err != nil {
			fmt.Fprintf(out, yellow("  ✗")+" Auto-record failed: %v\n", err)
		} else {
			a.Recorder = rec
			fmt.Fprintf(out, green("✓")+" Recording to %s\n", recPath)
		}
	}

	// Replay mode — run turns from a session file and return without entering REPL.
	if a.Config.ReplayPath != "" {
		outPath := a.Config.ReplayOutputPath
		if outPath == "" {
			outPath = DefaultSessionPath(a.SessionsDir)
		}
		replayCtx, replayCancel := context.WithCancel(context.Background())
		defer replayCancel()
		fmt.Fprintln(out, cyan(bold(sep)))
		fmt.Fprintf(out, "  Replay mode: %s\n", a.Config.ReplayPath)
		fmt.Fprintln(out, cyan(bold(sep)))
		fmt.Fprintln(out)
		return a.ReplayFromFountain(replayCtx, a.Config.ReplayPath, outPath, out)
	}

	// --continue flag: pre-load history from a named session file.
	if a.Config.ContinuePath != "" {
		n, contErr := a.ContinueFromFountain(a.Config.ContinuePath)
		if contErr != nil {
			fmt.Fprintf(out, yellow("  ✗")+" Continue failed: %v\n", contErr)
		} else {
			fmt.Fprintf(out, green("✓")+" Loaded %d turns from %s\n", n, a.Config.ContinuePath)
		}
	}

	// Ready line
	fmt.Fprintln(out, cyan(bold(sep)))
	if a.Client != nil {
		fmt.Fprintf(out, "  Connected: %s\n", green(a.Client.Name()))
	} else {
		fmt.Fprintf(out, "  %s\n", yellow("No backend — use /ollama start"))
	}
	fmt.Fprintln(out, dim("  /help for commands · /exit to quit"))
	fmt.Fprintln(out, cyan(bold(sep)))
	fmt.Fprintln(out)

	// REPL
	for {
		input, err := le.Prompt(a.prompt())
		if err == io.EOF || err == termlib.ErrInterrupted {
			fmt.Fprintln(out, dim("Goodbye."))
			return nil
		}
		if err != nil {
			return err
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/") {
			le.AppendHistory(input)
			shouldExit, cmdErr := a.dispatch(input, out)
			if cmdErr != nil {
				fmt.Fprintf(out, red("Error: ")+"%v\n", cmdErr)
			}
			if shouldExit {
				break
			}
			continue
		}

		// "!" prefix — run a shell command, stream output live, inject into context.
		// Security: Parses the command line to avoid shell injection via sh -c.
		// Supports simple commands with quoted arguments but does NOT support
		// shell metacharacters (|, >, <, &, ;, etc.) for security.
		if strings.HasPrefix(input, "!") {
			cmdLine := strings.TrimSpace(strings.TrimPrefix(input, "!"))
			if cmdLine == "" {
				continue
			}
			le.AppendHistory(input)
			fmt.Fprintf(out, "  $ %s\n", cmdLine)

			// Parse command line into program and arguments
			// Uses shell-like quoting but does NOT process shell metacharacters
			program, args, err := parseCommandLine(cmdLine)
			if err != nil {
				fmt.Fprintf(out, red("Error parsing command: %v\n"), err)
				continue
			}

			// Safe mode check: verify command is in allowlist
			if a.Config.SafeMode && !a.Config.IsCommandAllowed(program) {
				if a.AuditBuffer != nil {
					a.AuditBuffer.Log(ActionCommand, program+" "+strings.Join(args, " "), StatusDenied)
				}
				fmt.Fprintf(out, yellow("  Command %q is not allowed in safe mode.\n"), program)
				fmt.Fprintf(out, "  Allowed commands: %s\n", strings.Join(a.Config.AllowedCommands, ", "))
				fmt.Fprintln(out, "  Use /safemode off to disable, or /safemode allow CMD to add it.")
				continue
			}

			// Log allowed command execution
			if a.AuditBuffer != nil {
				a.AuditBuffer.Log(ActionCommand, program+" "+strings.Join(args, " "), StatusAllowed)
			}

			var capBuf bytes.Buffer
			mw := io.MultiWriter(out, &capBuf)

			var bashCtx context.Context
			var cancelBash context.CancelFunc
			if a.Config.RunTimeout > 0 {
				bashCtx, cancelBash = context.WithTimeout(context.Background(), a.Config.RunTimeout)
			} else {
				bashCtx, cancelBash = context.WithCancel(context.Background())
			}
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt)
			wasCancelled := false
			watchDone := make(chan struct{})
			go func() {
				defer signal.Stop(sigCh)
				select {
				case <-sigCh:
					wasCancelled = true
					cancelBash()
				case <-watchDone:
				}
			}()

			shCmd := exec.CommandContext(bashCtx, program, args...)
			if a.Workspace != nil {
				shCmd.Dir = a.Workspace.Root
			}
			// Restrict environment to prevent inheriting sensitive variables
			shCmd.Env = filterEnvironment(os.Environ())
			shCmd.Stdout = mw
			shCmd.Stderr = mw
			runErr := shCmd.Run()
			close(watchDone)
			cancelBash()

			fmt.Fprintln(out)
			if wasCancelled || errors.Is(runErr, context.Canceled) {
				fmt.Fprintln(out, dim("  Cancelled."))
				continue
			}

			exitCode := 0
			exitNote := ""
			if shCmd.ProcessState != nil {
				exitCode = shCmd.ProcessState.ExitCode()
				if exitCode != 0 {
					exitNote = fmt.Sprintf(" (exit %d)", exitCode)
				}
			}

			output := capBuf.Bytes()
			truncated := false
			if len(output) > maxRunOutput {
				output = output[:maxRunOutput]
				truncated = true
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("[context: ! %s%s]\n\n```\n", cmdLine, exitNote))
			sb.Write(output)
			if truncated {
				sb.WriteString("\n... (output truncated)")
			}
			sb.WriteString("\n```\n")
			a.AddMessage("user", sb.String())
			fmt.Fprintf(out, dim("  %d bytes added to context%s.\n"), len(output), exitNote)

			if a.Recorder != nil {
				if recErr := a.Recorder.RecordShellCommand(cmdLine, string(output), exitCode); recErr != nil {
					fmt.Fprintf(out, yellow("  ✗")+" Recording error: %v\n", recErr)
				}
			}
			continue
		}

		// @mention dispatch — send prompt to a registered remote endpoint.
		if name, prompt, ok := ParseAtMention(input); ok {
			if a.Routes == nil || !a.Routes.Enabled {
				fmt.Fprintln(out, yellow("  Routing is off.")+" Use /route on to enable @mentions.")
				continue
			}
			ep := a.Routes.Lookup(name)
			if ep == nil {
				fmt.Fprintf(out, yellow("  @%s not found.")+" Use /route list to see registered endpoints.\n", name)
				continue
			}
			le.AppendHistory(input)
			fmt.Fprintf(out, dim("  → dispatching to @%s\n"), name)
			fmt.Fprintln(out)

			dispCtx, cancelDisp := context.WithCancel(context.Background())
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt)
			wasCancelled := false
			watchDone := make(chan struct{})
			go func() {
				defer signal.Stop(sigCh)
				select {
				case <-sigCh:
					wasCancelled = true
					cancelDisp()
				case <-watchDone:
				}
			}()

			sp := newSpinner(out, 0, "@"+name+" · working")
			reply, dispErr := DispatchToEndpoint(dispCtx, ep, a.History, prompt, a.Config, out)
			sp.stop()
			close(watchDone)
			cancelDisp()

			if wasCancelled || errors.Is(dispErr, context.Canceled) {
				fmt.Fprintln(out, dim("  Cancelled."))
				continue
			}
			if dispErr != nil {
				fmt.Fprintf(out, red("Error: ")+"%v\n", dispErr)
				continue
			}

			fmt.Fprintln(out)
			fmt.Fprintln(out, dim("  @"+name))
			a.AddMessage("user", input)
			a.AddMessage("assistant", reply)

			if a.Recorder != nil {
				if recErr := a.Recorder.RecordTurn(input, reply); recErr != nil {
					fmt.Fprintf(out, yellow("  ✗")+" Recording error: %v\n", recErr)
				}
			}
			a.autoExecuteReply(reply, out, reader, context.Background())
			continue
		}

		// Chat
		if a.Client == nil {
			fmt.Fprintln(out, yellow("No backend connected.")+" Use /ollama start.")
			continue
		}

		le.AppendHistory(input)

		// Intercept skill-related questions and answer directly from the
		// catalog. Small models (e.g. tinyllama) reliably ignore the
		// <available_skills> system-prompt block, so we handle these
		// locally rather than letting the LLM hallucinate an answer.
		if len(a.Skills) > 0 && LooksLikeSkillQuery(input) {
			fmt.Fprintln(out, dim("  (answered from skill catalog — use /skill load NAME to activate a skill)"))
			cmdSkill(a, []string{"list"}, out)
			continue
		}

		// Auto-dispatch compiled skills whose trigger pattern matches the input.
		// Iterate sorted names for deterministic first-match semantics.
		if len(a.Skills) > 0 {
			triggered := false
			for _, name := range SortedSkillNames(a.Skills) {
				skill := a.Skills[name]
				if MatchesTrigger(skill, input) {
					fmt.Fprintf(out, dim("  (trigger matched skill %q)\n"), name)
					triggerReader := bufio.NewReaderSize(a.In, 1)
					if err := DispatchSkill(context.Background(), a, skill, input, triggerReader, out); err != nil {
						fmt.Fprintf(out, red("  ✗ skill dispatch error: ")+"%v\n", err)
					}
					triggered = true
					break
				}
			}
			if triggered {
				continue
			}
		}

		// RAG context injection — prepend relevant chunks before sending.
		augmented := a.ragAugment(input)
		a.AddMessage("user", augmented)

		// Token-count warning — runs only when the backend is Ollama.
		if ac, ok := a.Client.(*AnyLLMClient); ok && ac.ProviderName() == "ollama" {
			histText := HistoryText(a.History)
			n, exact := CountTokens(context.Background(), ac.BackendURL(), ac.ModelName(), histText)
			limit := a.Config.OllamaContextLength
			qualifier := "~"
			if exact {
				qualifier = ""
			}
			if limit > 0 {
				pct := n * 100 / limit
				switch {
				case pct >= 100:
					fmt.Fprintf(out, red("  ✗ Context full: %s%d / %d tokens (%d%%) — reply may be truncated\n"), qualifier, n, limit, pct)
				case pct >= 80:
					fmt.Fprintf(out, yellow("  ⚠ Context %d%% full: %s%d / %d tokens\n"), pct, qualifier, n, limit)
				}
			}
		}
		var modelsUsed []string
		if a.Client != nil {
			modelsUsed = []string{a.Client.Name()}
		}
		fmt.Fprintln(out)

		// Spinner label: when routing escalated, name the full model and note it is working.
		spLabel := a.spinnerLabel()
		if len(modelsUsed) > 1 {
			spLabel = a.Client.Name() + " · working on it"
			if a.ActiveSkill != "" {
				spLabel += " · " + a.ActiveSkill
			}
		}

		// Build a cancellable context; Ctrl+C cancels the LLM call.
		chatCtx, cancelChat := context.WithCancel(context.Background())
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		wasCancelled := false
		watchDone := make(chan struct{})
		go func() {
			defer signal.Stop(sigCh)
			select {
			case <-sigCh:
				wasCancelled = true
				cancelChat()
			case <-watchDone:
			}
		}()

		var buf strings.Builder
		sp := newSpinner(out, a.estimateDuration(), spLabel)
		var stats ChatStats
		var chatErr error
		if a.Tools != nil && a.Config.ToolsEnabled {
			ex := NewToolExecutor(a.Tools, a.Client, a.Config)
			var updatedHistory []Message
			updatedHistory, stats, chatErr = ex.RunToolLoop(chatCtx, a.History, &buf)
			if chatErr == nil {
				// Preserve any intermediate tool-call/result messages added by the loop.
				a.History = updatedHistory
			}
		} else {
			stats, chatErr = a.Client.Chat(chatCtx, a.History, &buf)
		}
		sp.stop()
		close(watchDone) // stop the signal-watcher goroutine
		cancelChat()     // release context resources (idempotent)

		if wasCancelled || errors.Is(chatErr, context.Canceled) {
			fmt.Fprintln(out, dim("  Cancelled."))
			a.History = a.History[:len(a.History)-1]
			continue
		}
		if chatErr != nil {
			fmt.Fprintf(out, red("Error: ")+"%v\n", chatErr)
			a.History = a.History[:len(a.History)-1]
			continue
		}
		fmt.Fprint(out, buf.String())
		fmt.Fprintln(out)
		fmt.Fprintln(out, dim(formatStatLine(modelsUsed, stats, a.effectiveContextLimit())))
		a.recordStats(stats)
		a.AddMessage("assistant", buf.String())
		if a.Recorder != nil {
			if recErr := a.Recorder.RecordTurnWithStats(input, buf.String(), stats, modelsUsed, ""); recErr != nil {
				fmt.Fprintf(out, yellow("  ✗")+" Recording error: %v\n", recErr)
			}
		}
		a.autoExecuteReply(buf.String(), out, reader, chatCtx)
	}

	fmt.Fprintln(out, dim("Goodbye."))
	return nil
}

// formatStatLine produces the permanent post-response status line.
// models lists the model name(s) that handled the turn (e.g. ["fast", "full"]
// when routing escalated, or a single name otherwise).
// contextLimit is the model's context window in tokens (0 = unknown).
func formatStatLine(models []string, stats ChatStats, contextLimit int) string {
	modelPart := strings.Join(models, " → ")
	elapsed := stats.Elapsed.Round(time.Millisecond)

	if stats.ReplyTokens == 0 {
		if modelPart == "" {
			return ""
		}
		return "  " + modelPart + " · " + elapsed.String()
	}

	var ctxPart string
	if contextLimit > 0 && stats.PromptTokens > 0 {
		pct := stats.PromptTokens * 100 / contextLimit
		ctxPart = fmt.Sprintf("%d/%d ctx (%d%%)", stats.PromptTokens, contextLimit, pct)
	} else {
		ctxPart = fmt.Sprintf("%d ctx", stats.PromptTokens)
	}

	core := fmt.Sprintf("%d reply + %s · %s · %.1f tok/s",
		stats.ReplyTokens, ctxPart, elapsed, stats.TokensPerSec)
	if modelPart == "" {
		return "  " + core
	}
	return "  " + modelPart + " · " + core
}

// initWorkspace resolves and announces the workspace root. It is a fatal error
// if the workspace cannot be created.
func (a *Agent) initWorkspace(out io.Writer) error {
	ws, err := NewWorkspace(a.Config.WorkDir)
	if err != nil {
		return fmt.Errorf("workspace init: %w", err)
	}
	a.Workspace = ws
	fmt.Fprintf(out, green("✓")+" Workspace: %s\n", ws.Root)
	return nil
}

// initKnowledgeBase opens (or creates) the SQLite knowledge base. Failures are
// non-fatal: the user is warned but Harvey continues without a KB.
func (a *Agent) initKnowledgeBase(out io.Writer) {
	kb, err := OpenKnowledgeBase(a.Workspace, a.Config.KnowledgeDB)
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" Knowledge base unavailable: %v\n", err)
		return
	}
	a.KB = kb
	fmt.Fprintf(out, green("✓")+" Knowledge base: %s\n", kb.Path())
}

// initModelCache opens (or creates) the SQLite model capability cache. Failures
// are non-fatal: the user is warned but Harvey continues without a cache.
func (a *Agent) initModelCache(out io.Writer) {
	mc, err := OpenModelCache(a.Workspace, a.Config.ModelCacheDB)
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" Model cache unavailable: %v\n", err)
		return
	}
	a.ModelCache = mc
	fmt.Fprintf(out, green("✓")+" Model cache: %s\n", mc.Path())
}

// initRag opens the active RAG store when one is configured in harvey.yaml.
// Failures are non-fatal. RagOn is set to match cfg.RagEnabled.
func (a *Agent) initRag(out io.Writer) {
	entry := a.Config.ActiveRagStore()
	if entry == nil {
		return
	}
	dbPath, err := a.Workspace.AbsPath(entry.DBPath)
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" RAG store unavailable: %v\n", err)
		return
	}
	store, err := NewRagStore(dbPath, entry.EmbeddingModel)
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" RAG store unavailable: %v\n", err)
		return
	}
	a.Rag = store
	a.RagOn = a.Config.RagEnabled
	status := "off"
	if a.RagOn {
		status = "on"
	}
	fmt.Fprintf(out, green("✓")+" RAG store: %s (%s) [%s]\n", entry.Name, entry.DBPath, status)
}

/** selectBackend runs the interactive startup sequence to choose a backend.
 * preferredModel hints at which Ollama model to pre-select (from a prior session);
 * pass an empty string when no preference is known.
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
		if err := StartOllamaService(); err != nil {
			fmt.Fprintf(out, red("  Failed: ")+"%v\n", err)
		} else {
			fmt.Fprintln(out, green("  ✓")+" Ollama started")
			return a.pickOllamaModel(reader, out, preferredModel)
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, dim("  No backend selected. Use /ollama start once inside."))
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
	if a.Config.OllamaModel != "" {
		a.Client = newOllamaLLMClient(a.Config.OllamaURL, a.Config.OllamaModel, a.Config.OllamaTimeout)
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
		a.Config.OllamaModel = models[0]
		a.Client = newOllamaLLMClient(a.Config.OllamaURL, models[0], a.Config.OllamaTimeout)
		fmt.Fprintf(out, "  Using model: %s\n", cyan(models[0]))
		return nil
	}

	// Try to match the preferred model against the available list.
	if preferredModel != "" {
		for _, m := range models {
			if strings.EqualFold(extractModelName(m), preferredModel) ||
				strings.EqualFold(m, preferredModel) {
				a.Config.OllamaModel = m
				a.Client = newOllamaLLMClient(a.Config.OllamaURL, m, a.Config.OllamaTimeout)
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

	a.Config.OllamaModel = chosen
	a.Client = newOllamaLLMClient(a.Config.OllamaURL, chosen, a.Config.OllamaTimeout)
	fmt.Fprintf(out, "  Using model: %s\n", cyan(chosen))
	return nil
}

// pickSession scans sessDir for .spmd and .fountain files and, if any exist,
// asks the user whether to resume one. Returns the chosen file path and the
// ALL-CAPS model name extracted from it; both are empty if no session is chosen.
func (a *Agent) pickSession(reader *bufio.Reader, out io.Writer, sessDir string) (path, model string) {
	files, err := ListSessionFiles(sessDir)
	if err != nil || len(files) == 0 {
		return "", ""
	}
	if !askYesNo(reader, out, "  Resume a prior session? [y/N] ", false) {
		return "", ""
	}
	fmt.Fprintln(out)
	limit := len(files)
	if limit > 20 {
		limit = 20
	}
	for i, f := range files[:limit] {
		fmt.Fprintf(out, "    [%d] %-44s  %s\n", i+1,
			f.Name, f.ModTime.Format("2006-01-02 15:04"))
	}
	fmt.Fprintf(out, "    Select session [1-%d, 0=none]: ", limit)
	line, _ := reader.ReadString('\n')
	idx := 0
	fmt.Sscanf(strings.TrimSpace(line), "%d", &idx)
	if idx < 1 || idx > limit {
		return "", ""
	}
	chosen := files[idx-1]
	m, _ := ExtractModelFromSession(chosen.Path)
	return chosen.Path, m
}

// loadSkills scans the standard skill directories, stores the catalog on the
// agent, and appends the XML catalog block to the system prompt so the model
// knows what skills are available. It also updates Config.SystemPrompt so
// that /clear re-injects the catalog after resetting history. Non-fatal:
// if no skills are found the function returns silently.
func (a *Agent) loadSkills(out io.Writer) {
	cat := ScanSkills(a.Workspace.Root, a.Config.AgentsDir)
	if len(cat) == 0 {
		return
	}
	a.Skills = cat
	block := CatalogSystemPromptBlock(cat)

	// Persist in Config so ClearHistory() keeps the catalog across /clear.
	if a.Config.SystemPrompt != "" {
		a.Config.SystemPrompt += "\n\n" + block
	} else {
		a.Config.SystemPrompt = block
	}

	// Update the system message already in History (added before this call).
	injected := false
	for i, m := range a.History {
		if m.Role == "system" {
			a.History[i].Content += "\n\n" + block
			injected = true
			break
		}
	}
	if !injected {
		a.History = append([]Message{{Role: "system", Content: block}}, a.History...)
	}

	proj, user := 0, 0
	for _, s := range cat {
		if s.Source == SkillSourceProject {
			proj++
		} else {
			user++
		}
	}
	detail := ""
	switch {
	case proj > 0 && user > 0:
		detail = fmt.Sprintf(" (%d project, %d user)", proj, user)
	case proj > 0:
		detail = " (project)"
	default:
		detail = " (user)"
	}
	fmt.Fprintf(out, green("✓")+" Skills: %d skill(s) available%s\n", len(cat), detail)
	a.registerSkillCommands()
}

/** askYesNo prints prompt, reads a line, and returns true for "y"/"yes".
 * defaultYes controls what an empty (Enter) response means.
 *
 * Parameters:
 *   reader     (*bufio.Reader) — source for the user's answer.
 *   out        (io.Writer)     — destination for the prompt string.
 *   prompt     (string)        — text to print before reading.
 *   defaultYes (bool)          — return value when the user presses Enter.
 *
 * Returns:
 *   bool — true if the user answered yes (or pressed Enter with defaultYes=true).
 *
 * Example:
 *   if askYesNo(out, reader, "Continue? [Y/n] ", true) {
 *       // proceed
 *   }
 */
func askYesNo(reader *bufio.Reader, out io.Writer, prompt string, defaultYes bool) bool {
	fmt.Fprint(out, prompt)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "" {
		return defaultYes
	}
	return answer == "y" || answer == "yes"
}

// ragAugment prepends relevant RAG chunks to prompt when RAG is enabled.
// Returns the original prompt unchanged when RAG is off, unconfigured, or
// when no chunks are retrieved. Errors are silently swallowed so a RAG
// failure never blocks the chat turn.
// ragMinScore is the minimum cosine similarity a chunk must have to be injected
// as context. Chunks scoring below this threshold are discarded so that irrelevant
// results don't waste the limited context window of small models.
const ragMinScore = 0.3

func (a *Agent) ragAugment(prompt string) string {
	if !a.RagOn || a.Rag == nil {
		return prompt
	}
	entry := a.Config.ActiveRagStore()
	if entry == nil || entry.EmbeddingModel == "" {
		return prompt
	}

	// Resolve embedding model for the current generation model.
	embedModel := entry.EmbeddingModel
	if entry.ModelMap != nil {
		if mapped, ok := entry.ModelMap[a.Config.OllamaModel]; ok && mapped != "" {
			embedModel = mapped
		}
	}

	embedder := NewOllamaEmbedder(a.Config.OllamaURL, embedModel)
	chunks, err := a.Rag.Query(prompt, embedder, 5)
	if err != nil || len(chunks) == 0 {
		return prompt
	}

	// Discard chunks below the relevance threshold; they confuse small models
	// and waste context tokens without adding useful information.
	var relevant []Chunk
	for _, c := range chunks {
		if c.Score >= ragMinScore {
			relevant = append(relevant, c)
		}
	}
	if len(relevant) == 0 {
		return prompt
	}

	var sb strings.Builder
	sb.WriteString("### Context (from knowledge base)\n\n")
	for i, c := range relevant {
		if c.Source != "" {
			fmt.Fprintf(&sb, "[%d] (source: %s)\n%s\n\n", i+1, c.Source, c.Content)
		} else {
			fmt.Fprintf(&sb, "[%d] %s\n\n", i+1, c.Content)
		}
	}
	sb.WriteString("---\n\n")
	sb.WriteString(prompt)
	return sb.String()
}
