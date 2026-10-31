%harvey(7) user manual | version 0.0.20 unreleased
% R. S. Doiel
% 2026-10-30

# NAME

CONTEXT — show what the model sees on the next turn

# SYNOPSIS

/context

# DESCRIPTION

/context prints the prompt Harvey would send next, grouped by what each part
is, with its estimated size in tokens and its share of the prompt limit:

  system prompt        the instructions from HARVEY.md and the skill catalog
  pinned               what you asked to keep with /pin
  summaries            summaries Harvey made of older turns
  recalled knowledge   knowledge-base rows recalled or injected
  dialog               the turns kept word for word

The prompt limit is the model's known prompt limit, or 85% of its context
window when only the window is known. When neither is known, nothing is left
out and the shares are not shown. If the next turn would leave older turns out,
/context says how many items.

The same figures are written to the session log: each turn's model_request
names the pinned items, the summaries, the tail kept and how many items were
left out.

/context add, /context show and /context clear were removed. Use
/pin --text TEXT, /pins and /unpin N instead.

# SEE ALSO

  /pin       — keep something in front of the model for this session
  /pins      — list what is pinned, with cost
  /unpin     — stop keeping a pin
  /summarize — condense history by hand
  /help pin

