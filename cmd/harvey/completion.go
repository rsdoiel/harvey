package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	harvey "github.com/rsdoiel/harvey"
)

// Shell completion for the harvey binary's own verbs and options. It is
// separate from the in-session /command completion (design/tab-completion-
// design.md). Nothing here is a second hand-kept list: the verbs are the
// non-option cases of the switch in runArgs (completion_test.go reads main.go
// and compares), the options are flagSpecs plus the immediate ones, and the
// help topics are parsed from the topic index. The same design as antenna's
// and kb's completion.

/** completionVerbs lists the verbs harvey accepts as its first argument. Keep
 * it in step with the verb cases in runArgs;
 * TestCompletion_VerbsMatchTheSwitchInMain compares the two.
 *
 * Returns:
 *   []string — verb names, sorted.
 *
 * Example:
 *   fmt.Println(completionVerbs()) // [completion help init]
 */
func completionVerbs() []string {
	return []string{"completion", "help", "init"}
}

const (
	valuePath = "path" // the value is a file or directory name: complete from the file system
	valueFree = "free" // the value is free text (a model name, a URL): offer nothing
)

/** completionValueKind says what each value-taking option's value is, so
 * completion offers files after --replay and nothing after --model.
 * TestCompletion_EveryValueFlagIsClassified fails when an option that takes a
 * value is missing from this table or an entry is not an option.
 */
var completionValueKind = map[string]string{
	"-m": valueFree, "--model": valueFree,
	"--ollama":        valueFree,
	"--llamafile":     valuePath,
	"--llamafile-url": valueFree,
	"--llamafile-dir": valuePath,
	"-w":              valuePath, "--workdir": valuePath,
	"--record-file":   valuePath,
	"--continue":      valuePath,
	"--replay":        valuePath,
	"--replay-output": valuePath,
}

/** completionOptions returns every option completion offers: each spelling in
 * flagSpecs, the immediate options (help, version, license, json), and the
 * single-dash -help that runArgs also accepts.
 *
 * Returns:
 *   []string — option spellings, sorted.
 *
 * Example:
 *   for _, o := range completionOptions() { fmt.Println(o) }
 */
func completionOptions() []string {
	seen := map[string]bool{"-help": true}
	for _, n := range completionImmediateOptions {
		seen[n] = true
	}
	for _, f := range flagSpecs {
		for _, n := range f.names {
			seen[n] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// completionImmediateOptions are the options outside flagSpecs: they print something and
// exit, or (--json) are taken out of the line before flagSpecs sees it.
var completionImmediateOptions = []string{"-h", "--help", "-v", "--version", "-l", "--license", "--json"}

var topicAliasGroup = regexp.MustCompile(`([\w, -]+?)\s*→\s*[\w-]+`)

/** completionHelpTopics returns the names `harvey help` accepts: the topics
 * and aliases in the topic index, and "topics" itself. It reads the index
 * rather than keeping a list, so a new topic is completed when it is added.
 *
 * Returns:
 *   []string — topic names and aliases, sorted.
 *
 * Example:
 *   fmt.Println(completionHelpTopics()) // [alias attach audit ... write]
 */
func completionHelpTopics() []string {
	seen := map[string]bool{"topics": true}
	inAliases := false
	for _, line := range strings.Split(harvey.HelpTopicsText(), "\n") {
		if strings.Contains(line, "Aliases:") {
			inAliases = true
			line = strings.Replace(line, "Aliases:", "        ", 1)
		}
		if !strings.HasPrefix(line, "  ") {
			continue
		}
		if !inAliases {
			if f := strings.Fields(line); len(f) > 0 {
				seen[f[0]] = true
			}
			continue
		}
		for _, g := range topicAliasGroup.FindAllStringSubmatch(line, -1) {
			for _, n := range strings.Split(g[1], ",") {
				if n = strings.TrimSpace(n); n != "" {
					seen[n] = true
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// completionShells are the shells harvey can generate for.
var completionShells = []string{"bash", "powershell"}

// completionInstallFlags are the spellings of the install option.
var completionInstallFlags = []string{"-install", "--install"}

var nonIdent = regexp.MustCompile(`[^A-Za-z0-9_]`)

func flagsOfKind(kind string) []string {
	var out []string
	for n, k := range completionValueKind {
		if k == kind {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func psList(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = "'" + s + "'"
	}
	return strings.Join(q, ", ")
}

/** writeCompletion writes a shell completion script for the named shell.
 * Nothing is written when the shell is not supported.
 *
 * Parameters:
 *   w       (io.Writer) — destination for the script
 *   appName (string)    — binary name to register; a .exe suffix is ignored
 *   shell   (string)    — "bash" or "powershell" (case-insensitive; "pwsh" accepted)
 *
 * Returns:
 *   error — a usage error for an unsupported shell, or the failed write
 *
 * Example:
 *   err := writeCompletion(os.Stdout, "harvey", "bash")
 */
func writeCompletion(w io.Writer, appName, shell string) error {
	name := strings.TrimSuffix(appName, ".exe")
	var script string
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "bash":
		script = bashCompletion
	case "powershell", "pwsh":
		script = powershellCompletion
	default:
		return harvey.Usagef("unsupported shell %q; use one of: %s", shell, strings.Join(completionShells, ", "))
	}
	pathFlags, freeFlags := flagsOfKind(valuePath), flagsOfKind(valueFree)
	r := strings.NewReplacer(
		"{app_name}", name,
		"{app_func}", nonIdent.ReplaceAllString(name, "_"),
		"{verbs}", strings.Join(completionVerbs(), " "),
		"{options}", strings.Join(completionOptions(), " "),
		"{topics}", strings.Join(completionHelpTopics(), " "),
		"{shells}", strings.Join(completionShells, " "),
		"{install_flags}", strings.Join(completionInstallFlags, " "),
		"{path_flags}", strings.Join(pathFlags, "|"),
		"{free_flags}", strings.Join(freeFlags, "|"),
		"{value_flags}", strings.Join(append(append([]string{}, pathFlags...), freeFlags...), "|"),
		"{ps_verbs}", psList(completionVerbs()),
		"{ps_options}", psList(completionOptions()),
		"{ps_topics}", psList(completionHelpTopics()),
		"{ps_shells}", psList(completionShells),
		"{ps_install_flags}", psList(completionInstallFlags),
		"{ps_path_flags}", psList(pathFlags),
		"{ps_free_flags}", psList(freeFlags),
	)
	_, err := io.WriteString(w, r.Replace(script))
	return err
}

const bashCompletion = `# bash completion for {app_name}
# Load with:  source <({app_name} completion bash)
# Or install: {app_name} completion bash --install
_{app_func}() {
    local cur prev verb i w
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev=""
    (( COMP_CWORD > 0 )) && prev="${COMP_WORDS[COMP_CWORD-1]}"
    COMPREPLY=()
    case "$prev" in
        {path_flags})
            compopt -o filenames 2>/dev/null
            COMPREPLY=( $(compgen -f -- "$cur") )
            return 0 ;;
        {free_flags})
            return 0 ;;
    esac
    # The verb is the first word after the command that is not an option and
    # not the value of one.
    verb=""
    for (( i=1; i<COMP_CWORD; i++ )); do
        w="${COMP_WORDS[i]}"
        case "$w" in
            {value_flags}) (( i++ )) ;;
            -*) ;;
            *) verb="$w"; break ;;
        esac
    done
    case "$verb" in
        "")
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "{options}" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "{verbs}" -- "$cur") )
            fi ;;
        help)
            COMPREPLY=( $(compgen -W "{topics}" -- "$cur") ) ;;
        completion)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "{install_flags}" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "{shells}" -- "$cur") )
            fi ;;
        *)
            compopt -o filenames 2>/dev/null
            COMPREPLY=( $(compgen -f -- "$cur") ) ;;
    esac
    return 0
}
complete -F _{app_func} {app_name}
`

const powershellCompletion = `# PowerShell completion for {app_name}
# Load with:  {app_name} completion powershell | Out-String | Invoke-Expression
# Or install: {app_name} completion powershell --install
Register-ArgumentCompleter -Native -CommandName {app_name},{app_name}.exe -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $verbs        = @({ps_verbs})
    $options      = @({ps_options})
    $topics       = @({ps_topics})
    $shells       = @({ps_shells})
    $installFlags = @({ps_install_flags})
    $pathFlags    = @({ps_path_flags})
    $freeFlags    = @({ps_free_flags})

    function Complete-Files($word) {
        Get-ChildItem -Path "$word*" -ErrorAction SilentlyContinue | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_.Name, $_.Name, 'ProviderItem', $_.Name)
        }
    }

    # Words typed after the command name, not counting the word being completed.
    $words = @($commandAst.CommandElements | Select-Object -Skip 1 | ForEach-Object { $_.ToString() })
    if ($wordToComplete -ne '' -and $words.Count -gt 0) {
        $words = @($words | Select-Object -SkipLast 1)
    }

    $prev = if ($words.Count -gt 0) { $words[-1] } else { '' }
    if ($prev -in $pathFlags) { return Complete-Files $wordToComplete }
    if ($prev -in $freeFlags) { return }

    # The verb is the first word that is not an option or the value of one.
    $verb = $null
    for ($i = 0; $i -lt $words.Count; $i++) {
        if (($words[$i] -in $pathFlags) -or ($words[$i] -in $freeFlags)) { $i++; continue }
        if ($words[$i].StartsWith('-')) { continue }
        $verb = $words[$i]
        break
    }

    $candidates = switch ($verb) {
        $null        { if ($wordToComplete.StartsWith('-')) { $options } else { $verbs } }
        'help'       { $topics }
        'completion' { if ($wordToComplete.StartsWith('-')) { $installFlags } else { $shells } }
        default      { $null }
    }
    if ($null -eq $candidates) { return Complete-Files $wordToComplete }
    $candidates | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`

// completionMarker tags the profile line installCompletion adds, so a second
// install finds it and leaves the profile alone.
const completionMarker = "# harvey completion"

/** installCompletion installs the completion script for a shell so it loads in
 * every new session, and returns the path of the script it wrote.
 *
 * For bash the script goes in the bash-completion user directory
 * ($XDG_DATA_HOME/bash-completion/completions/NAME, or the same under
 * ~/.local/share), which bash-completion loads on demand. An existing file
 * there is replaced only when harvey wrote it; any other file is left alone
 * and the error is a cant_create (exit 73).
 *
 * For PowerShell the script is written beside the profile and the profile gets
 * one line that dot-sources it, added once.
 *
 * Parameters:
 *   appName  (string) — binary name to register; a .exe suffix is ignored
 *   shell    (string) — "bash" or "powershell" ("pwsh" accepted)
 *   home     (string) — the user's home directory
 *   dataHome (string) — $XDG_DATA_HOME, or "" for the default
 *   goos     (string) — runtime.GOOS, which picks the PowerShell profile directory
 *
 * Returns:
 *   string — the installed script path
 *   error  — a usage error for an unsupported shell, a cant_create for a file
 *            that is not harvey's, or the failed write
 *
 * Example:
 *   path, err := installCompletion("harvey", "bash", home, "", "linux")
 */
func installCompletion(appName, shell, home, dataHome, goos string) (string, error) {
	name := strings.TrimSuffix(appName, ".exe")
	var script bytes.Buffer
	if err := writeCompletion(&script, appName, shell); err != nil {
		return "", err
	}
	if strings.ToLower(strings.TrimSpace(shell)) == "bash" {
		if dataHome == "" {
			dataHome = filepath.Join(home, ".local", "share")
		}
		path := filepath.Join(dataHome, "bash-completion", "completions", name)
		if old, err := os.ReadFile(path); err == nil {
			if !strings.HasPrefix(string(old), "# bash completion for ") {
				return "", harvey.CantCreatef("%s exists and was not written by %s; not overwriting it", path, name)
			}
		} else if !os.IsNotExist(err) {
			return "", err
		}
		return path, writeFileIn(path, script.Bytes())
	}
	// powershell, checked by writeCompletion
	profileDir := filepath.Join(home, ".config", "powershell")
	if goos == "windows" {
		profileDir = filepath.Join(home, "Documents", "PowerShell")
	}
	path := filepath.Join(profileDir, name+"-completion.ps1")
	if err := writeFileIn(path, script.Bytes()); err != nil {
		return "", err
	}
	profile := filepath.Join(profileDir, "Microsoft.PowerShell_profile.ps1")
	cur, err := os.ReadFile(profile)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if strings.Contains(string(cur), path) {
		return path, nil
	}
	if len(cur) > 0 && !bytes.HasSuffix(cur, []byte("\n")) {
		cur = append(cur, '\n')
	}
	line := fmt.Sprintf(". '%s' %s\n", path, completionMarker)
	return path, os.WriteFile(profile, append(cur, []byte(line)...), 0o644)
}

// writeFileIn writes data to path, creating its directory first.
func writeFileIn(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
