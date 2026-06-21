
# Action Items

## Bugs

### MatchesTrigger false-positive on short keywords inside filenames

`MatchesTrigger` uses `strings.Contains` for keyword mode, so any trigger
keyword that is a substring of a word in the prompt fires the skill.

Reproducer: asking Harvey to `read Rethinking_Data_Use_in_Large_Language_Models.pdf`
triggers the `harvey-memory` skill because `"at"` (from `"at session start"` in
its trigger) is a substring of `"data"` in the filename. Harvey then returns
the memory DIGEST.md instead of the PDF.

Fix: switch keyword matching from `strings.Contains` to whole-word
(`\b`-bounded) regex matching so that `"at"` only fires on the standalone
word, not inside `"data"`.

## Release Review

## Next (v0.0.15 release)


