package harvey

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// ANSI escape codes for terminal styling.
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiRed    = "\033[31m"
	sep        = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
)

func bold(s string) string   { return ansiBold + s + ansiReset }
func dim(s string) string    { return ansiDim + s + ansiReset }
func green(s string) string  { return ansiGreen + s + ansiReset }
func yellow(s string) string { return ansiYellow + s + ansiReset }
func red(s string) string    { return ansiRed + s + ansiReset }
func cyan(s string) string   { return ansiCyan + s + ansiReset }

// prompt returns the input prompt string reflecting the current backend state.
func (a *Agent) prompt() string {
	if a.Client == nil {
		return "harvey (no backend) > "
	}
	return "harvey > "
}

// Run prints the startup banner, runs the backend selection sequence,
// then starts the interactive REPL. It reads from os.Stdin.
func (a *Agent) Run(out io.Writer) error {
	a.registerCommands()
	reader := bufio.NewReader(os.Stdin)

	// Banner
	fmt.Fprintln(out, cyan(bold(sep)))
	fmt.Fprintf(out, "  %s  %s\n", bold("Harvey"), dim(Version))
	fmt.Fprintln(out, cyan(bold(sep)))

	// System prompt
	if a.Config.SystemPrompt != "" {
		fmt.Fprintln(out, green("✓")+" Loaded HARVEY.md as system prompt")
		a.AddMessage("system", a.Config.SystemPrompt)
	} else {
		fmt.Fprintln(out, dim("  No HARVEY.md found in current directory"))
	}

	// Backend selection
	if err := a.selectBackend(reader, out); err != nil {
		return err
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

		var buf strings.Builder
		ctx := context.Background()
		if chatErr := a.Client.Chat(ctx, a.History, io.MultiWriter(out, &buf)); chatErr != nil {
			fmt.Fprintf(out, "\n"+red("Error: ")+"%v\n", chatErr)
			// Remove the failed user message so history stays consistent.
			a.History = a.History[:len(a.History)-1]
			continue
		}
		fmt.Fprintln(out, "\n")
		a.AddMessage("assistant", buf.String())
	}

	fmt.Fprintln(out, dim("Goodbye."))
	return nil
}

// selectBackend runs the interactive startup sequence to choose a backend.
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

// pickOllamaModel selects a model from the running Ollama server. If only one
// model is installed it is selected automatically; otherwise the user chooses.
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

// askYesNo prints prompt, reads a line, and returns true for "y"/"yes".
// defaultYes controls what an empty (Enter) response means.
func askYesNo(reader *bufio.Reader, out io.Writer, prompt string, defaultYes bool) bool {
	fmt.Fprint(out, prompt)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "" {
		return defaultYes
	}
	return answer == "y" || answer == "yes"
}
