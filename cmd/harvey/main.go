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
		case "-h", "-help", "--help":
			// Optional topic: harvey --help skills
			if i+1 < len(os.Args) && len(os.Args[i+1]) > 0 && os.Args[i+1][0] != '-' {
				i++
				switch os.Args[i] {
				case "clear":
					fmt.Print(fmtHelp(harvey.ClearHelpText, appName, version, releaseDate, releaseHash))
				case "compact", "summarize":
					fmt.Print(fmtHelp(harvey.SummarizeHelpText, appName, version, releaseDate, releaseHash))
				case "context":
					fmt.Print(fmtHelp(harvey.ContextHelpText, appName, version, releaseDate, releaseHash))
				case "editing", "edit", "keybindings", "keys":
					fmt.Print(fmtHelp(harvey.EditingHelpText, appName, version, releaseDate, releaseHash))
				case "files":
					fmt.Print(fmtHelp(harvey.FilesHelpText, appName, version, releaseDate, releaseHash))
				case "file-tree", "filetree":
					fmt.Print(fmtHelp(harvey.FileTreeHelpText, appName, version, releaseDate, releaseHash))
				case "git":
					fmt.Print(fmtHelp(harvey.GitHelpText, appName, version, releaseDate, releaseHash))
				case "inspect":
					fmt.Print(fmtHelp(harvey.InspectHelpText, appName, version, releaseDate, releaseHash))
				case "kb", "knowledge", "knowledge-base":
					fmt.Print(fmtHelp(harvey.KBHelpText, appName, version, releaseDate, releaseHash))
				case "model":
					fmt.Print(fmtHelp(harvey.ModelHelpText, appName, version, releaseDate, releaseHash))
				case "model-alias", "alias":
					fmt.Print(fmtHelp(harvey.ModelAliasHelpText, appName, version, releaseDate, releaseHash))
				case "ollama":
					fmt.Print(fmtHelp(harvey.OllamaHelpText, appName, version, releaseDate, releaseHash))
				case "rag":
					fmt.Print(fmtHelp(harvey.RagHelpText, appName, version, releaseDate, releaseHash))
				case "read":
					fmt.Print(fmtHelp(harvey.ReadHelpText, appName, version, releaseDate, releaseHash))
				case "read-dir", "readdir":
					fmt.Print(fmtHelp(harvey.ReadDirHelpText, appName, version, releaseDate, releaseHash))
				case "record", "recording":
					fmt.Print(fmtHelp(harvey.RecordHelpText, appName, version, releaseDate, releaseHash))
				case "rename":
					fmt.Print(fmtHelp(harvey.RenameHelpText, appName, version, releaseDate, releaseHash))
				case "routing", "route":
					fmt.Print(fmtHelp(harvey.RoutingHelpText, appName, version, releaseDate, releaseHash))
				case "run":
					fmt.Print(fmtHelp(harvey.RunHelpText, appName, version, releaseDate, releaseHash))
				case "search":
					fmt.Print(fmtHelp(harvey.SearchHelpText, appName, version, releaseDate, releaseHash))
				case "security", "safemode", "safe-mode", "permissions", "audit":
					fmt.Print(fmtHelp(harvey.SecurityHelpText, appName, version, releaseDate, releaseHash))
				case "session", "sessions":
					fmt.Print(fmtHelp(harvey.SessionHelpText, appName, version, releaseDate, releaseHash))
				case "skill-set", "skillset":
					fmt.Print(fmtHelp(harvey.SkillSetHelpText, appName, version, releaseDate, releaseHash))
				case "skills", "skill":
					fmt.Print(fmtHelp(harvey.SkillsHelpText, appName, version, releaseDate, releaseHash))
				case "status":
					fmt.Print(fmtHelp(harvey.StatusHelpText, appName, version, releaseDate, releaseHash))
				case "write":
					fmt.Print(fmtHelp(harvey.WriteHelpText, appName, version, releaseDate, releaseHash))
				default:
					fmt.Fprintf(os.Stderr, "Unknown help topic %q.\nAvailable topics: alias, audit, clear, compact, context, editing, file-tree, files, git, inspect, kb, model, model-alias, ollama, permissions, rag, read, read-dir, record, rename, routing, run, safemode, search, security, session, skill-set, skills, status, summarize, write\n", os.Args[i])
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
			cfg.OllamaModel = next()
		case "--ollama":
			cfg.OllamaURL = next()
		case "-w", "--workdir":
			cfg.WorkDir = next()
		case "-r", "--record":
			cfg.AutoRecord = true
		case "--record-file":
			cfg.AutoRecord = true
			cfg.RecordPath = next()
		case "--continue":
			cfg.ContinuePath = next()
		case "--replay":
			cfg.ReplayPath = next()
		case "--replay-output":
			cfg.ReplayOutputPath = next()
		case "--debug":
			cfg.Debug = true
			setDebugEnv()
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
