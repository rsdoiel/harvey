I want you to integrate the SessionManager into the Harvey agent. All the
relevant code is in this workspace. Here is exactly what needs to happen:

---

## 1. harvey.go — extend the Agent struct

Add two fields to the Agent struct (around line 107):

    Sessions   *SessionManager
    SessionID  int64

---

## 2. terminal.go — open sessions on startup

Add a new method initSessions alongside initKnowledgeBase (around line 227):

    func (a *Agent) initSessions(out io.Writer) {
        sm, err := OpenSessionManager(a.Workspace)
        if err != nil {
            fmt.Fprintf(out, "  ✗ Sessions unavailable: %v\n", err)
            return
        }
        a.Sessions = sm
        fmt.Fprintln(out, "✓ Sessions: .harvey/sessions.db")
    }

Call it in Run() immediately after a.initKnowledgeBase(out) (around line 84).

Also defer cleanup in Run() alongside the existing Recorder defer:

    defer func() {
        if a.Sessions != nil {
            a.Sessions.Close()
        }
    }()

---

## 3. terminal.go — resume or start a session after backend selection

After the selectBackend call in Run() succeeds (around line 98), add this
block. It loads the last session and offers to resume it; if the user
declines (or no session exists), it creates a fresh one.

    if a.Sessions != nil {
        last, err := a.Sessions.LoadLast()
        if err != nil {
            fmt.Fprintf(out, yellow("  ✗")+" Session load error: %v\n", err)
        } else if last != nil && len(last.History) > 0 {
            turns := 0
            for _, m := range last.History {
                if m.Role == "user" {
                    turns++
                }
            }
            label := last.Name
            if label == "" {
                label = "(unnamed)"
            }
            fmt.Fprintf(out, "\n  Last session: %s — %d turn(s), model: %s, %s\n",
                label, turns, last.Model, last.LastActive.Format("2006-01-02 15:04"))
            if askYesNo(reader, out, "  Resume last session? [Y/n] ", true) {
                a.History = last.History
                a.SessionID = last.ID
                fmt.Fprintf(out, green("✓")+" Session %d resumed (%d messages)\n",
                    last.ID, len(last.History))
            }
        }
        if a.SessionID == 0 {
            model := ""
            if a.Client != nil {
                model = a.Client.Name()
            }
            id, err := a.Sessions.Create(a.Workspace.Root, model, nil)
            if err != nil {
                fmt.Fprintf(out, yellow("  ✗")+" Could not create session: %v\n", err)
            } else {
                a.SessionID = id
            }
        }
    }

---

## 4. terminal.go — checkpoint after each chat turn

In the REPL loop, after a.AddMessage("assistant", buf.String()) (around
line 200), add a checkpoint save:

    if a.Sessions != nil && a.SessionID != 0 {
        model := ""
        if a.Client != nil {
            model = a.Client.Name()
        }
        if err := a.Sessions.Save(a.SessionID, model, a.History); err != nil {
            fmt.Fprintf(out, yellow("  ✗")+" Session save error: %v\n", err)
        }
    }

---

## 5. commands.go — update cmdClear to start a new session

In cmdClear (around line 222), after a.ClearHistory(), add:

    if a.Sessions != nil {
        model := ""
        if a.Client != nil {
            model = a.Client.Name()
        }
        id, err := a.Sessions.Create(a.Workspace.Root, model, nil)
        if err != nil {
            fmt.Fprintf(out, "  Session create error: %v\n", err)
        } else {
            a.SessionID = id
        }
    }

---

## 6. commands.go — update cmdStatus to show session info

In cmdStatus (around line 199), add after the KB block:

    if a.Sessions != nil && a.SessionID != 0 {
        fmt.Fprintf(out, "Session:   id=%d\n", a.SessionID)
    } else {
        fmt.Fprintln(out, "Session:   none")
    }

---

## 7. commands.go — add /session command

Register a new command in registerCommands():

    "session": {
        Usage:       "/session <list|resume ID|new|name TEXT>",
        Description: "Manage Harvey sessions",
        Handler:     cmdSession,
    },

Implement cmdSession:

    func cmdSession(a *Agent, args []string, out io.Writer) error {
        if a.Sessions == nil {
            fmt.Fprintln(out, "Sessions are not available.")
            return nil
        }
        if len(args) == 0 || strings.ToLower(args[0]) == "list" {
            sessions, err := a.Sessions.List()
            if err != nil {
                return err
            }
            if len(sessions) == 0 {
                fmt.Fprintln(out, "  (no sessions)")
                return nil
            }
            for _, s := range sessions {
                active := ""
                if s.ID == a.SessionID {
                    active = " *"
                }
                label := s.Name
                if label == "" {
                    label = "(unnamed)"
                }
                fmt.Fprintf(out, "  [%d]%s %s — %s — %s\n",
                    s.ID, active, label, s.Model,
                    s.LastActive.Format("2006-01-02 15:04"))
            }
            return nil
        }
        switch strings.ToLower(args[0]) {
        case "resume":
            if len(args) < 2 {
                fmt.Fprintln(out, "Usage: /session resume ID")
                return nil
            }
            var id int64
            if _, err := fmt.Sscanf(args[1], "%d", &id); err != nil {
                fmt.Fprintf(out, "Invalid session ID: %s\n", args[1])
                return nil
            }
            s, err := a.Sessions.Load(id)
            if err != nil {
                return err
            }
            if s == nil {
                fmt.Fprintf(out, "No session with id=%d\n", id)
                return nil
            }
            a.History = s.History
            a.SessionID = s.ID
            fmt.Fprintf(out, "Session %d resumed (%d messages, model: %s)\n",
                s.ID, len(s.History), s.Model)
        case "new":
            model := ""
            if a.Client != nil {
                model = a.Client.Name()
            }
            id, err := a.Sessions.Create(a.Workspace.Root, model, nil)
            if err != nil {
                return err
            }
            a.ClearHistory()
            a.SessionID = id
            fmt.Fprintf(out, "New session started (id=%d)\n", id)
        case "name":
            if len(args) < 2 {
                fmt.Fprintln(out, "Usage: /session name TEXT")
                return nil
            }
            if a.SessionID == 0 {
                fmt.Fprintln(out, "No active session.")
                return nil
            }
            name := strings.Join(args[1:], " ")
            if err := a.Sessions.Rename(a.SessionID, name); err != nil {
                return err
            }
            fmt.Fprintf(out, "Session %d named %q\n", a.SessionID, name)
        default:
            fmt.Fprintf(out, "Unknown session subcommand: %s\n", args[0])
            fmt.Fprintln(out, "Usage: /session <list|resume ID|new|name TEXT>")
        }
        return nil
    }

---

## Verification steps

After making all changes, run:

    go build ./...
    go test ./...

Both must pass before you report the task complete. Fix any compilation
errors before moving on. Do not change sessions.go or sessions_test.go —
those files are already correct.
