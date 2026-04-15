package main

import (
	"fmt"
	"os"

	harvey "github.com/rsdoiel/harvey"
)

const helpText = `{app_name} - a terminal agent for local large language models

USAGE:
  {app_name} [OPTIONS]

OPTIONS:
  -h, --help          display this help message
  -v, --version       display version information
  -l, --license       display license information
  -m, --model MODEL   Ollama model to use on startup
      --ollama URL    Ollama base URL (default: http://localhost:11434)
  -w, --workdir DIR   workspace directory (default: current directory)

ENVIRONMENT:
  PUBLICAI_API_KEY    API key for publicai.co

DESCRIPTION:
  {app_name} looks for HARVEY.md in the current directory and uses it as a
  system prompt. It then connects to a local Ollama server or publicai.co
  and starts an interactive chat session.

  All file I/O is constrained to the workspace directory (--workdir or ".").
  A knowledge base is stored at <workdir>/.harvey/knowledge.db and is created
  automatically on first run.

  Type /help inside the session for available slash commands.
`

func main() {
	cfg := harvey.DefaultConfig()

	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		next := func() string {
			i++
			if i >= len(os.Args) {
				fmt.Fprintf(os.Stderr, "%s requires an argument\n", arg)
				os.Exit(1)
			}
			return os.Args[i]
		}
		switch arg {
		case "-h", "--help":
			fmt.Print(harvey.FmtHelp(helpText, "harvey", harvey.Version, harvey.ReleaseDate, harvey.ReleaseHash))
			os.Exit(0)
		case "-v", "--version":
			fmt.Printf("%s %s (released %s, %s)\n", "harvey", harvey.Version, harvey.ReleaseDate, harvey.ReleaseHash)
			os.Exit(0)
		case "-l", "--license":
			fmt.Print(harvey.LicenseText)
			os.Exit(0)
		case "-m", "--model":
			cfg.OllamaModel = next()
		case "--ollama":
			cfg.OllamaURL = next()
		case "-w", "--workdir":
			cfg.WorkDir = next()
		default:
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", arg)
			os.Exit(1)
		}
	}

	cfg.SystemPrompt = harvey.LoadHarveyMD()
	ws, err := harvey.NewWorkspace(cfg.WorkDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	agent := harvey.NewAgent(cfg, ws)
	if err := agent.Run(os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
