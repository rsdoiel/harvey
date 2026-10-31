%harvey(7) user manual | version 0.0.20 unreleased
% R. S. Doiel
% 2026-10-30

# NAME

PIN — keep something in front of the model for this session

# SYNOPSIS

/pin --text TEXT...
/pin EVENT_ID
/pin kb:record:PROJECT/DR-NNNN
/pin kb:observation:ID
/pins
/unpin N | PIN_ID | REF

# DESCRIPTION

A pin keeps something in the prompt however long the conversation gets. The
prompt for each turn is made of the system prompt, then the pins, then
summaries, recalled knowledge, and the most recent turns; when the
conversation outgrows the model's prompt limit, older turns are summarised or
left out, but pins stay. Use a pin for a constraint, a decision, or a passage
you cannot afford to lose.

A pin can be:

  --text TEXT       text you type
  EVENT_ID          an event of this session's log, such as the line you
                    typed or a model reply: the id looks like
                    harvey-session-20261030-1412#0042
  kb:record:REF     a decision record from the knowledge base, read with the
                    installed kb: kb:record:harvey/DR-0048
  kb:observation:ID an observation: kb:observation:592

A pin is recorded in the session log (a pin event, and an unpin event when it
is removed) and appears in the manifest of every turn after it. Pinning an
event does not remove it from the dialog, so while it is still among the recent
turns it is in the prompt twice.

# SCOPE

Pins belong to the session. A new session starts with none, /clear keeps them,
and they are not saved anywhere else. Use the knowledge base to keep something
beyond the session.

# THE CAP

Together the pins may take a quarter of the prompt limit. A pin that would go
over is refused with exit status 1, and the refusal lists the existing pins by
cost so you can /unpin one. When the prompt limit is not known there is no cap.

# SUBCOMMANDS

/pins
  List the pins: number, what was pinned, estimated tokens, share of the prompt
  limit, and the start of the text; then the total against the cap.

/unpin N
  Remove the pin numbered N in /pins. The pin's id (the event id of the pin) or
  the text of its first column also work.

# EXIT STATUS

  0   done
  1   a pin over the cap; an event or kb row that does not exist; no log to
      look an event up in
  2   a wrong argument: no argument, something that is not an event id, a kb
      identity or --text, an unpin that names no pin

# SEE ALSO

  /context   — show what the model sees, with pins and their share
  /clear     — reset the conversation (pins survive)
  /summarize — condense history by hand
  /kb        — the knowledge base the kb: pins read from

