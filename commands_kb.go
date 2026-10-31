// Package harvey — commands_kb.go implements /kb: it runs the installed kb
// (workspace DR-0034), except for the two verbs only harvey can do, inject and
// learn, which stay harvey's until the design brief's decision 4 is made.
package harvey

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	knowledge "github.com/rsdoiel/knowledge"
)

// kbInstallHint is what an error says when kb is not on PATH.
const kbInstallHint = "go install github.com/rsdoiel/knowledge/cmd/kb@latest"

/** cmdKB handles /kb. It runs the installed kb with the given arguments in the
 * workspace, shows its output, and returns its exit status as the matching
 * class (workspace DR-0003). The knowledge base is the workspace's, passed
 * with -db unless the arguments name one. Two verbs are harvey's own and are
 * not sent to kb: inject (puts knowledge base content into the model's
 * context) and learn, and so is "project use ID", which sets harvey's
 * in-session current project.
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent.
 *   args ([]string)  — everything after /kb: options, a verb, its arguments.
 *   out  (io.Writer) — destination for kb's output.
 *
 * Returns:
 *   error — nil when kb exited 0; otherwise an error of the class kb's exit code names.
 *
 * Example:
 *   err := cmdKB(a, []string{"search", "streaming"}, os.Stdout)
 */
func cmdKB(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		return Usagef("usage: /kb [OPTIONS] VERB [ARGS...]   runs the installed kb (try /kb help); /kb inject and /kb learn are harvey's own")
	}
	verb, logArgs := kbVerbOf(args)
	if verb == "project" && len(logArgs) > 1 && strings.ToLower(logArgs[1]) == "use" {
		return kbProjectUse(a, logArgs[2:], out)
	}
	switch verb {
	case "inject", "learn":
		if a.KB == nil {
			return Unavailablef("the knowledge base is not open. This should not happen — please restart Harvey")
		}
		return a.loggedKB(verb, append([]string{verb}, args[1:]...), out, func(w io.Writer) error {
			if verb == "inject" {
				return kbInject(a, args[1:], w)
			}
			return kbLearn(a, args[1:], w)
		})
	}
	return a.loggedKB(verb, logArgs, out, func(w io.Writer) error { return a.runKB(args, w) })
}

// kbProjectUse handles /kb project use ID, which sets harvey's in-session
// current project (kb has no such notion); inject, learn and memory recall
// read it.
func kbProjectUse(a *Agent, args []string, out io.Writer) error {
	if len(args) != 1 {
		return Usagef("usage: /kb project use ID")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return Usagef("invalid project ID: %s", args[0])
	}
	a.Config.Memory.CurrentProjectID = id
	fmt.Fprintf(out, "Current project set to id=%d.\n", id)
	return nil
}

// kbVerbOf finds the verb in /kb's arguments: the first one that is not an
// option. It returns the verb and the arguments reordered as verb first, for
// the session log. An option that takes a value (-db PATH) is skipped with it.
func kbVerbOf(args []string) (verb string, logArgs []string) {
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			if (args[i] == "-db" || args[i] == "--db") && i+1 < len(args) {
				i++
			}
			continue
		}
		rest := append([]string(nil), args[:i]...)
		rest = append(rest, args[i+1:]...)
		return strings.ToLower(args[i]), append([]string{args[i]}, rest...)
	}
	return "", args
}

// knowledgeDBPath is the workspace's knowledge base, as kb should open it.
func (a *Agent) knowledgeDBPath() string {
	p := a.Config.Memory.KnowledgeDB
	if p == "" {
		return knowledge.DefaultPath(a.Workspace.Root)
	}
	if !filepath.IsAbs(p) {
		if abs, err := a.Workspace.AbsPath(p); err == nil {
			return abs
		}
	}
	return p
}

// runKB runs the installed kb. Options before the verb are kept in place, and
// -db is added in front unless the arguments already name one.
func (a *Agent) runKB(args []string, out io.Writer) error {
	bin, err := exec.LookPath("kb")
	if err != nil {
		return Unavailablef("kb is not installed (or not on PATH). Install it with: %s", kbInstallHint)
	}
	full := args
	hasDB := false
	for _, x := range args {
		if x == "-db" || x == "--db" || strings.HasPrefix(x, "-db=") || strings.HasPrefix(x, "--db=") {
			hasDB = true
		}
	}
	if !hasDB {
		full = append([]string{"-db", a.knowledgeDBPath()}, args...)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if a.Config.Security.RunTimeout > 0 {
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), a.Config.Security.RunTimeout)
	}
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, full...)
	cmd.Dir = a.Workspace.Root
	cmd.Env = filterCommandEnvironment(os.Environ())
	cmd.Stdout, cmd.Stderr = out, out // no stdin: kb's terminal-only verbs refuse, as they should
	runErr := cmd.Run()
	if cmd.ProcessState == nil {
		return startFailure("kb", runErr)
	}
	return kbExitError(cmd.ProcessState.ExitCode(), ctx.Err() != nil)
}

// kbExitError turns kb's exit code into an error of the class the workspace
// table (DR-0003) gives that code; a code the table does not know is internal.
func kbExitError(code int, timedOut bool) error {
	if timedOut {
		return ClassedAs(ClassUnavailable, fmt.Errorf("kb did not finish within the run timeout"))
	}
	if code == 0 {
		return nil
	}
	class := ClassInternal
	for _, c := range []ExitClass{ClassNegative, ClassUsage, ClassData, ClassNoInput, ClassUnavailable, ClassInternal,
		ClassCantCreate, ClassIO, ClassTempFail, ClassNoPermission, ClassConfig} {
		if c.Code == code {
			class = c
		}
	}
	return ClassedAs(class, fmt.Errorf("kb exited %d", code))
}

// kbVerbNames reads the verbs the installed kb has from kb -json verbs, for
// completion; harvey's own verbs are added. When kb is absent or answers
// unreadably only harvey's own are returned.
func kbVerbNames() []string {
	names := map[string]bool{"inject": true, "learn": true}
	if bin, err := exec.LookPath("kb"); err == nil {
		if raw, err := exec.Command(bin, "-json", "verbs").Output(); err == nil {
			var t struct {
				Verbs []struct {
					Name string `json:"name"`
				} `json:"verbs"`
			}
			if json.Unmarshal(raw, &t) == nil {
				for _, v := range t.Verbs {
					names[v.Name] = true
				}
			}
		}
	}
	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// kbInject formats KB content as Markdown and adds it to the conversation as
// context. With no argument it uses the current project (or all projects when
// none is set); with a project name it injects only that project.
func kbInject(a *Agent, args []string, out io.Writer) error {
	projectID := a.Config.Memory.CurrentProjectID
	label := "all projects"

	if len(args) > 0 {
		name := strings.Join(args, " ")
		p, err := a.KB.ProjectByName(name)
		if err != nil {
			return err
		}
		if p == nil {
			return Negativef("project %q not found. Use /kb project list to see available projects", name)
		}
		projectID = p.ID
		label = fmt.Sprintf("project %q", p.Name)
	} else if projectID > 0 {
		label = fmt.Sprintf("current project (id=%d)", projectID)
	}

	md, err := a.KB.FormatMarkdown(projectID)
	if err != nil {
		return err
	}
	if md == "" {
		fmt.Fprintln(out, "  Knowledge base is empty.")
		return nil
	}

	injectRef := "kb:all"
	if projectID > 0 {
		injectRef = fmt.Sprintf("kb:project:%d", projectID)
	}
	a.noteKBInjection(injectRef, "[knowledge base context]\n\n"+md)
	a.addPrompt("user", "kb-inject", "[knowledge base context]\n\n"+md)
	fmt.Fprintf(out, green("✓")+" KB context for %s injected (%d bytes).\n", label, len(md))
	return nil
}
