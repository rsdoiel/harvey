package harvey

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Command describes a slash command and its handler.
type Command struct {
	Usage       string
	Description string
	// Handler is nil for commands handled directly in the REPL (exit, quit).
	Handler func(a *Agent, args []string, out io.Writer) error
}

// registerCommands wires the built-in slash commands onto the agent.
func (a *Agent) registerCommands() {
	a.commands = map[string]*Command{
		"help": {
			Usage:       "/help",
			Description: "List available slash commands",
			Handler:     cmdHelp,
		},
		"status": {
			Usage:       "/status",
			Description: "Show current connection and session status",
			Handler:     cmdStatus,
		},
		"clear": {
			Usage:       "/clear",
			Description: "Clear conversation history",
			Handler:     cmdClear,
		},
		"ollama": {
			Usage:       "/ollama <start|stop|status|list|use MODEL>",
			Description: "Control the local Ollama service",
			Handler:     cmdOllama,
		},
		"publicai": {
			Usage:       "/publicai <connect|disconnect|status>",
			Description: "Manage the publicai.co connection",
			Handler:     cmdPublicAI,
		},
		"exit": {
			Usage:       "/exit",
			Description: "Exit Harvey",
			Handler:     nil,
		},
		"quit": {
			Usage:       "/quit",
			Description: "Exit Harvey",
			Handler:     nil,
		},
	}
}

// dispatch parses a slash command and runs it. Returns (shouldExit, error).
func (a *Agent) dispatch(input string, out io.Writer) (bool, error) {
	parts := strings.Fields(strings.TrimPrefix(input, "/"))
	if len(parts) == 0 {
		return false, nil
	}
	name := strings.ToLower(parts[0])
	args := parts[1:]

	if name == "exit" || name == "quit" {
		return true, nil
	}
	cmd, ok := a.commands[name]
	if !ok {
		fmt.Fprintf(out, "Unknown command: /%s  (type /help for a list)\n", name)
		return false, nil
	}
	if cmd.Handler != nil {
		return false, cmd.Handler(a, args, out)
	}
	return false, nil
}

func cmdHelp(a *Agent, _ []string, out io.Writer) error {
	fmt.Fprintln(out)
	for _, cmd := range a.commands {
		fmt.Fprintf(out, "  %-45s %s\n", cmd.Usage, cmd.Description)
	}
	fmt.Fprintln(out)
	return nil
}

func cmdStatus(a *Agent, _ []string, out io.Writer) error {
	if a.Client == nil {
		fmt.Fprintln(out, "Backend:  none")
	} else {
		fmt.Fprintf(out, "Backend:  %s\n", a.Client.Name())
	}
	fmt.Fprintf(out, "History:  %d messages\n", len(a.History))
	return nil
}

func cmdClear(a *Agent, _ []string, out io.Writer) error {
	a.ClearHistory()
	fmt.Fprintln(out, "Conversation history cleared.")
	return nil
}

func cmdOllama(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /ollama <start|stop|status|list|use MODEL>")
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "start":
		fmt.Fprintln(out, "Starting Ollama...")
		if err := StartOllamaService(); err != nil {
			return err
		}
		fmt.Fprintln(out, "Ollama is running.")
	case "stop":
		fmt.Fprintln(out, "Use your system's service manager to stop Ollama (e.g. systemctl stop ollama).")
	case "status":
		if ProbeOllama(a.Config.OllamaURL) {
			fmt.Fprintln(out, "Ollama is running.")
		} else {
			fmt.Fprintln(out, "Ollama is not running.")
		}
	case "list":
		if !ProbeOllama(a.Config.OllamaURL) {
			fmt.Fprintln(out, "Ollama is not running.")
			return nil
		}
		models, err := NewOllamaClient(a.Config.OllamaURL, "").Models(context.Background())
		if err != nil {
			return err
		}
		if len(models) == 0 {
			fmt.Fprintln(out, "No models installed. Run: ollama pull <model>")
			return nil
		}
		for _, m := range models {
			marker := "  "
			if oc, ok := a.Client.(*OllamaClient); ok && oc.Model() == m {
				marker = "* "
			}
			fmt.Fprintf(out, "%s%s\n", marker, m)
		}
	case "use":
		if len(args) < 2 {
			fmt.Fprintln(out, "Usage: /ollama use MODEL")
			return nil
		}
		model := args[1]
		if !ProbeOllama(a.Config.OllamaURL) {
			fmt.Fprintln(out, "Ollama is not running. Use /ollama start first.")
			return nil
		}
		a.Config.OllamaModel = model
		a.Client = NewOllamaClient(a.Config.OllamaURL, model)
		fmt.Fprintf(out, "Now using Ollama model: %s\n", model)
	default:
		fmt.Fprintf(out, "Unknown ollama subcommand: %s\n", args[0])
	}
	return nil
}

func cmdPublicAI(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /publicai <connect|disconnect|status>")
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "connect":
		if a.Config.PublicAIKey == "" {
			fmt.Fprintln(out, "No API key found. Set the PUBLICAI_API_KEY environment variable.")
			return nil
		}
		a.Client = NewPublicAIClient(a.Config.PublicAIURL, a.Config.PublicAIKey, a.Config.PublicAIModel)
		fmt.Fprintf(out, "Connected to publicai.co (%s).\n", a.Config.PublicAIModel)
	case "disconnect":
		if _, ok := a.Client.(*PublicAIClient); ok {
			a.Client = nil
			fmt.Fprintln(out, "Disconnected from publicai.co.")
		} else {
			fmt.Fprintln(out, "Not currently connected to publicai.co.")
		}
	case "status":
		if _, ok := a.Client.(*PublicAIClient); ok {
			fmt.Fprintf(out, "Connected to publicai.co (%s).\n", a.Config.PublicAIModel)
		} else {
			fmt.Fprintln(out, "Not connected to publicai.co.")
		}
	default:
		fmt.Fprintf(out, "Unknown publicai subcommand: %s\n", args[0])
	}
	return nil
}
