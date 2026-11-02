package harvey

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// D3 of the observation-drafting plan (design/observation-drafting-design.md,
// decisions 2, 5 and 6): the prompt, the reading of the reply, the origin line.

/** proposalBodyCap is the most a drafted observation's text keeps, in bytes,
 * before the origin line; a longer one is cut at a word and marked.
 */
const proposalBodyCap = 600

// draftKinds is the vocabulary of kb observation kinds, which a draft uses.
var draftKinds = []string{"note", "finding", "decision", "question", "hypothesis"}

/** Proposal is one observation a model suggests keeping.
 *
 * Fields:
 *   Kind (string) — one of note, finding, decision, question, hypothesis.
 *   Body (string) — one line of text, at most proposalBodyCap bytes.
 *
 * Example:
 *   // {Kind: "finding", Body: "hailo-ollama closes the stream past 3,300 characters"}
 */
type Proposal struct {
	Kind string
	Body string
}

const draftInstructions = `You read part of a working session between an author and a coding assistant, and you note what is worth remembering in a later session.

Trust what the commands showed and what the author said over what the assistant claimed: the assistant is often wrong. A claim about the world that only the assistant made, with no command output or tool result in the excerpt to back it, is not worth keeping: do not write it as a note or a finding. If a tool result in the excerpt says something different from what the assistant then claimed, write that as a finding: what the tool result showed, and that the assistant's answer did not match it. Keep only what a later session would want: how a tool or model behaved, a decision and the reason for it, a failure and its cause, a question left open. Leave out chit-chat, steps that went as expected, and anything already obvious from the files.

Reply with one observation per line. Begin each line with one of the words note, finding, decision, question or hypothesis, then a colon. For example:
finding: the build fails on Go 1.26 because go.mod now requires 1.27
question: does the tool-result role work on the hailo models?
decision: draft with the 3b model on the Pi because the 7b one is too slow
finding: grep found parse() in utils.go, but the assistant's answer said it did not exist and did not match the output

Write each as one self-contained sentence under 300 characters that makes sense without the conversation. Never copy a password, key or token.

If nothing is worth keeping, reply with the single word NONE.`

/** BuildDraftMessages writes the request for one drafting pass: the
 * instructions as the system message and the pass's entries as the user's.
 *
 * Parameters:
 *   pass    (DraftPass) — the transcript to read.
 *   project (string)    — the kb project the drafts will go to, named so the
 *                         model knows what the observations are for.
 *
 * Returns:
 *   []Message — a system and a user message.
 *
 * Example:
 *   msgs := BuildDraftMessages(pass, "harvey")
 */
func BuildDraftMessages(pass DraftPass, project string) []Message {
	var b strings.Builder
	fmt.Fprintf(&b, "Project: %s\n\nSession excerpt (events %s to %s):\n", project, pass.First, pass.Last)
	for _, e := range pass.Entries {
		b.WriteString("\n")
		b.WriteString(e.Text)
		b.WriteString("\n")
	}
	return []Message{
		{Role: "system", Content: draftInstructions},
		{Role: "user", Content: b.String()},
	}
}

var (
	proposalLine   = regexp.MustCompile(`^([A-Za-z]+)(?:\s*:|\s+[-–—])\s*(\S.*)$`)
	proposalShaped = regexp.MustCompile(`^[A-Za-z]+\s*:\s*\S`)
	kindOnly       = regexp.MustCompile(`^([A-Za-z]+)\s*:?\s*$`)
	bodyLabel      = regexp.MustCompile(`(?i)^(?:text|body|observation)\s*[:\-–—]\s*`)
	noneLine       = regexp.MustCompile(`(?i)^none\b`)
	listMarker     = regexp.MustCompile(`^(?:[-*•]\s+|\d+[.)]\s+)`)
)

func isDraftKind(w string) bool {
	for _, k := range draftKinds {
		if k == w {
			return true
		}
	}
	return false
}

// classifyProposalLine says whether a cleaned line names a kind, and if so which
// and what follows it on the line. "finding: text" is a kind with a body;
// "finding", "finding:" and the literal "KIND: finding" a small model copies from
// a placeholder are a kind alone, whose text is the next line.
func classifyProposalLine(line string) (kind, body string, isKind bool) {
	if m := proposalLine.FindStringSubmatch(line); m != nil {
		w := strings.ToLower(m[1])
		if isDraftKind(w) {
			return w, strings.TrimSpace(m[2]), true
		}
		if w == "kind" {
			if t := strings.ToLower(strings.Trim(strings.TrimSpace(m[2]), ".*")); isDraftKind(t) {
				return t, "", true
			}
		}
		return "", "", false
	}
	if m := kindOnly.FindStringSubmatch(line); m != nil && isDraftKind(strings.ToLower(m[1])) {
		return strings.ToLower(m[1]), "", true
	}
	return "", "", false
}

/** ParseProposals reads a drafter's reply. It expects one "KIND: text" per
 * line and tolerates what small models write: list markers and numbering,
 * bold markers, code fences, a preamble line, a kind in any case, "-" in place
 * of ":", and a kind alone on a line (with or without a colon, or as the
 * literal "KIND: finding" copied from a placeholder) with its text on the next
 * line, which may carry a "Text:" label. A line shaped like "WORD: text" whose
 * WORD is not a kind is dropped and counted; a line with no such shape (a
 * preamble) is ignored; an empty body is dropped. NONE, or an empty reply, is
 * no proposals. A body over proposalBodyCap is cut at a word.
 *
 * Parameters:
 *   reply (string) — the model's text.
 *
 * Returns:
 *   []Proposal — in the order written.
 *   int        — how many "WORD: text" lines were dropped for an unknown kind.
 *
 * Example:
 *   props, dropped := ParseProposals("finding: the build needs Go 1.27.3\nNONE")
 */
func ParseProposals(reply string) ([]Proposal, int) {
	var out []Proposal
	dropped := 0
	pending := ""
	emit := func(kind, body string) {
		body = strings.TrimSpace(body)
		if body != "" {
			out = append(out, Proposal{Kind: kind, Body: cutAtWord(body, proposalBodyCap)})
		}
	}
	for _, raw := range strings.Split(reply, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		line = listMarker.ReplaceAllString(line, "")
		line = strings.ReplaceAll(line, "**", "")
		line = strings.TrimSpace(line)
		if noneLine.MatchString(line) {
			pending = ""
			continue
		}
		kind, body, isKind := classifyProposalLine(line)
		switch {
		case isKind && body != "":
			emit(kind, body)
			pending = ""
		case isKind:
			pending = kind
		case pending != "":
			emit(pending, bodyLabel.ReplaceAllString(line, ""))
			pending = ""
		case proposalShaped.MatchString(line):
			dropped++
		}
	}
	return out, dropped
}

// cutAtWord cuts s to at most max bytes at a word boundary and marks the cut.
func cutAtWord(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	head := s[:cut]
	if i := strings.LastIndexAny(head, " \t"); i > cut/2 {
		head = head[:i]
	}
	return strings.TrimRight(head, " \t,;:") + "…"
}

/** FinishBody adds the origin line to a proposal's text (design decision 6):
 * "Drafted from <session>#<first>..#<last>." naming the events of the pass it
 * came from, so a reviewer can open that span of the log. When kb can link a
 * draft to a session event this line goes and the link takes its place.
 *
 * Parameters:
 *   p    (Proposal)  — the proposal.
 *   pass (DraftPass) — the pass that produced it.
 *
 * Returns:
 *   string — the body followed by a newline and the origin line.
 *
 * Example:
 *   FinishBody(p, pass) // "the stream closes\nDrafted from s#0042..#0057."
 */
func FinishBody(p Proposal, pass DraftPass) string {
	first, last := pass.First, pass.Last
	span := first
	if first != last {
		if i := strings.LastIndex(last, "#"); i >= 0 && strings.HasPrefix(last, first[:strings.LastIndex(first, "#")+1]) {
			span = first + ".." + last[i:]
		} else {
			span = first + ".." + last
		}
	}
	return p.Body + "\nDrafted from " + span + "."
}

/** RunDraftPass asks the drafter about one pass and reads the answer. The call
 * is an ad-hoc model request with the purpose "draft", so the session log says
 * what it was for.
 *
 * Parameters:
 *   ctx     (context.Context) — carries cancellation; the purpose is added.
 *   d       (Drafter)         — the model, local or a route.
 *   pass    (DraftPass)       — the transcript to read.
 *   project (string)          — the kb project the drafts are for.
 *   session (string)          — the session being read, for the log's record.
 *   n, of   (int)             — this pass's number and the pass count, 1-based.
 *
 * Returns:
 *   []Proposal — what the model suggested, parsed.
 *   int        — lines dropped for an unknown kind.
 *   error      — the model's error, naming the pass and wrapping the cause.
 *
 * Example:
 *   props, dropped, err := RunDraftPass(ctx, d, pass, "harvey", "s1", 1, 2)
 */
func RunDraftPass(ctx context.Context, d Drafter, pass DraftPass, project, session string, n, of int) ([]Proposal, int, error) {
	ctx = withCallPurpose(ctx, "draft", map[string]any{"session": session, "pass": n, "of": of, "from": pass.First, "to": pass.Last})
	ctx = withMaxTokens(ctx, DraftMaxTokens)
	reply, err := d.Draft(ctx, BuildDraftMessages(pass, project))
	if err != nil {
		return nil, 0, fmt.Errorf("pass %d of %d: %w", n, of, err)
	}
	props, dropped := ParseProposals(reply)
	return props, dropped, nil
}
