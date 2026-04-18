package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	harvey "github.com/rsdoiel/harvey"
)

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
				case "skills", "skill":
					fmt.Print(harvey.SkillsHelpText)
				default:
					fmt.Fprintf(os.Stderr, "Unknown help topic %q. Available topics: skills\n", os.Args[i])
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
		case "-s", "--session":
			id, err := strconv.ParseInt(next(), 10, 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "--session requires a numeric ID\n")
				os.Exit(1)
			}
			cfg.ResumeSessionID = id
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
