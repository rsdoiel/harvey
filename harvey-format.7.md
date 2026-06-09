%harvey(7) user manual | version 0.0.10 8c8d863
% R. S. Doiel
% 2026-06-09

# NAME

FORMAT — format source files in-place using language-specific tools

# SYNOPSIS

/format FILE [FILE...]

# DESCRIPTION

/format reads each FILE from the workspace, detects its language from the
file extension, runs the registered formatter for that language, and writes
the result back to FILE if formatting changed it.

Each file is processed independently and reported on its own line, so a
single /format call can format files in several languages at once.

# SUPPORTED LANGUAGES

~~~
  Language    Extensions               Formatter
  ─────────── ──────────────────────── ─────────────────────────────
  Go          .go .mod .sum            gofmt
  C           .c .h                    clang-format
  C++         .cpp .cc .cxx .hpp .hh   clang-format
  Python      .py                      black
  Rust        .rs                      rustfmt
  JavaScript  .js                      prettier
  TypeScript  .ts                      prettier
  Pascal      .pas .p                  built-in normaliser
  Oberon      .obn (.Mod)              built-in normaliser
  BASIC       .bas .bi                 built-in normaliser
~~~

External formatters (gofmt, clang-format, black, rustfmt, prettier) read the
file's content on stdin and write the formatted result on stdout — Harvey
only overwrites the file on disk after the formatter succeeds. If the tool is
not installed, /format reports the file as already formatted rather than
failing.

The Pascal, Oberon, and BASIC formatters are built into Harvey and only
normalise whitespace and line endings; no external tool is required.

# OUTPUT

For each FILE, /format prints one of:

  FILE: formatted (N → M bytes)
    The formatter changed the file; it was rewritten with the new size.

  FILE: already formatted
    The formatter ran but produced identical output, or the formatter's
    external tool is not installed.

  FILE: no language registered for extension ".ext"
    The file extension is not recognised by Harvey's language registry.

  FILE: no formatter registered for "LANG"
    The language is recognised but has no formatter configured.

  FILE: read error: ...
  FILE: path error: ...
  FILE: write error: ...
    A filesystem error occurred; the file is left unchanged.

# SAFE MODE

A formatter that rewrites a file in place (rather than via stdin/stdout) is
blocked while Safe Mode is on, and /format reports:

  FILE: file-mode formatter requires safe mode off (/safemode off)

None of the default formatters listed above use this mode, so /format
behaves the same with Safe Mode on or off. See /help security for details.

# EXAMPLES

Format a single Go file:

~~~
  harvey > /format harvey/spinner.go
    harvey/spinner.go: formatted (4312 → 4298 bytes)
~~~

Format several files across languages in one call:

~~~
  harvey > /format cmd/harvey/main.go agents/scripts/build.py
    cmd/harvey/main.go: already formatted
    agents/scripts/build.py: formatted (1820 → 1795 bytes)
~~~

# SEE ALSO

  /write PATH       — save the last assistant reply to a file
  /run COMMAND      — run a shell command (e.g. a formatter not yet wired up)
  /help security    — Safe Mode and file-mode formatter restrictions

