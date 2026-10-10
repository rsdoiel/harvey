package main

import (
	"os"
	"path/filepath"
	"testing"

	harvey "github.com/rsdoiel/harvey"
)

// The command line beats agents/harvey.yaml, whichever setting it is. Run
// loads the file after the command line has been applied, so a setting the
// file also names used to overwrite the flag (--ollama and -m were fixed first,
// cli_override_test.go). Each case here applies a real flag, then loads a
// harvey.yaml that disagrees with it, in the order the program does.

type precedenceCase struct {
	flag  string
	value string
	yaml  string
	check func(c *harvey.Config) (got, want string)
}

func loadWithYAML(t *testing.T, cfg *harvey.Config, yaml string) {
	t.Helper()
	ws, err := harvey.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ws.HarveyDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.HarveyDir(), "harvey.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := harvey.LoadHarveyYAML(ws, cfg); err != nil {
		t.Fatalf("LoadHarveyYAML: %v", err)
	}
}

func b2s(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

var precedenceCases = []precedenceCase{
	{"--llamafile-url", "http://flag:1", "llamafile:\n  url: http://yaml:2\n",
		func(c *harvey.Config) (string, string) { return c.Llamafile.URL, "http://flag:1" }},
	{"--llamafile-dir", "/flag/models", "llamafile:\n  models_dir: /yaml/models\n",
		func(c *harvey.Config) (string, string) { return c.Llamafile.ModelsDir, "/flag/models" }},
	{"-r", "", "session:\n  auto_record: false\n",
		func(c *harvey.Config) (string, string) { return b2s(c.Session.AutoRecord), "true" }},
	{"--record-file", "/flag/rec.spmd", "session:\n  record_path: /yaml/rec.spmd\n",
		func(c *harvey.Config) (string, string) { return c.Session.RecordPath, "/flag/rec.spmd" }},
	{"--resume", "", "session:\n  resume_latest: false\n",
		func(c *harvey.Config) (string, string) { return b2s(c.Session.ResumeLatest), "true" }},
	{"--continue", "/flag/s.spmd", "session:\n  continue_path: /yaml/s.spmd\n",
		func(c *harvey.Config) (string, string) { return c.Session.ContinuePath, "/flag/s.spmd" }},
	{"--replay", "/flag/r.spmd", "session:\n  replay_path: /yaml/r.spmd\n",
		func(c *harvey.Config) (string, string) { return c.Session.ReplayPath, "/flag/r.spmd" }},
	{"--replay-output", "/flag/out.spmd", "session:\n  replay_output_path: /yaml/out.spmd\n",
		func(c *harvey.Config) (string, string) { return c.Session.ReplayOutputPath, "/flag/out.spmd" }},
	{"--replay-continue", "", "session:\n  replay_continue: false\n",
		func(c *harvey.Config) (string, string) { return b2s(c.Session.ReplayContinue), "true" }},
}

func TestFlags_CommandLineBeatsHarveyYAML(t *testing.T) {
	for _, tc := range precedenceCases {
		t.Run(tc.flag, func(t *testing.T) {
			st := &startState{cfg: harvey.DefaultConfig()}
			spec := findFlag(tc.flag)
			if spec == nil {
				t.Fatalf("no such flag %s", tc.flag)
			}
			if err := spec.apply(st, tc.value); err != nil {
				t.Fatal(err)
			}
			loadWithYAML(t, st.cfg, tc.yaml)
			if got, want := tc.check(st.cfg); got != want {
				t.Errorf("%s: after loading harvey.yaml the setting is %q, want the flag's %q", tc.flag, got, want)
			}
		})
	}
}

// Without the flag the file still sets them.
func TestFlags_HarveyYAMLStillAppliesWithoutTheFlag(t *testing.T) {
	cfg := harvey.DefaultConfig()
	loadWithYAML(t, cfg, "llamafile:\n  url: http://yaml:2\n  models_dir: /yaml/models\nsession:\n  record_path: /yaml/rec.spmd\n")
	if cfg.Llamafile.URL != "http://yaml:2" || cfg.Llamafile.ModelsDir != "/yaml/models" || cfg.Session.RecordPath != "/yaml/rec.spmd" {
		t.Errorf("yaml values not applied: %+v %+v", cfg.Llamafile, cfg.Session)
	}
}

// A flag that sets nothing the file can set is listed here, so a new flag has
// to be classified: either it is a precedenceCase, or it is named below with
// the reason it cannot collide.
var flagsWithoutYAMLCounterpart = map[string]string{
	"-m": "covered by TestFlags_OllamaAndModelAreMarkedExplicit", "--model": "same as -m",
	"--ollama":    "covered by TestFlags_OllamaAndModelAreMarkedExplicit",
	"--llamafile": "adds a session-only registry entry; harvey.yaml models are appended after it and llamafile.active never auto-selects",
	"-w":          "the file is found through the workspace, so there is no yaml counterpart", "--workdir": "same as -w",
	"--record": "same as -r", "--debug": "there is no debug key in harvey.yaml",
}

func TestFlags_EveryFlagIsCheckedAgainstHarveyYAML(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range precedenceCases {
		covered[tc.flag] = true
	}
	for _, f := range flagSpecs {
		ok := false
		for _, n := range f.names {
			if covered[n] || flagsWithoutYAMLCounterpart[n] != "" {
				ok = true
			}
		}
		if !ok {
			t.Errorf("flag %v is neither a precedence case nor listed as having no harvey.yaml counterpart", f.names)
		}
	}
}

// HARVEY_LLAMAFILE_DIR beats llamafile.models_dir (CONFIGURATION.md) and is
// itself beaten by --llamafile-dir.
func TestEnv_LlamafileDirBeatsYAMLAndLosesToTheFlag(t *testing.T) {
	t.Run("env beats yaml", func(t *testing.T) {
		t.Setenv("HARVEY_LLAMAFILE_DIR", "/env/models")
		cfg := harvey.DefaultConfig()
		applyEnvironment(cfg)
		loadWithYAML(t, cfg, "llamafile:\n  models_dir: /yaml/models\n")
		if cfg.Llamafile.ModelsDir != "/env/models" {
			t.Errorf("ModelsDir = %q, want /env/models", cfg.Llamafile.ModelsDir)
		}
	})
	t.Run("flag beats env", func(t *testing.T) {
		t.Setenv("HARVEY_LLAMAFILE_DIR", "/env/models")
		st := &startState{cfg: harvey.DefaultConfig()}
		if err := findFlag("--llamafile-dir").apply(st, "/flag/models"); err != nil {
			t.Fatal(err)
		}
		applyEnvironment(st.cfg)
		loadWithYAML(t, st.cfg, "llamafile:\n  models_dir: /yaml/models\n")
		if st.cfg.Llamafile.ModelsDir != "/flag/models" {
			t.Errorf("ModelsDir = %q, want /flag/models", st.cfg.Llamafile.ModelsDir)
		}
	})
	t.Run("flag equal to the default still beats env", func(t *testing.T) {
		t.Setenv("HARVEY_LLAMAFILE_DIR", "/env/models")
		st := &startState{cfg: harvey.DefaultConfig()}
		def := harvey.DefaultLlamafileModelsDir()
		if err := findFlag("--llamafile-dir").apply(st, def); err != nil {
			t.Fatal(err)
		}
		applyEnvironment(st.cfg)
		if st.cfg.Llamafile.ModelsDir != def {
			t.Errorf("ModelsDir = %q, want the flag's %q", st.cfg.Llamafile.ModelsDir, def)
		}
	})
}
