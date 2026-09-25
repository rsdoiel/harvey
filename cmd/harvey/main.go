package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	harvey "github.com/rsdoiel/harvey"
)

// setDebugEnv sets environment variables enabling debug output for both
// Harvey and Ollama. Called once at startup when --debug is passed.
func setDebugEnv() {
	os.Setenv("OLLAMA_DEBUG", "1")
	os.Setenv("HARVEY_DEBUG", "1")
}

// checkWorkDir enforces the workspace-boundary security invariant when
// -w/--workdir was explicitly passed: the process's cwd must lie inside the
// requested root. It returns an error otherwise. A no-op when explicit is
// false, since the default (cwd-as-workspace) case always trivially satisfies
// containment.
func checkWorkDir(workDir string, explicit bool) error {
	if !explicit {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return harvey.RequireCWDInRoot(cwd, workDir)
}

func main() {
	os.Exit(mainRun(os.Args, os.Stdout, os.Stderr))
}

// exitStatus is the process exit status for an error mainRun is returning. A
// usage error is 2; every other error is still 1 until the classified sites of
// exit-codes-plan.md H3 replace this with harvey.ExitCodeFor.
func exitStatus(err error) int {
	if harvey.ExitCodeFor(err) == harvey.ClassUsage {
		return harvey.ClassUsage.Code
	}
	return 1
}

// mainRun is the whole of harvey's command line, with the process's streams
// and arguments passed in so it can be tested. args[0] is the program name.
// It returns the exit status; main only calls os.Exit with it.
func mainRun(args []string, out, errOut io.Writer) int {
	err := runArgs(args, out, errOut)
	if err == nil {
		return 0
	}
	if harvey.ExitCodeFor(err) == harvey.ClassUsage {
		fmt.Fprintln(errOut, err)
	} else {
		fmt.Fprintf(errOut, "Error: %v\n", err)
	}
	return exitStatus(err)
}

func runArgs(args []string, out, errOut io.Writer) error {
	appName := filepath.Base(args[0])
	version, releaseDate, releaseHash := harvey.Version, harvey.ReleaseDate, harvey.ReleaseHash
	licenseText, fmtHelp, helpText := harvey.LicenseText, harvey.FmtHelp, harvey.HelpText

	cfg := harvey.DefaultConfig()
	workDirExplicit := false

	for i := 1; i < len(args); i++ {
		arg := args[i]
		// next returns the value of a flag that takes one.
		next := func() (string, error) {
			i++
			if i >= len(args) {
				return "", harvey.Usagef("%s requires an argument", arg)
			}
			return args[i], nil
		}
		// value sets *dst from the flag's argument.
		value := func(dst *string) error {
			v, err := next()
			if err != nil {
				return err
			}
			*dst = v
			return nil
		}
		switch arg {
		case "init":
			// harvey init <source> — seed model aliases from another workspace or YAML file
			if i+1 >= len(args) {
				return harvey.Usagef("Usage: harvey init <workspace-path|aliases.yaml>")
			}
			i++
			source := args[i]
			if err := checkWorkDir(cfg.WorkDir, workDirExplicit); err != nil {
				return err
			}
			ws, wsErr := harvey.NewWorkspace(cfg.WorkDir)
			if wsErr != nil {
				return wsErr
			}
			if err := harvey.LoadHarveyYAML(ws, cfg); err != nil {
				return fmt.Errorf("loading workspace config: %w", err)
			}
			if _, _, err := harvey.ImportAliasesFrom(source, ws, cfg, out); err != nil {
				return err
			}
			return nil
		case "help":
			// harvey help [TOPIC]
			var topic string
			if i+1 < len(args) && len(args[i+1]) > 0 && args[i+1][0] != '-' {
				i++
				topic = args[i]
			}
			if topic == "" {
				fmt.Fprint(out, fmtHelp(helpText, appName, version, releaseDate, releaseHash))
			} else if topic == "topics" || topic == "index" {
				fmt.Fprint(out, harvey.HelpTopicsText())
			} else if !harvey.PrintHelpTopic(out, topic, appName, version, releaseDate, releaseHash) {
				return harvey.Usagef("Unknown help topic %q.\nType '%s help topics' for the topic index.", topic, appName)
			}
			return nil
		case "-h", "-help", "--help":
			// Optional topic: harvey --help skills
			if i+1 < len(args) && len(args[i+1]) > 0 && args[i+1][0] != '-' {
				i++
				topic := args[i]
				if topic == "topics" || topic == "index" {
					fmt.Fprint(out, harvey.HelpTopicsText())
				} else if !harvey.PrintHelpTopic(out, topic, appName, version, releaseDate, releaseHash) {
					return harvey.Usagef("Unknown help topic %q.\nType '%s --help topics' for the topic index.", topic, appName)
				}
			} else {
				fmt.Fprint(out, fmtHelp(helpText, appName, version, releaseDate, releaseHash))
			}
			return nil
		case "-v", "--version":
			fmt.Fprintf(out, "%s %s (released %s, %s)\n", appName, version, releaseDate, releaseHash)
			return nil
		case "-l", "--license":
			fmt.Fprint(out, licenseText)
			return nil
		case "-m", "--model":
			if err := value(&cfg.Ollama.Model); err != nil {
				return err
			}
		case "--ollama":
			if err := value(&cfg.Ollama.URL); err != nil {
				return err
			}
		case "--llamafile":
			// Session-only: create a synthetic registry entry without persisting.
			p, err := next()
			if err != nil {
				return err
			}
			cfg.Llamafile.Models = append(cfg.Llamafile.Models, harvey.LlamafileEntry{
				Name: harvey.LlamafileModelNameFromPath(p),
				Path: p,
			})
			cfg.Llamafile.Active = harvey.LlamafileModelNameFromPath(p)
		case "--llamafile-url":
			if err := value(&cfg.Llamafile.URL); err != nil {
				return err
			}
		case "--llamafile-dir":
			if err := value(&cfg.Llamafile.ModelsDir); err != nil {
				return err
			}
		case "-w", "--workdir":
			if err := value(&cfg.WorkDir); err != nil {
				return err
			}
			workDirExplicit = true
		case "-r", "--record":
			cfg.Session.AutoRecord = true
		case "--record-file":
			if err := value(&cfg.Session.RecordPath); err != nil {
				return err
			}
			cfg.Session.AutoRecord = true
		case "--resume":
			cfg.Session.ResumeLatest = true
		case "--continue":
			if err := value(&cfg.Session.ContinuePath); err != nil {
				return err
			}
		case "--replay":
			if err := value(&cfg.Session.ReplayPath); err != nil {
				return err
			}
		case "--replay-output":
			if err := value(&cfg.Session.ReplayOutputPath); err != nil {
				return err
			}
		case "--replay-continue":
			cfg.Session.ReplayContinue = true
		case "--debug":
			cfg.Debug = true
			setDebugEnv()
		default:
			if strings.HasPrefix(arg, "-") {
				return harvey.Usagef("Unknown flag: %s", arg)
			}
			return harvey.Usagef("unexpected argument: %s", arg)
		}
	}

	// HARVEY_LLAMAFILE_DIR env var overrides the YAML default but is itself
	// overridden by the --llamafile-dir flag (already applied above).
	if v := os.Getenv("HARVEY_LLAMAFILE_DIR"); v != "" && cfg.Llamafile.ModelsDir == harvey.DefaultLlamafileModelsDir() {
		cfg.Llamafile.ModelsDir = v
	}

	if err := checkWorkDir(cfg.WorkDir, workDirExplicit); err != nil {
		return err
	}
	ws, err := harvey.NewWorkspace(cfg.WorkDir)
	if err != nil {
		return err
	}
	cfg.SystemPrompt = ws.LoadHarveyMD()
	if cfg.Session.ResumeLatest && cfg.Session.ContinuePath == "" {
		sessDir := filepath.Join(ws.HarveyDir(), "sessions")
		if p := harvey.MostRecentSession(sessDir); p != "" {
			cfg.Session.ContinuePath = p
		} else {
			fmt.Fprintln(errOut, "  No sessions found in agents/sessions/ — starting fresh.")
		}
	}
	agent := harvey.NewAgent(cfg, ws)
	return agent.Run(out)
}
