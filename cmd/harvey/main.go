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
	// A directory that is not there is a missing input (66); the containment
	// check below is a bad -w value (2).
	fi, err := os.Stat(workDir)
	if err != nil {
		return harvey.NoInputf("workspace directory %s does not exist: %w", workDir, err)
	}
	if !fi.IsDir() {
		return harvey.NoInputf("workspace %s is not a directory", workDir)
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

// startState is what the command line builds up before a session starts.
type startState struct {
	cfg             *harvey.Config
	workDirExplicit bool   // -w/--workdir was given, so the cwd must lie inside it
	llamafile       string // the path given to --llamafile, checked after parsing
}

// flagSpec is one option of harvey. The table below is the single list of
// them: runArgs looks each argument up in it, and the enforcement test compares
// it with the OPTIONS section of the manual, so a flag cannot be added to one
// and not the other.
type flagSpec struct {
	names      []string // every spelling, for example "-m" and "--model"
	takesValue bool     // the next argument is its value
	apply      func(st *startState, v string) error
}

var flagSpecs = []flagSpec{
	{[]string{"-m", "--model"}, true, func(st *startState, v string) error { st.cfg.Ollama.Model = v; return nil }},
	{[]string{"--ollama"}, true, func(st *startState, v string) error { st.cfg.Ollama.URL = v; return nil }},
	{[]string{"--llamafile"}, true, func(st *startState, v string) error {
		// Session-only: create a synthetic registry entry without persisting.
		// The file itself is checked once the whole command line has parsed, so
		// a usage mistake later in it is reported as one.
		st.llamafile = v
		st.cfg.Llamafile.Models = append(st.cfg.Llamafile.Models, harvey.LlamafileEntry{
			Name: harvey.LlamafileModelNameFromPath(v),
			Path: v,
		})
		st.cfg.Llamafile.Active = harvey.LlamafileModelNameFromPath(v)
		return nil
	}},
	{[]string{"--llamafile-url"}, true, func(st *startState, v string) error { st.cfg.Llamafile.URL = v; return nil }},
	{[]string{"--llamafile-dir"}, true, func(st *startState, v string) error { st.cfg.Llamafile.ModelsDir = v; return nil }},
	{[]string{"-w", "--workdir"}, true, func(st *startState, v string) error {
		st.cfg.WorkDir = v
		st.workDirExplicit = true
		return nil
	}},
	{[]string{"-r", "--record"}, false, func(st *startState, _ string) error { st.cfg.Session.AutoRecord = true; return nil }},
	{[]string{"--record-file"}, true, func(st *startState, v string) error {
		st.cfg.Session.RecordPath = v
		st.cfg.Session.AutoRecord = true
		return nil
	}},
	{[]string{"--resume"}, false, func(st *startState, _ string) error { st.cfg.Session.ResumeLatest = true; return nil }},
	{[]string{"--continue"}, true, func(st *startState, v string) error { st.cfg.Session.ContinuePath = v; return nil }},
	{[]string{"--replay"}, true, func(st *startState, v string) error { st.cfg.Session.ReplayPath = v; return nil }},
	{[]string{"--replay-output"}, true, func(st *startState, v string) error { st.cfg.Session.ReplayOutputPath = v; return nil }},
	{[]string{"--replay-continue"}, false, func(st *startState, _ string) error { st.cfg.Session.ReplayContinue = true; return nil }},
	{[]string{"--debug"}, false, func(st *startState, _ string) error {
		st.cfg.Debug = true
		setDebugEnv()
		return nil
	}},
}

// findFlag returns the table entry that has arg as one of its spellings.
func findFlag(arg string) *flagSpec {
	for i := range flagSpecs {
		for _, n := range flagSpecs[i].names {
			if n == arg {
				return &flagSpecs[i]
			}
		}
	}
	return nil
}

// exitStatus is the process exit status for an error mainRun is returning: the
// code of its class, and 70 for an error nothing classified.
func exitStatus(err error) int {
	return harvey.ExitCodeFor(err).Code
}

// mainRun is the whole of harvey's command line, with the process's streams
// and arguments passed in so it can be tested. args[0] is the program name.
// It returns the exit status; main only calls os.Exit with it. --json is
// recognised here, wherever it sits on the line, so it still applies when a
// later flag is the one that fails; runArgs never sees the token.
func mainRun(args []string, out, errOut io.Writer) int {
	jsonOut, args := harvey.ExtractJSONFlag(args)
	err := runArgs(args, out, errOut)
	if err == nil {
		return 0
	}
	if jsonOut {
		return harvey.PrintJSONError(errOut, err)
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

	st := &startState{cfg: harvey.DefaultConfig()}
	cfg := st.cfg

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
		switch arg {
		case "init":
			// harvey init <source> — seed model aliases from another workspace or YAML file
			if i+1 >= len(args) {
				return harvey.Usagef("Usage: harvey init <workspace-path|aliases.yaml>")
			}
			i++
			source := args[i]
			if err := checkWorkDir(cfg.WorkDir, st.workDirExplicit); err != nil {
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
		default:
			spec := findFlag(arg)
			if spec == nil {
				if strings.HasPrefix(arg, "-") {
					return harvey.Usagef("Unknown flag: %s", arg)
				}
				return harvey.Usagef("unexpected argument: %s", arg)
			}
			v := ""
			if spec.takesValue {
				var err error
				if v, err = next(); err != nil {
					return err
				}
			}
			if err := spec.apply(st, v); err != nil {
				return err
			}
		}
	}

	// HARVEY_LLAMAFILE_DIR env var overrides the YAML default but is itself
	// overridden by the --llamafile-dir flag (already applied above).
	if v := os.Getenv("HARVEY_LLAMAFILE_DIR"); v != "" && cfg.Llamafile.ModelsDir == harvey.DefaultLlamafileModelsDir() {
		cfg.Llamafile.ModelsDir = v
	}

	if st.llamafile != "" {
		if err := harvey.CheckLlamafileInput(st.llamafile); err != nil {
			return err
		}
	}
	if err := checkWorkDir(cfg.WorkDir, st.workDirExplicit); err != nil {
		return err
	}
	ws, err := harvey.NewWorkspace(cfg.WorkDir)
	if err != nil {
		return err
	}
	if err := harvey.CheckStartupInputs(cfg); err != nil {
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
