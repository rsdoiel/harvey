
# Action Items

## Bugs

- `/model use` (no arg): prints usage error instead of showing a picker of registered
  llamafile models and aliases. Should call SelectFromStrings(allModelNames(a), ...) when
  len(args) < 2, matching the UX of /ollama use, /llamafile use, and /rag use.

- `/session use` / `/session continue` (no arg): prints usage error instead of showing a
  picker of available session files. Should call ListSessionFiles(a.SessionsDir) and
  SelectFrom when len(args) < 2, matching the UX of /rag use and /llamafile use.

## Release Review

## Next (v0.0.15 release)


