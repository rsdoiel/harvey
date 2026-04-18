package harvey

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
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
	defer func() {
		if a.Recorder != nil {
			path := a.Recorder.Path()
			a.Recorder.Close()
			a.Recorder = nil
			fmt.Fprintf(out, dim("  Session saved to %s\n"), path)
		}
	}()
	reader := bufio.NewReader(os.Stdin)

	// Banner
	fmt.Fprintln(out, cyan(bold(sep)))
	fmt.Fprintf(out, "  %s  %s\n", bold("Harvey"), dim(Version))
	fmt.Fprintln(out, cyan(bold(sep)))

	// Workspace
	if err := a.initWorkspace(out); err != nil {
		return err
	}

	// Knowledge base
	a.initKnowledgeBase(out)

	// Session manager (non-fatal)
	a.initSessionManager(out)
	if a.SM != nil {
		defer a.SM.Close()
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

	// Backend selection
	if err := a.selectBackend(reader, out); err != nil {
		return err
	}

	// Session resume or create (after backend is known so we can store the model name)
	if a.SM != nil {
		a.initSession(reader, out)
	}

	// Auto-start recording if configured.
	if a.Config.AutoRecord {
		recPath := a.Config.RecordPath
		if recPath == "" {
			recPath = DefaultSessionPath(a.Workspace.Root)
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

	// Ready line
	fmt.Fprintln(out, cyan(bold(sep)))
	if a.Client != nil {
		fmt.Fprintf(out, "  Connected: %s\n", green(a.Client.Name()))
	} else {
		fmt.Fprintf(out, "  %s\n", yellow("No backend — use /ollama start or /publicai connect"))
	}
	fmt.Fprintln(out, dim("  /help for commands · /exit to quit"))
	fmt.Fprintln(out, cyan(bold(sep)))
	fmt.Fprintln(out)

	// REPL
	for {
		fmt.Fprint(out, a.prompt())
		line, err := reader.ReadString('\n')
		if err != nil {
			// EOF (Ctrl+D) — clean exit
			fmt.Fprintln(out, "\n"+dim("Goodbye."))
			return nil
		}
		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/") {
			shouldExit, cmdErr := a.dispatch(input, out)
			if cmdErr != nil {
				fmt.Fprintf(out, red("Error: ")+"%v\n", cmdErr)
			}
			if shouldExit {
				break
			}
			continue
		}

		// Chat
		if a.Client == nil {
			fmt.Fprintln(out, yellow("No backend connected.")+" Use /ollama start or /publicai connect.")
			continue
		}

		a.AddMessage("user", input)
		fmt.Fprintln(out)

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
		sp := newSpinner(out, a.estimateDuration())
		stats, chatErr := a.Client.Chat(chatCtx, a.History, &buf)
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
		fmt.Fprintln(out, dim("  "+stats.Format()))
		a.recordStats(stats)
		a.AddMessage("assistant", buf.String())
		if a.SM != nil && a.SessionID != 0 {
			model := ""
			if a.Client != nil {
				model = a.Client.Name()
			}
			if saveErr := a.SM.Save(a.SessionID, model, a.History); saveErr != nil {
				fmt.Fprintf(out, yellow("  ✗")+" Session save error: %v\n", saveErr)
			}
		}
		if a.Recorder != nil {
			if recErr := a.Recorder.RecordTurnWithStats(input, buf.String(), stats); recErr != nil {
				fmt.Fprintf(out, yellow("  ✗")+" Recording error: %v\n", recErr)
			}
		}
		a.autoExecuteReply(buf.String(), out, reader, chatCtx)
	}

	fmt.Fprintln(out, dim("Goodbye."))
	return nil
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
	kb, err := OpenKnowledgeBase(a.Workspace)
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" Knowledge base unavailable: %v\n", err)
		return
	}
	a.KB = kb
	fmt.Fprintln(out, green("✓")+" Knowledge base: .harvey/knowledge.db")
}

/** selectBackend runs the interactive startup sequence to choose a backend.
 *
 * Parameters:
 *   reader (*bufio.Reader) — reads user input.
 *   out    (io.Writer)     — destination for prompt and status messages.
 *
 * Returns:
 *   error — on unexpected read failures.
 *
 * Example:
 *   err := agent.selectBackend(reader, os.Stdout)
 */
func (a *Agent) selectBackend(reader *bufio.Reader, out io.Writer) error {
	fmt.Fprintf(out, "\n  Checking Ollama at %s...\n", a.Config.OllamaURL)

	if ProbeOllama(a.Config.OllamaURL) {
		fmt.Fprintln(out, green("  ✓")+" Ollama is running")
		return a.pickOllamaModel(reader, out)
	}

	fmt.Fprintln(out, yellow("  ✗")+" Ollama is not running")

	if askYesNo(reader, out, "    Start Ollama now? [Y/n] ", true) {
		fmt.Fprintln(out, "  Starting Ollama...")
		if err := StartOllamaService(); err != nil {
			fmt.Fprintf(out, red("  Failed: ")+"%v\n", err)
		} else {
			fmt.Fprintln(out, green("  ✓")+" Ollama started")
			return a.pickOllamaModel(reader, out)
		}
	}

	fmt.Fprintln(out)
	if askYesNo(reader, out, "    Use publicai.co instead? [Y/n] ", true) {
		if a.Config.PublicAIKey == "" {
			fmt.Fprintln(out, yellow("  ✗")+" PUBLICAI_API_KEY is not set.")
			fmt.Fprintln(out, dim("  Set the environment variable and restart, or use /publicai connect later."))
		} else {
			a.Client = NewPublicAIClient(a.Config.PublicAIURL, a.Config.PublicAIKey, a.Config.PublicAIModel)
			fmt.Fprintf(out, green("  ✓")+" Connected to publicai.co (%s)\n", a.Config.PublicAIModel)
		}
		return nil
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, dim("  No backend selected. Use /ollama start or /publicai connect once inside."))
	return nil
}

/** pickOllamaModel selects a model from the running Ollama server. If only one
 * model is installed it is selected automatically; otherwise the user chooses.
 *
 * Parameters:
 *   reader (*bufio.Reader) — reads the user's model selection.
 *   out    (io.Writer)     — destination for the model list prompt.
 *
 * Returns:
 *   error — on unexpected failures listing models.
 *
 * Example:
 *   err := agent.pickOllamaModel(reader, os.Stdout)
 */
func (a *Agent) pickOllamaModel(reader *bufio.Reader, out io.Writer) error {
	models, err := NewOllamaClient(a.Config.OllamaURL, "").Models(context.Background())
	if err != nil || len(models) == 0 {
		fmt.Fprintln(out, yellow("  ✗")+" No models installed. Run: ollama pull <model>")
		return nil
	}

	chosen := models[0]
	if len(models) > 1 {
		fmt.Fprintln(out, "  Available models:")
		for i, m := range models {
			fmt.Fprintf(out, "    [%d] %s\n", i+1, m)
		}
		fmt.Fprintf(out, "    Select model [1-%d, default=1]: ", len(models))
		line, _ := reader.ReadString('\n')
		idx := 0
		fmt.Sscanf(strings.TrimSpace(line), "%d", &idx)
		if idx >= 1 && idx <= len(models) {
			chosen = models[idx-1]
		}
	}

	a.Config.OllamaModel = chosen
	a.Client = NewOllamaClient(a.Config.OllamaURL, chosen)
	fmt.Fprintf(out, "  Using model: %s\n", cyan(chosen))
	return nil
}

// initSessionManager opens (or creates) the session database. Failures are
// non-fatal: the user is warned but Harvey continues without session persistence.
func (a *Agent) initSessionManager(out io.Writer) {
	sm, err := OpenSessionManager(a.Workspace)
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" Session manager unavailable: %v\n", err)
		return
	}
	a.SM = sm
	fmt.Fprintln(out, green("✓")+" Sessions: .harvey/sessions.db")
}

// initSession either resumes an existing session (prompted or via --session ID)
// or creates a new one. It is called after the backend is selected so the model
// name can be recorded in the new session row.
func (a *Agent) initSession(reader *bufio.Reader, out io.Writer) {
	// Capture the current system prompt that was just added to history.
	currentSysPrompt := ""
	for _, m := range a.History {
		if m.Role == "system" {
			currentSysPrompt = m.Content
			break
		}
	}

	var session *Session
	if a.Config.ResumeSessionID > 0 {
		s, err := a.SM.Load(a.Config.ResumeSessionID)
		if err != nil {
			fmt.Fprintf(out, yellow("  ✗")+" Could not load session %d: %v\n", a.Config.ResumeSessionID, err)
		} else if s == nil {
			fmt.Fprintf(out, yellow("  ✗")+" Session %d not found, starting new.\n", a.Config.ResumeSessionID)
		} else {
			session = s
		}
	} else {
		last, err := a.SM.LoadLast()
		if err != nil {
			fmt.Fprintf(out, yellow("  ✗")+" Could not check last session: %v\n", err)
		} else if last != nil {
			label := last.Name
			if label == "" {
				label = fmt.Sprintf("#%d", last.ID)
			}
			turns := 0
			for _, m := range last.History {
				if m.Role == "user" {
					turns++
				}
			}
			prompt := fmt.Sprintf("  Resume session %s (%d turns, %s)? [Y/n] ",
				label, turns, last.LastActive.Format("2006-01-02 15:04"))
			if askYesNo(reader, out, prompt, true) {
				session = last
			}
		}
	}

	if session != nil {
		// Restore history, keeping the freshly expanded system prompt active.
		a.History = session.History
		if currentSysPrompt != "" {
			replaced := false
			for i, m := range a.History {
				if m.Role == "system" {
					a.History[i].Content = currentSysPrompt
					replaced = true
					break
				}
			}
			if !replaced {
				a.History = append([]Message{{Role: "system", Content: currentSysPrompt}}, a.History...)
			}
		}
		a.SessionID = session.ID
		label := session.Name
		if label == "" {
			label = fmt.Sprintf("#%d", session.ID)
		}
		fmt.Fprintf(out, green("✓")+" Resumed session %s (%d messages)\n", label, len(a.History))
		return
	}

	// Create a fresh session.
	model := ""
	if a.Client != nil {
		model = a.Client.Name()
	}
	id, err := a.SM.Create(a.Workspace.Root, model, a.History)
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" Could not create session: %v\n", err)
		return
	}
	a.SessionID = id
	fmt.Fprintf(out, green("✓")+" New session #%d\n", id)
}

// loadSkills scans the standard skill directories, stores the catalog on the
// agent, and appends the XML catalog block to the system prompt so the model
// knows what skills are available. It also updates Config.SystemPrompt so
// that /clear re-injects the catalog after resetting history. Non-fatal:
// if no skills are found the function returns silently.
func (a *Agent) loadSkills(out io.Writer) {
	cat := ScanSkills(a.Workspace.Root)
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
