package harvey

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// D5 of the observation-drafting plan (design/observation-drafting-design.md,
// decisions 1, 4, 5): resolve the project, read what kb holds, write drafts.
// Everything goes through the installed kb (lookKB, so a missing or old kb is
// unavailable and nothing runs). The only observation kb is ever asked to add
// is a draft signed with --by; nothing here promotes one (TestDraftObs_*).

// kbFailure turns a non-zero kb exit into an error of the right class that
// carries kb's own message.
func kbFailure(what string, exit int, stderr []byte) error {
	msg := strings.TrimSpace(string(stderr))
	if i := strings.LastIndex(msg, "\n"); i >= 0 {
		msg = strings.TrimSpace(msg[i+1:])
	}
	var env struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(msg), &env) == nil && env.Error != "" {
		msg = env.Error
	}
	class := ExitCodeFor(kbExitError(exit, false))
	if msg == "" {
		return ClassedAs(class, fmt.Errorf("%s: kb exited %d", what, exit))
	}
	return ClassedAs(class, fmt.Errorf("%s: %s", what, msg))
}

/** DraftBy is the name a draft is signed with: "harvey" and the model, so the
 * review queue shows which model wrote it.
 *
 * Parameters:
 *   clientName (string) — the drafter's name, such as "ollama (qwen2.5:3b)".
 *
 * Returns:
 *   string — "harvey qwen2.5:3b"; just "harvey" when the name is empty.
 *
 * Example:
 *   DraftBy("ollama (qwen2.5:3b)") // "harvey qwen2.5:3b"
 */
func DraftBy(clientName string) string {
	m := strings.TrimSpace(modelFromClientName(clientName))
	if m == "" {
		return "harvey"
	}
	return "harvey " + m
}

/** KBProjectNames lists the names of the kb projects in db.
 *
 * Parameters:
 *   root (string) — working directory for kb (the workspace root).
 *   db   (string) — path of the knowledge database.
 *
 * Returns:
 *   []string — project names, in kb's order.
 *   error    — unavailable (69) when kb is missing or too old, else kb's class.
 *
 * Example:
 *   names, err := KBProjectNames(a.Workspace.Root, a.knowledgeDBPath())
 */
func KBProjectNames(root, db string) ([]string, error) {
	out, stderr, exit, err := runKBJSON(root, db, 0, "project", "list")
	if err != nil {
		return nil, err
	}
	if exit != 0 {
		return nil, kbFailure("kb project list", exit, stderr)
	}
	var rows []struct{ Name string }
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, ClassedAs(ClassData, fmt.Errorf("kb project list: unreadable output: %w", err))
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return names, nil
}

/** ResolveDraftProject picks the kb project drafts are attached to: the name
 * given with --project, else memory.knowledge_base.project from harvey.yaml,
 * else the workspace directory's name when a kb project has it. A name that
 * kb does not have is exit 1 and lists the projects; with nothing to go on it
 * is a usage error (exit 2) that lists them.
 *
 * Parameters:
 *   root       (string) — the workspace root; its base name is the last resort.
 *   db         (string) — path of the knowledge database.
 *   flag       (string) — --project, or "".
 *   configured (string) — memory.knowledge_base.project, or "".
 *
 * Returns:
 *   string — the project's name.
 *   error  — described above.
 *
 * Example:
 *   project, err := ResolveDraftProject(root, db, "harvey", "")
 */
func ResolveDraftProject(root, db, flag, configured string) (string, error) {
	names, err := KBProjectNames(root, db)
	if err != nil {
		return "", err
	}
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	list := strings.Join(names, ", ")
	if list == "" {
		list = "none yet"
	}
	for _, want := range []string{flag, configured} {
		if want == "" {
			continue
		}
		if !have[want] {
			return "", ClassedAs(ClassNegative, fmt.Errorf("kb project %q not found (projects: %s)", want, list))
		}
		return want, nil
	}
	if base := filepath.Base(root); have[base] {
		return base, nil
	}
	return "", Usagef("no kb project to draft into: name one with --project, or set memory.knowledge_base.project in agents/harvey.yaml (projects: %s)", list)
}

/** ExistingObservationBodies returns the text of every observation in a
 * project, reviewed or drafted, for the duplicate check.
 *
 * Parameters:
 *   root, db (string) — as for KBProjectNames.
 *   project  (string) — the kb project.
 *
 * Returns:
 *   []string — the bodies.
 *   error    — exit 1 (negative) when the project does not exist.
 *
 * Example:
 *   bodies, err := ExistingObservationBodies(root, db, "harvey")
 */
func ExistingObservationBodies(root, db, project string) ([]string, error) {
	out, stderr, exit, err := runKBJSON(root, db, 0, "observation", "list", "--project", project, "--status", "all")
	if err != nil {
		return nil, err
	}
	if exit != 0 {
		return nil, kbFailure("kb observation list", exit, stderr)
	}
	var rows []struct{ Body string }
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, ClassedAs(ClassData, fmt.Errorf("kb observation list: unreadable output: %w", err))
	}
	bodies := make([]string, 0, len(rows))
	for _, r := range rows {
		bodies = append(bodies, r.Body)
	}
	return bodies, nil
}

/** WriteDraft records one proposal in kb as a draft: it is not searchable and
 * is not recalled until a person promotes it. It is the only place harvey adds
 * an observation, and it always passes --draft and --by.
 *
 * Parameters:
 *   root, db (string) — as for KBProjectNames.
 *   project  (string) — the kb project.
 *   by       (string) — who drafted it, from DraftBy; one non-blank line. kb enforces
 *                       this, and the kind, and answers a bad one with exit 2.
 *   p        (Proposal) — the kind and the text (with its origin line).
 *
 * Returns:
 *   int64 — the draft's observation id.
 *   error — usage (2) for a blank by or a kind kb does not have; exit 1 for an
 *           unknown project; unavailable (69) when kb is missing or too old.
 *
 * Example:
 *   id, err := WriteDraft(root, db, "harvey", DraftBy(d.Name()), p)
 */
func WriteDraft(root, db, project, by string, p Proposal) (int64, error) {
	out, stderr, exit, err := runKBJSON(root, db, 0,
		"observation", "add", "--project", project, "--draft", "--by", by, p.Kind, p.Body)
	if err != nil {
		return 0, err
	}
	if exit != 0 {
		return 0, kbFailure("kb observation add --draft", exit, stderr)
	}
	var res struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(out, &res); err != nil || res.ID == 0 {
		return 0, ClassedAs(ClassData, fmt.Errorf("kb observation add --draft: unreadable output %q", strings.TrimSpace(string(out))))
	}
	return res.ID, nil
}
