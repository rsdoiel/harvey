// Package harvey — skill_dispatch.go handles runtime activation of skills.
// A skill is used directly: activating it puts its SKILL.md body into the
// conversation and the model answers with that guidance in context.
//
//   - Trigger matching (MatchesTrigger) via regex or keyword patterns
//   - Skill lookup in the catalog (SortedSkillNames for deterministic ordering)
//   - Automatic activation (autoLoadSkill) of the first skill whose trigger matches
//
// Skills are activated automatically by their Trigger pattern, or explicitly
// with /<skill-name> or /skill load NAME. Skills are no longer compiled to Bash
// or PowerShell (workspace DR-0008).

package harvey

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

/** MatchesTrigger reports whether prompt matches the trigger pattern stored in
 * skill.Trigger. Returns false when skill.Trigger is empty.
 *
 * Two trigger modes are supported:
 *   Regexp mode  — trigger value is wrapped in slashes: /pattern/
 *                  Matched case-insensitively via (?i).
 *   Keyword mode — trigger value is a whitespace-separated list of words.
 *                  Any single word present in the lowercased prompt triggers.
 *
 * A malformed regexp returns false rather than panicking.
 *
 * Parameters:
 *   skill  (*SkillMeta) — skill with optional Trigger field.
 *   prompt (string)     — raw user input to match against.
 *
 * Returns:
 *   bool — true when prompt matches the trigger.
 *
 * Example:
 *   skill.Trigger = "pdf extract"
 *   MatchesTrigger(skill, "please extract the pdf") // true
 */
func MatchesTrigger(skill *SkillMeta, prompt string) bool {
	if skill.Trigger == "" {
		return false
	}
	lower := strings.ToLower(prompt)
	t := strings.TrimSpace(skill.Trigger)

	if strings.HasPrefix(t, "/") {
		// Support /pattern/ and /pattern/flags — find the closing slash.
		lastSlash := strings.LastIndex(t[1:], "/")
		if lastSlash >= 0 {
			pattern := t[1 : lastSlash+1]
			re, err := regexp.Compile("(?i)" + pattern)
			if err != nil {
				return false
			}
			return re.MatchString(lower)
		}
	}

	// Build a single word-boundary regex from all keywords so that short words
	// like "at" don't fire on substrings like "data" inside a filename.
	var parts []string
	for _, kw := range strings.Fields(strings.ToLower(t)) {
		parts = append(parts, regexp.QuoteMeta(kw))
	}
	if len(parts) == 0 {
		return false
	}
	re, err := regexp.Compile(`(?i)\b(` + strings.Join(parts, "|") + `)\b`)
	if err != nil {
		return false
	}
	return re.MatchString(lower)
}

/** SortedSkillNames returns the skill names from cat in sorted order.
 * Used to provide deterministic first-match semantics when scanning triggers.
 *
 * Parameters:
 *   cat (SkillCatalog) — the catalog to sort.
 *
 * Returns:
 *   []string — skill names in ascending alphabetical order.
 *
 * Example:
 *   for _, name := range SortedSkillNames(a.Skills) {
 *       if MatchesTrigger(a.Skills[name], input) { ... }
 *   }
 */
func SortedSkillNames(cat SkillCatalog) []string {
	names := make([]string, 0, len(cat))
	for n := range cat {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

/** autoLoadSkill loads the first skill, in name order, whose trigger matches
 * input into the conversation.
 *
 * Parameters:
 *   input (string)    — the user's prompt text.
 *   out   (io.Writer) — destination for status messages.
 *
 * Returns:
 *   bool — true when a skill matched and was loaded; the caller still sends
 *          input to the model, which answers with the skill in context.
 *
 * Example:
 *   if a.autoLoadSkill("please extract the pdf", os.Stdout) { ... }
 */
func (a *Agent) autoLoadSkill(input string, out io.Writer) bool {
	for _, name := range SortedSkillNames(a.Skills) {
		skill := a.Skills[name]
		if !MatchesTrigger(skill, input) {
			continue
		}
		fmt.Fprintf(out, dim("  (trigger matched skill %q)\n"), name)
		if err := loadSkillAs(a, skill, out, "auto"); err != nil {
			fmt.Fprintf(out, red("  ✗ skill load error: ")+"%v\n", err)
		}
		return true
	}
	return false
}
