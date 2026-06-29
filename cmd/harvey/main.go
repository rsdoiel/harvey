package main

import (
	"fmt"
	"os"
	"path/filepath"

	harvey "github.com/rsdoiel/harvey"
)

// setDebugEnv sets environment variables enabling debug output for both
// Harvey and Ollama. Called once at startup when --debug is passed.
func setDebugEnv() {
	os.Setenv("OLLAMA_DEBUG", "1")
	os.Setenv("HARVEY_DEBUG", "1")
}

func main() {
	appName := filepath.Base(os.Args[0])
	version, releaseDate, releaseHash := harvey.Version, harvey.ReleaseDate, harvey.ReleaseHash
	licenseText, fmtHelp, helpText := harvey.LicenseText, harvey.FmtHelp, harvey.HelpText


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
		case "init":
			// harvey init <source> — seed model aliases from another workspace or YAML file
			if i+1 >= len(os.Args) {
				fmt.Fprintln(os.Stderr, "Usage: harvey init <workspace-path|aliases.yaml>")
				os.Exit(1)
			}
			i++
			source := os.Args[i]
			ws, wsErr := harvey.NewWorkspace(cfg.WorkDir)
			if wsErr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", wsErr)
				os.Exit(1)
			}
			if err := harvey.LoadHarveyYAML(ws, cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error loading workspace config: %v\n", err)
				os.Exit(1)
			}
			if _, _, err := harvey.ImportAliasesFrom(source, ws, cfg, os.Stdout); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0)
		case "help":
			// harvey help [TOPIC]
			var topic string
			if i+1 < len(os.Args) && len(os.Args[i+1]) > 0 && os.Args[i+1][0] != '-' {
				i++
				topic = os.Args[i]
			}
			if topic == "" {
				fmt.Print(fmtHelp(helpText, appName, version, releaseDate, releaseHash))
			} else if topic == "topics" || topic == "index" {
				fmt.Print(harvey.HelpTopicsText())
			} else if !harvey.PrintHelpTopic(os.Stdout, topic, appName, version, releaseDate, releaseHash) {
				fmt.Fprintf(os.Stderr, "Unknown help topic %q.\nType '%s help topics' for the topic index.\n", topic, appName)
				os.Exit(1)
			}
			os.Exit(0)
		case "-h", "-help", "--help":
			// Optional topic: harvey --help skills
			if i+1 < len(os.Args) && len(os.Args[i+1]) > 0 && os.Args[i+1][0] != '-' {
				i++
				topic := os.Args[i]
				if topic == "topics" || topic == "index" {
					fmt.Print(harvey.HelpTopicsText())
				} else if !harvey.PrintHelpTopic(os.Stdout, topic, appName, version, releaseDate, releaseHash) {
					fmt.Fprintf(os.Stderr, "Unknown help topic %q.\nType '%s --help topics' for the topic index.\n", topic, appName)
					os.Exit(1)
				}
			} else {
				fmt.Print(fmtHelp(helpText, appName, version, releaseDate, releaseHash))
			}
			os.Exit(0)
		case "-v", "--version":
			fmt.Printf("%s %s (released %s, %s)\n", appName, version, releaseDate, releaseHash)
			os.Exit(0)
		case "-l", "--license":
			fmt.Print(licenseText)
			os.Exit(0)
		case "-m", "--model":
			cfg.Ollama.Model = next()
		case "--ollama":
			cfg.Ollama.URL = next()
		case "--llamafile":
			// Session-only: create a synthetic registry entry without persisting.
			p := next()
			cfg.Llamafile.Models = append(cfg.Llamafile.Models, harvey.LlamafileEntry{
				Name: harvey.LlamafileModelNameFromPath(p),
				Path: p,
			})
			cfg.Llamafile.Active = harvey.LlamafileModelNameFromPath(p)
		case "--llamafile-url":
			cfg.Llamafile.URL = next()
		case "--llamafile-dir":
			cfg.Llamafile.ModelsDir = next()
		case "-w", "--workdir":
			cfg.WorkDir = next()
		case "-r", "--record":
			cfg.Session.AutoRecord = true
		case "--record-file":
			cfg.Session.AutoRecord = true
			cfg.Session.RecordPath = next()
		case "--resume":
			cfg.Session.ResumeLatest = true
		case "--continue":
			cfg.Session.ContinuePath = next()
		case "--replay":
			cfg.Session.ReplayPath = next()
		case "--replay-output":
			cfg.Session.ReplayOutputPath = next()
		case "--replay-continue":
			cfg.Session.ReplayContinue = true
		case "--debug":
			cfg.Debug = true
			setDebugEnv()
		default:
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", arg)
			os.Exit(1)
		}
	}

	// HARVEY_LLAMAFILE_DIR env var overrides the YAML default but is itself
	// overridden by the --llamafile-dir flag (already applied above).
	if v := os.Getenv("HARVEY_LLAMAFILE_DIR"); v != "" && cfg.Llamafile.ModelsDir == harvey.DefaultLlamafileModelsDir() {
		cfg.Llamafile.ModelsDir = v
	}

	cfg.SystemPrompt = harvey.LoadHarveyMD()
	ws, err := harvey.NewWorkspace(cfg.WorkDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if cfg.Session.ResumeLatest && cfg.Session.ContinuePath == "" {
		sessDir := filepath.Join(ws.HarveyDir(), "sessions")
		if p := harvey.MostRecentSession(sessDir); p != "" {
			cfg.Session.ContinuePath = p
		} else {
			fmt.Fprintln(os.Stderr, "  No sessions found in agents/sessions/ — starting fresh.")
		}
	}
	agent := harvey.NewAgent(cfg, ws)
	if err := agent.Run(os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
