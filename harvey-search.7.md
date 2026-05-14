%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

SEARCH — regex search across workspace files

# SYNOPSIS

/search PATTERN [PATH]

# DESCRIPTION

/search walks the workspace (or a subdirectory) and prints every line that
matches PATTERN. PATTERN is a Go regular expression.

Results are shown in the format:

  file.go:42: matching line text

Hidden directories (names beginning with ".") are skipped. Results are
capped at 200 matches to prevent flooding the context window. If the cap
is reached, a truncation notice is printed.

PATH is relative to the workspace root. When omitted, the entire workspace
is searched.

/search is useful for finding where a symbol is defined or used before
asking the model to explain or modify it. The results are printed to the
REPL but are not automatically injected into the conversation — paste the
relevant lines or use /read to load the file.

# EXAMPLES

Search for a function name:

~~~
  harvey > /search ragAugment
~~~

Search for a TODO comment in a subdirectory:

~~~
  harvey > /search "TODO|FIXME" src/
~~~

Case-insensitive search:

~~~
  harvey > /search "(?i)context.length"
~~~

# SEE ALSO

  /read FILE...    — load a file into context after finding it
  /files [PATH]    — list directory contents

