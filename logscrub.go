package harvey

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// logFlagWords are the bare words recorded as flags on a session-log event.
// They are never redacted: a bare word is usually prose, and the author
// reviews the flags (DR-0036, plan item L1). It is a copy of the list in
// memory_scrub.go on purpose, so that file can be removed with the memory
// store (DR-0037) without touching this one.
var logFlagWords = []string{
	"password",
	"passwd",
	"token",
	"secret",
	"api_key",
	"apikey",
	"api-key",
	"-----BEGIN",
	"Authorization:",
	"Bearer ",
	"private_key",
	"privatekey",
	"aws_secret",
	"aws_access",
	"client_secret",
	"client_id",
}

// logRedactedMarker matches a marker this scrubber wrote on an earlier pass.
var logRedactedMarker = regexp.MustCompile(`\[REDACTED:[a-z]+\]`)

// logPEMBlock matches a private-key PEM or PGP block through its END line, or to the
// end of the text when the block is cut off. Certificates and public keys are
// not secrets and are left alone.
var logPEMBlock = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----.*?(?:-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----|\z)`)

// logAuthorization matches an Authorization header; group 1 is the header
// name and separator, group 2 the credential up to a quote or the line end.
var logAuthorization = regexp.MustCompile(`(?i)(\bAuthorization:[ \t]*)([^\r\n'"]+)`)

// logBearer matches a bare "Bearer <token>"; group 1 is the scheme and the
// space, group 2 the token. The scheme is matched case-sensitively, as HTTP
// writes it, so prose about "the bearer of" something is left alone.
var logBearer = regexp.MustCompile(`(\bBearer[ \t]+)([A-Za-z0-9._~+/=-]{3,})`)

// logSecretName matches NAME=value and NAME: value where NAME holds a known
// secret keyword. Group 1 is the name, group 2 the keyword, group 3 the
// separator (with any closing quote of the name), group 4 the value. The
// name may end in _KEY or _ACCESS_KEY (AWS_SECRET_ACCESS_KEY) and nothing
// else, so max_tokens and tokenizer are not names. The value must start on the
// same line; an empty value does not take the next line. A quoted value
// takes adjacent quoted segments with it, as in a shell word like 'it'"'"'s'.
var logSecretName = regexp.MustCompile(`(?i)\b([a-z0-9_.-]*?(password|passwd|secret|api[_-]?key|private[_-]?key|token)(?:[_.-](?:access[_.-]?)?key)?)(["']?[ \t]*[=:][ \t]*)("[^"\n]*"(?:"[^"\n]*")*|'[^'\n]*'(?:'[^'\n]*')*|[^\s,;"'\]})]+)`)

/** ScrubReport describes what ScrubEvent did to a piece of text.
 *
 * Fields:
 *   Redactions (map[string]int) — number of values replaced, by class:
 *                                 pem, authorization, bearer, secret.
 *                                 Never holds a value.
 *   Flags      ([]string)       — bare sensitive words found outside the
 *                                 redacted spans, e.g. `contains "password"`;
 *                                 flagged for review, not redacted.
 *
 * Example:
 *   _, rep := ScrubEvent("API_KEY=abc", "")
 *   fmt.Println(rep.String()) // "secret 1"
 */
type ScrubReport struct {
	Redactions map[string]int
	Flags      []string
}

/** String names the redaction classes and counts, such as "pem 1, secret 2",
 * for the level-debug note the writer appends. It is "" when nothing was
 * redacted, and never contains a value.
 *
 * Returns:
 *   string — classes in alphabetical order with their counts.
 *
 * Example:
 *   _, rep := ScrubEvent("token=a b_secret=c", "")
 *   fmt.Println(rep.String()) // "secret 2"
 */
func (r ScrubReport) String() string {
	classes := make([]string, 0, len(r.Redactions))
	for c, n := range r.Redactions {
		if n > 0 {
			classes = append(classes, c)
		}
	}
	sort.Strings(classes)
	parts := make([]string, len(classes))
	for i, c := range classes {
		parts[i] = fmt.Sprintf("%s %d", c, r.Redactions[c])
	}
	return strings.Join(parts, ", ")
}

/** ScrubEvent prepares the text of a session-log event for writing. It
 * normalises the workspace path to <workspace> and replaces secret-shaped
 * values with [REDACTED:class]: PEM private-key blocks, the value of an
 * Authorization header, a bare "Bearer" token, and the value of NAME=value or
 * NAME: value where NAME is a known secret name. The name and the
 * surrounding text are kept. Bare sensitive words elsewhere are only flagged
 * in the report. Run it before the size cap and before hashing a blob, so the
 * hash is of the scrubbed text (DR-0039).
 *
 * The scrubber is idempotent: scrubbing its own output changes nothing and
 * redacts nothing more. An unusual secret that matches none of the shapes is
 * written as it is; the flags are how the author notices.
 *
 * Parameters:
 *   text      (string) — the payload text to scrub.
 *   workspace (string) — absolute workspace root to replace with
 *                        <workspace>; "" or "/" skips path normalisation.
 *
 * Returns:
 *   string      — the scrubbed text.
 *   ScrubReport — redaction counts by class and the flagged words.
 *
 * Example:
 *   out, rep := ScrubEvent("Authorization: Bearer abc", "")
 *   // out == "Authorization: [REDACTED:authorization]"
 *   // rep.Redactions["authorization"] == 1
 */
func ScrubEvent(text, workspace string) (string, ScrubReport) {
	if workspace != "" && workspace != "/" {
		text = strings.ReplaceAll(text, workspace, "<workspace>")
	}
	out, counts := redactSecrets(text, false)
	blanked, _ := redactSecrets(text, true)
	return out, ScrubReport{Redactions: counts, Flags: flagWords(blanked)}
}

// redactSecrets applies the redaction rules in a fixed order. With blank set
// each whole match is replaced by a space instead of a marker, which gives the
// text the flag scan reads so a redacted span cannot raise a flag.
func redactSecrets(text string, blank bool) (string, map[string]int) {
	counts := map[string]int{}
	text = rewrite(text, logPEMBlock, func(s string, m []int) (string, bool) {
		counts["pem"]++
		return pick(blank, " ", "[REDACTED:pem]"), true
	})
	text = rewrite(text, logAuthorization, func(s string, m []int) (string, bool) {
		if strings.HasPrefix(s[m[4]:m[5]], "[REDACTED:") {
			return s[m[0]:m[1]], false
		}
		counts["authorization"]++
		return pick(blank, " ", s[m[2]:m[3]]+"[REDACTED:authorization]"), true
	})
	text = rewrite(text, logBearer, func(s string, m []int) (string, bool) {
		counts["bearer"]++
		return pick(blank, " ", s[m[2]:m[3]]+"[REDACTED:bearer]"), true
	})
	text = rewrite(text, logSecretName, func(s string, m []int) (string, bool) {
		value := s[m[8]:m[9]]
		if strings.HasPrefix(value, "[REDACTED:") {
			return s[m[0]:m[1]], false
		}
		if strings.EqualFold(s[m[4]:m[5]], "token") {
			// "token" is a name only as a whole word: mytoken=x is not one.
			if prefix := s[m[2]:m[4]]; prefix != "" && !isNameSeparator(prefix[len(prefix)-1]) {
				return s[m[0]:m[1]], false
			}
		}
		counts["secret"]++
		return pick(blank, " ", s[m[2]:m[3]]+s[m[6]:m[7]]+"[REDACTED:secret]"), true
	})
	if blank {
		text = logRedactedMarker.ReplaceAllString(text, " ")
	}
	return text, counts
}

// rewrite replaces each match of re in s with what build returns for it;
// build returns false to leave a match as it is.
func rewrite(s string, re *regexp.Regexp, build func(s string, m []int) (string, bool)) string {
	var b strings.Builder
	last := 0
	for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
		repl, ok := build(s, m)
		if !ok {
			continue
		}
		b.WriteString(s[last:m[0]])
		b.WriteString(repl)
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

func pick(blank bool, whenBlank, otherwise string) string {
	if blank {
		return whenBlank
	}
	return otherwise
}

func isNameSeparator(c byte) bool { return c == '_' || c == '.' || c == '-' }

// flagWords lists the bare sensitive words found in text, in the order of
// logFlagWords, each once.
func flagWords(text string) []string {
	lower := strings.ToLower(text)
	var flags []string
	seen := map[string]bool{}
	for _, w := range logFlagWords {
		key := strings.ToLower(w)
		if seen[key] {
			continue
		}
		if strings.Contains(lower, key) {
			flags = append(flags, fmt.Sprintf("contains %q", w))
			seen[key] = true
		}
	}
	return flags
}
