package harvey

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// sessionpins.go — /pin, /unpin, /pins and /context (plan L8, DR-0038 item 4).
//
// A pin keeps something in front of the model for the rest of the session: free
// text, an event of the session log, or a row of the knowledge base. It is a
// pin event (the thing pinned and what it costs) and a message in the prompt,
// kept ahead of the dialog by the assembler. Pins belong to the session: a new
// session starts with none, and /clear keeps them.

// pinShare is the largest share of the prompt limit that pins may take together.
const pinShare = 0.25

// pinPrefix opens every pinned message; the assembler recognises pins by it.
const pinPrefix = "[pinned context]"

/** Pin is something kept in front of the model for the session.
 *
 * Fields:
 *   ID      (string) — the pin event's id in the session log; "pin-N" with no log.
 *   Ref     (string) — what was pinned: "text", an event id, or "kb:<type>:<id>".
 *   Text    (string) — the text pinned.
 *   Message (string) — the message that carries it in the prompt.
 *   Tokens  (int)    — its estimated cost.
 *
 * Example:
 *   for _, p := range a.Pins { fmt.Println(p.Ref, p.Tokens) }
 */
type Pin struct {
	ID      string
	Ref     string
	Text    string
	Message string
	Tokens  int
}

// pinMessage is the message a pin puts in the prompt.
func pinMessage(ref, text string) string {
	if ref == "text" {
		return pinPrefix + "\n\n" + text
	}
	return pinPrefix + "\n\n(" + ref + ")\n" + text
}

/** pinLimit returns the most tokens the pins may cost together, 0 for no limit
 * when the prompt limit is unknown.
 *
 * Example:
 *   if cap := a.pinLimit(); cap > 0 && cost > cap { refuse() }
 */
func (a *Agent) pinLimit() int {
	limit, _ := a.assembleLimit()
	return int(float64(limit) * pinShare)
}

func (a *Agent) pinTokens() int {
	n := 0
	for _, p := range a.Pins {
		n += p.Tokens
	}
	return n
}

// resolvePinTarget returns the text a /pin argument stands for.
func (a *Agent) resolvePinTarget(ref string) (string, error) {
	switch {
	case strings.HasPrefix(ref, "kb:"):
		return a.kbText(ref)
	case strings.Contains(ref, "#"):
		return a.eventText(ref)
	}
	return "", Usagef("don't know what %q is: give an event id (session#0042), a kb identity (kb:record:harvey/DR-0034) or --text TEXT", ref)
}

// eventText returns the text of an event of this session's log.
func (a *Agent) eventText(id string) (string, error) {
	if a.SessionLog == nil {
		return "", Negativef("there is no session log to look %s up in", id)
	}
	ev, err := a.SessionLog.Index().Event(id)
	if err != nil {
		return "", err
	}
	opts := RenderOptions{Blobs: BlobDir(a.SessionLog.Path())}
	var d struct {
		Text    json.RawMessage `json:"text"`
		Output  json.RawMessage `json:"output"`
		Message json.RawMessage `json:"message"`
		Args    json.RawMessage `json:"args"`
	}
	_ = json.Unmarshal(ev.Data, &d)
	for _, raw := range []json.RawMessage{d.Text, d.Output, d.Message, d.Args} {
		if len(raw) > 0 {
			if t := opts.contentOf(raw); t != "" {
				return t, nil
			}
		}
	}
	return "", Negativef("event %s (%s) has no text to pin", id, ev.Kind)
}

// kbText returns the text of a kb row, read with the installed kb.
func (a *Agent) kbText(ref string) (string, error) {
	parts := strings.SplitN(ref, ":", 3)
	if len(parts) != 3 || parts[2] == "" {
		return "", Usagef("a kb identity is kb:<type>:<id>, for example kb:record:harvey/DR-0034, got %q", ref)
	}
	var args []string
	switch parts[1] {
	case "record":
		args = []string{"record", "show", parts[2]}
	case "observation":
		args = []string{"--json", "observation", "show", parts[2]}
	default:
		return "", Usagef("can pin kb records and observations, not %q", parts[1])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kb", args...)
	if a.Workspace != nil {
		cmd.Dir = a.Workspace.Root
	}
	raw, err := cmd.Output()
	if err != nil {
		return "", Negativef("kb could not show %s: %v", ref, err)
	}
	if parts[1] == "observation" {
		var o struct{ Body string }
		if json.Unmarshal(raw, &o) != nil || o.Body == "" {
			return "", Negativef("kb returned no body for %s", ref)
		}
		return o.Body, nil
	}
	return strings.TrimSpace(string(raw)), nil
}

/** addPin pins text, recording it in the log and putting it in the prompt. A
 * pin that would take the pins past their share of the prompt limit is
 * refused (negative, exit 1) with the existing pins listed by cost.
 *
 * Parameters:
 *   ref  (string) — "text", an event id or a kb identity.
 *   text (string) — what to pin.
 *
 * Returns:
 *   Pin   — the pin made.
 *   error — negative if over the cap, usage if the text is empty.
 *
 * Example:
 *   p, err := a.addPin("text", "keep answers short")
 */
func (a *Agent) addPin(ref, text string) (Pin, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Pin{}, Usagef("nothing to pin")
	}
	msg := pinMessage(ref, text)
	cost := itemTokens(Message{Role: "user", Content: msg})
	if cap := a.pinLimit(); cap > 0 && a.pinTokens()+cost > cap {
		var b strings.Builder
		fmt.Fprintf(&b, "that pin costs ~%d tokens and the pins may take %d (%d%% of the prompt limit); now pinned:", cost, cap, int(pinShare*100))
		for i, p := range a.Pins {
			fmt.Fprintf(&b, "\n  %d  %s  ~%d tokens", i+1, p.Ref, p.Tokens)
		}
		b.WriteString("\n/unpin one to make room")
		return Pin{}, Negativef("%s", b.String())
	}
	p := Pin{ID: fmt.Sprintf("pin-%d", len(a.Pins)+1), Ref: ref, Text: text, Message: msg, Tokens: cost}
	if id := a.logEvent(KindPin, LevelInfo, map[string]any{
		"turn": a.logTurn, "ref": ref, "text": a.logContent(text), "tokens": cost,
	}); id != "" {
		p.ID = id
	}
	a.Pins = append(a.Pins, p)
	a.addPrompt("user", "pinned", msg)
	return p, nil
}

/** removePin unpins the pin with this number (as /pins lists them), event id
 * or ref, takes its message out of the prompt and logs it.
 *
 * Parameters:
 *   which (string) — a number, the pin event's id, or the ref.
 *
 * Returns:
 *   Pin   — the pin removed.
 *   error — usage if nothing matches.
 *
 * Example:
 *   p, err := a.removePin("2")
 */
func (a *Agent) removePin(which string) (Pin, error) {
	idx := -1
	if n, err := strconv.Atoi(which); err == nil && n >= 1 && n <= len(a.Pins) {
		idx = n - 1
	} else {
		for i, p := range a.Pins {
			if p.ID == which || p.Ref == which {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		return Pin{}, Usagef("no pin %q; /pins lists them", which)
	}
	p := a.Pins[idx]
	a.Pins = append(a.Pins[:idx:idx], a.Pins[idx+1:]...)
	before := a.historySnapshot()
	for i, m := range a.History {
		if m.Role == "user" && m.Content == p.Message {
			a.History = append(a.History[:i:i], a.History[i+1:]...)
			break
		}
	}
	a.logRewrite("unpin", before)
	a.logEvent(KindUnpin, LevelInfo, map[string]any{"turn": a.logTurn, "pin": p.ID})
	return p, nil
}

// share formats n as a percentage of limit, or "" when the limit is unknown.
func share(n, limit int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" (%d%%)", n*100/limit)
}

// cmdPin handles /pin REF and /pin --text TEXT.
func cmdPin(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		return Usagef("usage: /pin <EVENT_ID|kb:TYPE:ID|--text TEXT...>")
	}
	ref, text := "", ""
	if args[0] == "--text" || args[0] == "-t" {
		if len(args) < 2 {
			return Usagef("usage: /pin --text TEXT...")
		}
		ref, text = "text", strings.Join(args[1:], " ")
	} else {
		if len(args) > 1 {
			return Usagef("usage: /pin <EVENT_ID|kb:TYPE:ID|--text TEXT...>")
		}
		var err error
		if text, err = a.resolvePinTarget(args[0]); err != nil {
			return err
		}
		ref = args[0]
	}
	p, err := a.addPin(ref, text)
	if err != nil {
		return err
	}
	limit, _ := a.assembleLimit()
	fmt.Fprintf(out, green("✓")+" Pinned %s, ~%d tokens%s. The pins keep to %d%% of the prompt limit; /pins lists them.\n",
		p.Ref, p.Tokens, share(p.Tokens, limit), int(pinShare*100))
	return nil
}

// cmdUnpin handles /unpin N|ID.
func cmdUnpin(a *Agent, args []string, out io.Writer) error {
	if len(args) != 1 {
		return Usagef("usage: /unpin <N|PIN_ID|REF>")
	}
	p, err := a.removePin(args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(out, green("✓")+" Unpinned %s.\n", p.Ref)
	return nil
}

// cmdPins handles /pins.
func cmdPins(a *Agent, args []string, out io.Writer) error {
	if len(a.Pins) == 0 {
		fmt.Fprintln(out, "  No pins. /pin --text TEXT, /pin EVENT_ID or /pin kb:record:harvey/DR-0034 keeps something in front of the model for this session.")
		return nil
	}
	limit, _ := a.assembleLimit()
	for i, p := range a.Pins {
		preview := strings.Join(strings.Fields(p.Text), " ")
		if len(preview) > 60 {
			preview = preview[:57] + "..."
		}
		fmt.Fprintf(out, "  %d  %-28s ~%d tokens%s  %s\n", i+1, p.Ref, p.Tokens, share(p.Tokens, limit), preview)
	}
	cap := a.pinLimit()
	fmt.Fprintf(out, "  total ~%d tokens%s", a.pinTokens(), share(a.pinTokens(), limit))
	if cap > 0 {
		fmt.Fprintf(out, " of the %d the pins may take", cap)
	}
	fmt.Fprintln(out)
	return nil
}

// cmdContext shows what the model sees on the next turn.
func cmdContext(a *Agent, args []string, out io.Writer) error {
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "add", "clear", "show":
			return Usagef("/context %s was removed: pin with /pin --text TEXT, list with /pins, remove with /unpin N; /context now shows what the model sees", args[0])
		}
		return Usagef("usage: /context")
	}
	items := a.currentItems()
	limit, source := a.assembleLimit()
	msgs, man := Assemble(items, AssembleBudget{Limit: limit})
	_ = msgs
	tok := func(kind ItemKind) (n, count int) {
		for _, it := range items {
			if it.Kind == kind {
				n += itemTokens(it.Msg)
				count++
			}
		}
		return n, count
	}
	fmt.Fprintf(out, "  What the model sees on the next turn")
	if limit > 0 {
		fmt.Fprintf(out, " (limit ~%d tokens, from %s)", limit, strings.ReplaceAll(source, "_", " "))
	}
	fmt.Fprintln(out, ":")
	row := func(label string, n, count int) {
		fmt.Fprintf(out, "    %-22s %3d item(s)  ~%d tokens%s\n", label, count, n, share(n, limit))
	}
	n, c := tok(ItemSystem)
	row("system prompt", n, c)
	n, c = tok(ItemPin)
	row("pinned", n, c)
	n, c = tok(ItemSummary)
	row("summaries", n, c)
	n, c = tok(ItemKB)
	row("recalled knowledge", n, c)
	tailTokens := 0
	for _, it := range items {
		if it.Kind == ItemTurn {
			tailTokens += itemTokens(it.Msg)
		}
	}
	fmt.Fprintf(out, "    %-22s %3d turn(s)  ~%d tokens%s\n", "dialog", man.Tail.Turns, tailTokens, share(tailTokens, limit))
	if len(man.Dropped) > 0 {
		fmt.Fprintf(out, "  %d earlier item(s) will be left out to fit; /pin keeps something in front of the model.\n", len(man.Dropped))
	}
	fmt.Fprintf(out, "  Total ~%d tokens%s.\n", man.UsedEst, share(man.UsedEst, limit))
	return nil
}

// currentItems returns the history as prompt items, from the session log when
// it reproduces the history exactly, otherwise classified from the messages.
func (a *Agent) currentItems() []PromptItem {
	if a.SessionLog != nil {
		path := a.SessionLog.Path()
		if events, _, err := ReadSessionLog(path); err == nil {
			items := RebuildItems(events, RenderOptions{Blobs: BlobDir(path)})
			if sameMessages(items, a.History) {
				return items
			}
		}
	}
	items := make([]PromptItem, len(a.History))
	g := 0
	for i, m := range a.History {
		if m.Role == "user" && i > 0 && classifyItem(m, "") == ItemTurn {
			g++
		}
		items[i] = PromptItem{Msg: m, Kind: classifyItem(m, ""), Group: g}
	}
	return items
}
