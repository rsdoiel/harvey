package harvey

import (
	"io"
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
