package harvey

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reclassifying the command handlers' errors (the follow-up named in
// harvey/decisions/0006-*.md): a failure a slash command used to print and
// swallow now comes back as a classed error, so a non-interactive session
// exits with its class. Phase 1 is the dispatcher itself.

// An unknown command name is a usage error: the command line is wrong and
// nothing was attempted.
func TestDispatch_UnknownCommandIsUsageError(t *testing.T) {
	a, _ := runFixture(t, false)
	a.registerCommands()
	var out strings.Builder
	exit, err := a.dispatch("/nosuchcommand", &out)
	if exit {
		t.Errorf("dispatch asked to exit on an unknown command")
	}
	if err == nil {
		t.Fatalf("dispatch(/nosuchcommand) = nil, want a usage error")
	}
	if got := ExitCodeFor(err); got != ClassUsage {
		t.Errorf("class = %v (%v), want usage", got, err)
	}
	for _, want := range []string{"/nosuchcommand", "/help"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// Known commands, and the exit words, are not affected.
func TestDispatch_KnownCommandsStillSucceed(t *testing.T) {
	a, _ := runFixture(t, false)
	a.registerCommands()
	if _, err := a.dispatch("/help", io.Discard); err != nil {
		t.Errorf("dispatch(/help) = %v, want nil", err)
	}
	if exit, err := a.dispatch("/exit", io.Discard); !exit || err != nil {
		t.Errorf("dispatch(/exit) = (%v, %v), want (true, nil)", exit, err)
	}
}

// A scripted session that names an unknown command exits usage (2).
func TestRun_NonInteractive_UnknownCommandExitsUsage(t *testing.T) {
	a := scriptedFixture(t, false, "/nosuchcommand\n")
	err := a.Run(io.Discard)
	if got := ExitCodeFor(err); got != ClassUsage {
		t.Fatalf("Run() = %v (%v), want usage", got, err)
	}
}

// At a terminal a typo is something the person just tries again: the session
// still ends cleanly, and the message is shown.
func TestRun_Interactive_UnknownCommandStillExitsZero(t *testing.T) {
	a := scriptedFixture(t, true, "/nosuchcommand\n")
	var out strings.Builder
	if err := a.Run(&out); err != nil {
		t.Fatalf("Run() at a terminal = %v, want nil", err)
	}
	if !strings.Contains(out.String(), "/nosuchcommand") {
		t.Errorf("output does not name the unknown command:\n%s", out.String())
	}
}

// /loop stops on the first failing iteration, so a mistyped command inside a
// loop is reported once instead of running the whole interval count.
func TestRunLoopIteration_UnknownCommandReturnsUsageError(t *testing.T) {
	a, _ := runFixture(t, false)
	a.registerCommands()
	_, err := runLoopIteration(t.Context(), a, "/nosuchcommand", io.Discard)
	if got := ExitCodeFor(err); got != ClassUsage {
		t.Fatalf("runLoopIteration = %v (%v), want usage", got, err)
	}
}

// ─── Phase 2: file and workspace commands ────────────────────────────────────

// handlerCase runs one handler against a prepared agent and checks the class
// of the error it returns.
type handlerCase struct {
	name  string
	setup func(t *testing.T, a *Agent)
	run   func(a *Agent, out io.Writer) error
	want  ExitClass
}

func runHandlerCases(t *testing.T, cases []handlerCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAgent(t)
			if tc.setup != nil {
				tc.setup(t, a)
			}
			err := tc.run(a, io.Discard)
			if tc.want == ClassOK {
				if err != nil {
					t.Fatalf("error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("error = nil, want class %s", tc.want.Name)
			}
			if got := ExitCodeFor(err); got != tc.want {
				t.Fatalf("class = %s (%v), want %s", got.Name, err, tc.want.Name)
			}
		})
	}
}

func noWorkspace(t *testing.T, a *Agent) { a.Workspace = nil }

func denyRead(t *testing.T, a *Agent) {
	a.Config.Security.Permissions = map[string][]string{".": {"write"}}
}

func denyWrite(t *testing.T, a *Agent) {
	a.Config.Security.Permissions = map[string][]string{".": {"read"}}
}

func writeFixture(t *testing.T, a *Agent, rel, content string) {
	t.Helper()
	if err := a.Workspace.WriteFile(rel, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHandlerErrors_FilesAndTree(t *testing.T) {
	runHandlerCases(t, []handlerCase{
		{"files no workspace", noWorkspace, func(a *Agent, o io.Writer) error { return cmdFiles(a, nil, o) }, ClassNoInput},
		{"files missing dir", nil, func(a *Agent, o io.Writer) error { return cmdFiles(a, []string{"nope"}, o) }, ClassNoInput},
		{"files outside workspace", nil, func(a *Agent, o io.Writer) error { return cmdFiles(a, []string{"../.."}, o) }, ClassNoPermission},
		{"files ok", nil, func(a *Agent, o io.Writer) error { return cmdFiles(a, nil, o) }, ClassOK},
		{"file-tree outside workspace", nil, func(a *Agent, o io.Writer) error { return cmdFileTree(a, []string{"../.."}, o) }, ClassNoPermission},
		{"file-tree missing dir", nil, func(a *Agent, o io.Writer) error { return cmdFileTree(a, []string{"nope"}, o) }, ClassNoInput},
	})
}

func TestHandlerErrors_Read(t *testing.T) {
	read := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdRead(a, args, o) }
	}
	runHandlerCases(t, []handlerCase{
		{"no workspace", noWorkspace, read("x"), ClassNoInput},
		{"no args", nil, read(), ClassUsage},
		{"missing file", nil, read("nope.txt"), ClassNoInput},
		{"outside workspace", nil, read("../../etc/hostname"), ClassNoPermission},
		{"read denied", denyRead, read("a.txt"), ClassNoPermission},
		{"ok", func(t *testing.T, a *Agent) { writeFixture(t, a, "a.txt", "hi") }, read("a.txt"), ClassOK},
		{"one of two missing keeps the failure", func(t *testing.T, a *Agent) { writeFixture(t, a, "a.txt", "hi") }, read("a.txt", "nope.txt"), ClassNoInput},
	})
}

// A bulk /read still injects the files it could read, then reports the first
// failure: process everything, then exit with the class of the first failure.
func TestHandlerErrors_ReadKeepsTheGoodFilesWhenOneFails(t *testing.T) {
	a := newTestAgent(t)
	writeFixture(t, a, "a.txt", "hello")
	err := cmdRead(a, []string{"nope.txt", "a.txt"}, io.Discard)
	if got := ExitCodeFor(err); got != ClassNoInput {
		t.Fatalf("class = %s (%v), want no_input", got.Name, err)
	}
	found := false
	for _, m := range a.History {
		if strings.Contains(m.Content, "hello") {
			found = true
		}
	}
	if !found {
		t.Errorf("the readable file was not added to context")
	}
}

func TestHandlerErrors_ReadChunks(t *testing.T) {
	rc := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdReadChunks(a, args, o) }
	}
	withClient := func(t *testing.T, a *Agent) { a.Client = &mockLLMClient{} }
	runHandlerCases(t, []handlerCase{
		{"no workspace", noWorkspace, rc("x"), ClassNoInput},
		{"no backend", nil, rc("x"), ClassUnavailable},
		{"no args", withClient, rc(), ClassUsage},
		{"chunk-size without value", withClient, rc("f", "--chunk-size"), ClassUsage},
		{"chunk-size not a number", withClient, rc("f", "--chunk-size", "x"), ClassUsage},
		{"max-chunks without value", withClient, rc("f", "--max-chunks"), ClassUsage},
		{"max-chunks not a number", withClient, rc("f", "--max-chunks", "0"), ClassUsage},
		{"overlap without value", withClient, rc("f", "--overlap"), ClassUsage},
		{"overlap bad mode", withClient, rc("f", "--overlap", "word"), ClassUsage},
		{"flags only, no path", withClient, rc("--chunk-size", "10"), ClassUsage},
		{"no instruction and no history", withClient, rc("f"), ClassUsage},
		{"read denied", func(t *testing.T, a *Agent) { withClient(t, a); denyRead(t, a) }, rc("f", "summarize"), ClassNoPermission},
		{"missing file", withClient, rc("nope.md", "summarize"), ClassNoInput},
	})
}

func TestHandlerErrors_ReadDir(t *testing.T) {
	rd := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdReadDir(a, args, o) }
	}
	runHandlerCases(t, []handlerCase{
		{"no workspace", noWorkspace, rd(), ClassNoInput},
		{"depth without value", nil, rd("--depth"), ClassUsage},
		{"depth not a number", nil, rd("--depth", "x"), ClassUsage},
		{"two paths", nil, rd("a", "b"), ClassUsage},
		{"outside workspace", nil, rd("../.."), ClassNoPermission},
		{"read denied", denyRead, rd("."), ClassNoPermission},
		{"missing dir", nil, rd("nope"), ClassNoInput},
		{"a file, not a directory", func(t *testing.T, a *Agent) { writeFixture(t, a, "a.txt", "x") }, rd("a.txt"), ClassData},
		{"nothing readable", nil, rd("."), ClassNegative},
		{"ok", func(t *testing.T, a *Agent) { writeFixture(t, a, "a.txt", "x") }, rd("."), ClassOK},
	})
}

func TestHandlerErrors_ReadPDF(t *testing.T) {
	pdf := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdReadPDF(a, args, o) }
	}
	noPoppler := func(t *testing.T, a *Agent) { t.Setenv("PATH", t.TempDir()) }
	runHandlerCases(t, []handlerCase{
		{"no args", nil, pdf(), ClassUsage},
		{"poppler missing", noPoppler, pdf("x.pdf"), ClassUnavailable},
	})
	if checkPopplerTools() != nil {
		t.Skip("poppler not installed; skipping the cases that need pdfinfo")
	}
	runHandlerCases(t, []handlerCase{
		{"missing file", nil, pdf("/nonexistent/x.pdf"), ClassNoInput},
		{"bad page range", nil, pdf("/nonexistent/x.pdf", "9-1"), ClassUsage},
		{"not a PDF", func(t *testing.T, a *Agent) { writeFixture(t, a, "n.pdf", "not a pdf") }, func(a *Agent, o io.Writer) error {
			p, _ := a.Workspace.AbsPath("n.pdf")
			return cmdReadPDF(a, []string{p}, o)
		}, ClassData},
	})
}

func TestHandlerErrors_Attach(t *testing.T) {
	at := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdAttach(a, args, o) }
	}
	abs := func(rel string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error {
			p, _ := a.Workspace.AbsPath(rel)
			return cmdAttach(a, []string{p}, o)
		}
	}
	runHandlerCases(t, []handlerCase{
		{"no args", nil, at(), ClassUsage},
		{"missing file", nil, at("/nonexistent/file.txt"), ClassNoInput},
		{"a directory", nil, func(a *Agent, o io.Writer) error { return cmdAttach(a, []string{a.Workspace.Root}, o) }, ClassUsage},
		{"binary file", func(t *testing.T, a *Agent) { writeFixture(t, a, "b.bin", "ab\x00cd") }, abs("b.bin"), ClassData},
		{"text too large", func(t *testing.T, a *Agent) {
			writeFixture(t, a, "big.txt", strings.Repeat("x", attachMaxTextBytes+1))
		}, abs("big.txt"), ClassData},
		{"ok", func(t *testing.T, a *Agent) { writeFixture(t, a, "a.txt", "hi") }, abs("a.txt"), ClassOK},
	})
}

func TestHandlerErrors_Write(t *testing.T) {
	wr := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdWrite(a, args, o) }
	}
	withReply := func(t *testing.T, a *Agent) { a.AddMessage("assistant", "```\nhi\n```") }
	runHandlerCases(t, []handlerCase{
		{"no workspace", noWorkspace, wr("x"), ClassNoInput},
		{"no args", nil, wr(), ClassUsage},
		{"no reply to write", nil, wr("x.txt"), ClassNegative},
		{"write denied", func(t *testing.T, a *Agent) { withReply(t, a); denyWrite(t, a) }, wr("x.txt"), ClassNoPermission},
		{"outside workspace", withReply, wr("../x.txt"), ClassNoPermission},
		{"ok", withReply, wr("x.txt"), ClassOK},
	})
}

func TestHandlerErrors_Run(t *testing.T) {
	run := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdRun(a, args, o) }
	}
	runHandlerCases(t, []handlerCase{
		{"no workspace", noWorkspace, run("true"), ClassNoInput},
		{"no args", nil, run(), ClassUsage},
		{"refused by safe mode", func(t *testing.T, a *Agent) {
			a.Config.Security.SafeMode = true
			a.Config.Security.AllowedCommands = []string{"ls"}
		}, run("true"), ClassNoPermission},
		{"unterminated quote", nil, run("echo", "\"a"), ClassUsage},
		{"program not found", nil, run("no-such-program-xyz"), ClassNoInput},
		// The command's own exit status is output for the model, not a failure of /run.
		{"child exits non-zero", nil, run("false"), ClassOK},
		{"ok", nil, run("true"), ClassOK},
	})
}

func TestHandlerErrors_Search(t *testing.T) {
	se := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdSearch(a, args, o) }
	}
	runHandlerCases(t, []handlerCase{
		{"no workspace", noWorkspace, se("x"), ClassNoInput},
		{"no args", nil, se(), ClassUsage},
		{"bad pattern", nil, se("("), ClassUsage},
		{"outside workspace", nil, se("x", "../.."), ClassNoPermission},
		{"no matches", func(t *testing.T, a *Agent) { writeFixture(t, a, "a.txt", "hello") }, se("zzz"), ClassNegative},
		{"match", func(t *testing.T, a *Agent) { writeFixture(t, a, "a.txt", "hello") }, se("hell"), ClassOK},
	})
}

func TestHandlerErrors_Git(t *testing.T) {
	gi := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdGit(a, args, o) }
	}
	runHandlerCases(t, []handlerCase{
		{"no workspace", noWorkspace, gi("status"), ClassNoInput},
		{"no args", nil, gi(), ClassUsage},
		{"unsupported subcommand", nil, gi("push"), ClassUsage},
		// A temp directory is not a repository, so git itself fails (exit 128).
		{"git fails", func(t *testing.T, a *Agent) { t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(a.Workspace.Root)) }, gi("status"), ClassNegative},
	})
}

func TestHandlerErrors_Format(t *testing.T) {
	fm := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdFormat(a, args, o) }
	}
	runHandlerCases(t, []handlerCase{
		{"no workspace", noWorkspace, fm("x.go"), ClassNoInput},
		{"no args", nil, fm(), ClassUsage},
		{"missing file", nil, fm("nope.go"), ClassNoInput},
		{"no formatter for the extension", func(t *testing.T, a *Agent) { writeFixture(t, a, "a.zzz", "x") }, fm("a.zzz"), ClassNegative},
		{"bad source", func(t *testing.T, a *Agent) { writeFixture(t, a, "a.go", "package (") }, fm("a.go"), ClassData},
		{"already formatted", func(t *testing.T, a *Agent) { writeFixture(t, a, "a.go", "package a\n") }, fm("a.go"), ClassOK},
	})
}

func TestHandlerErrors_Summarize(t *testing.T) {
	su := func(a *Agent, o io.Writer) error { return cmdSummarize(a, nil, o) }
	runHandlerCases(t, []handlerCase{
		{"no backend", nil, su, ClassUnavailable},
		{"nothing to summarize", func(t *testing.T, a *Agent) { a.Client = &mockLLMClient{} }, su, ClassNegative},
	})
}

// End to end: a scripted session that reads a missing file exits no_input (66),
// and the first failure wins over a later one.
func TestRun_NonInteractive_FailedFileCommandDecidesExitClass(t *testing.T) {
	a := scriptedFixture(t, false, "/read nope.txt\n/nosuchcommand\n")
	err := a.Run(io.Discard)
	if got := ExitCodeFor(err); got != ClassNoInput {
		t.Fatalf("Run() = %s (%v), want no_input from the first failing command", got.Name, err)
	}
}

// ─── Phase 3: model, session, context, safe-mode, record, workspace ──────────

// isolateModels keeps a test away from the developer's real model directories
// and from any Ollama server that happens to be running.
func isolateModels(t *testing.T, a *Agent) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HARVEY_LLAMAFILE_DIR", "")
	a.Config.Ollama.URL = "http://127.0.0.1:1"
	// DefaultConfig resolved ~/Models before HOME was redirected.
	a.Config.Llamafile.ModelsDir = t.TempDir()
	a.Config.LlamaCpp.ModelsDir = t.TempDir()
}

func withModelCache(t *testing.T, a *Agent) {
	t.Helper()
	mc, err := OpenModelCache(a.Workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mc.Close() })
	a.ModelCache = mc
}

func TestHandlerErrors_Model(t *testing.T) {
	model := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdModel(a, args, o) }
	}
	runHandlerCases(t, []handlerCase{
		{"use: no such model", isolateModels, model("use", "no-such-model-xyz"), ClassNegative},
		{"list: no models", isolateModels, model("list"), ClassNegative},
		{"stop: no backend active", nil, model("stop"), ClassNegative},
		{"show: no such llamafile", func(t *testing.T, a *Agent) { isolateModels(t, a) }, model("show", "nope"), ClassNegative},
		{"mode: no model cache", nil, model("mode", "structured"), ClassNegative},
		{"mode: no active Ollama model", withModelCache, model("mode"), ClassNegative},
		{"mode: too many arguments", withModelCache, model("mode", "a", "b", "c"), ClassUsage},
		{"mode: unknown mode", withModelCache, model("mode", "m", "bogus"), ClassUsage},
		{"mode: ok", withModelCache, model("mode", "m", "prose"), ClassOK},
		{"alias set: missing arguments", nil, model("alias", "set", "x"), ClassUsage},
		{"alias tags: missing arguments", nil, model("alias", "tags", "x"), ClassUsage},
		{"alias tags: no such alias", nil, model("alias", "tags", "x", "t"), ClassNegative},
		{"alias remove: missing argument", nil, model("alias", "remove"), ClassUsage},
		{"alias remove: no such alias", nil, model("alias", "remove", "nope"), ClassNegative},
		{"alias: unknown subcommand", nil, model("alias", "bogus"), ClassUsage},
		{"alias set: ok", func(t *testing.T, a *Agent) { isolateModels(t, a) }, model("alias", "set", "mine", "some-model"), ClassOK},
	})
}

func TestHandlerErrors_Inspect(t *testing.T) {
	runHandlerCases(t, []handlerCase{
		{"no Ollama backend", nil, func(a *Agent, o io.Writer) error { return cmdInspect(a, nil, o) }, ClassUnavailable},
	})
}

func TestHandlerErrors_SafeMode(t *testing.T) {
	sm := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdSafeMode(a, args, o) }
	}
	runHandlerCases(t, []handlerCase{
		{"no arguments", nil, sm(), ClassUsage},
		{"allow without a command", nil, sm("allow"), ClassUsage},
		{"deny without a command", nil, sm("deny"), ClassUsage},
		{"unknown subcommand", nil, sm("bogus"), ClassUsage},
		{"deny a command not in the list", nil, sm("deny", "no-such-command-xyz"), ClassNegative},
		{"allow", nil, sm("allow", "ls"), ClassOK},
		{"status", nil, sm("status"), ClassOK},
		{"settings cannot be saved", func(t *testing.T, a *Agent) {
			if err := os.MkdirAll(filepath.Join(a.Workspace.Root, "agents"), 0o755); err != nil {
				t.Fatal(err)
			}
			// A directory where harvey.yaml should be makes the save fail.
			if err := os.MkdirAll(filepath.Join(a.Workspace.Root, "agents", "harvey.yaml"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, sm("on"), ClassIO},
	})
}

func TestHandlerErrors_RecordAndRename(t *testing.T) {
	rec := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdRecord(a, args, o) }
	}
	recording := func(t *testing.T, a *Agent) {
		r, err := NewRecorder(filepath.Join(a.Workspace.Root, "s.spmd"), "m", a.Workspace.Root)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { r.Close() })
		a.Recorder = r
	}
	runHandlerCases(t, []handlerCase{
		{"record: no arguments", nil, rec(), ClassUsage},
		{"record: unknown subcommand", nil, rec("bogus"), ClassUsage},
		{"record stop: not recording", nil, rec("stop"), ClassNegative},
		{"record start: already recording", recording, rec("start"), ClassNegative},
		{"record start: cannot create the file", nil, rec("start", "/nonexistent-dir/x/s.spmd"), ClassCantCreate},
		{"record status", nil, rec("status"), ClassOK},
		{"rename: not recording", nil, func(a *Agent, o io.Writer) error { return cmdRename(a, []string{"n"}, o) }, ClassNegative},
		{"rename: no name", recording, func(a *Agent, o io.Writer) error { return cmdRename(a, nil, o) }, ClassUsage},
	})
}

func TestHandlerErrors_Context(t *testing.T) {
	cx := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdContext(a, args, o) }
	}
	runHandlerCases(t, []handlerCase{
		{"add without text", nil, cx("add"), ClassUsage},
		{"unknown subcommand", nil, cx("bogus"), ClassUsage},
		{"show", nil, cx("show"), ClassOK},
		{"add", nil, cx("add", "note"), ClassOK},
	})
}

func TestHandlerErrors_Session(t *testing.T) {
	se := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdSession(a, args, o) }
	}
	withDir := func(t *testing.T, a *Agent) { a.SessionsDir = t.TempDir() }
	runHandlerCases(t, []handlerCase{
		{"no arguments", nil, se(), ClassUsage},
		{"unknown subcommand", nil, se("bogus"), ClassUsage},
		{"list: no sessions directory", nil, se("list"), ClassNoInput},
		{"list: empty directory", withDir, se("list"), ClassOK},
		{"show: no file", nil, se("show"), ClassUsage},
		{"show: missing file", nil, se("show", "/nonexistent/x.spmd"), ClassNoInput},
		{"show: not a session", func(t *testing.T, a *Agent) { writeFixture(t, a, "bad.spmd", "\x00\x01 not fountain") }, func(a *Agent, o io.Writer) error {
			p, _ := a.Workspace.AbsPath("bad.spmd")
			return cmdSession(a, []string{"show", p}, o)
		}, ClassOK}, // a file that parses as an empty session is not an error
		{"use: no sessions directory", nil, se("use"), ClassNoInput},
		{"use: no sessions", withDir, se("use"), ClassNegative},
		{"use: missing file", nil, se("use", "/nonexistent/x.spmd"), ClassNoInput},
		{"continue: missing file", nil, se("continue", "/nonexistent/x.spmd"), ClassNoInput},
		{"replay: no file", nil, se("replay"), ClassUsage},
		{"replay: no backend", nil, se("replay", "x.spmd"), ClassUnavailable},
	})
}

func TestHandlerErrors_Workspace(t *testing.T) {
	ws := func(args ...string) func(*Agent, io.Writer) error {
		return func(a *Agent, o io.Writer) error { return cmdWorkspace(a, args, o) }
	}
	runHandlerCases(t, []handlerCase{
		{"status: no workspace", noWorkspace, ws(), ClassNoInput},
		{"init: no workspace", noWorkspace, ws("init", "init"), ClassNoInput},
		{"unknown subcommand", nil, ws("x", "bogus"), ClassUsage},
		{"status", nil, ws(), ClassOK},
	})
}

func TestHandlerErrors_Help(t *testing.T) {
	runHandlerCases(t, []handlerCase{
		{"unknown topic", nil, func(a *Agent, o io.Writer) error { return cmdHelp(a, []string{"nosuchtopic"}, o) }, ClassUsage},
		{"topics", nil, func(a *Agent, o io.Writer) error { return cmdHelp(a, []string{"topics"}, o) }, ClassOK},
	})
}

// A failed @name model switch is a failure of the turn: a scripted session
// exits with its class instead of continuing as if nothing happened.
func TestRun_NonInteractive_FailedModelSwitchDecidesExitClass(t *testing.T) {
	a := scriptedFixture(t, false, "@broken hello\n")
	a.Config.ModelAliases = map[string]ModelAlias{
		"broken": {Model: "no-such-file.gguf", Engine: "llamacpp"},
	}
	a.Config.LlamaCpp.ModelsDir = t.TempDir()
	err := a.Run(io.Discard)
	got := ExitCodeFor(err)
	if got == ClassOK || got == ClassInternal {
		t.Fatalf("Run() = %s (%v), want a real failure class for a model that cannot be started", got.Name, err)
	}
}

// A model that cannot be found by /model use says no (exit 1), not "internal".
func TestRun_NonInteractive_UnknownModelDecidesExitClass(t *testing.T) {
	a := scriptedFixture(t, false, "/model use no-such-model-xyz\n")
	err := a.Run(io.Discard)
	if got := ExitCodeFor(err); got != ClassNegative {
		t.Fatalf("Run() = %s (%v), want negative (no such model)", got.Name, err)
	}
}
