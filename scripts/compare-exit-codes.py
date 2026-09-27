# /// script
# requires-python = ">=3.10"
# ///
"""Compare two builds of harvey/assay against the exit-codes-survey.md baseline.

This is H6 of exit-codes-plan.md; harvey/decisions/0005-*.md cites it. Modelled on
knowledge's scripts/compare-exit-codes.py (DR-0040/DR-0049), adapted for two binaries
with a fixed, hand-picked command list instead of one binary with a SYNOPSIS to walk.

    go build -o bin/harvey ./cmd/harvey && go build -o bin/assay ./cmd/assay
    uv run scripts/compare-exit-codes.py --base ~/tmp/harvey-v0.0.16 --new bin \\
        --work ~/Laboratory/tmp/hx

--base and --new each name a directory holding a `harvey` binary and an `assay` binary
(a release build vs. the tree under test). Every command below runs against both, in a
fresh copy of a scratch workspace per run: an isolated HOME, stdin from /dev/null, and
Ollama/llama.cpp pointed at a closed local port, so nothing here can reach a real
backend. The command list is exit-codes-survey.md's baseline table translated into
arguments and fixtures; a few survey rows need a live model response or a running
Ollama with zero registered models and are not reproducible without one, so they are
left out here and noted in the report footer for the reviewer to check by reading the
code instead.

The report lists every command whose exit code changed old to new, then any exit 70 (an
error nothing classified) or hang in the new binaries. Reading the changed codes against
harvey/decisions/0005-*.md is the reviewer's job: nothing here decides a change is right.

One trap, the same one knowledge's harness names: the run must never let a bad flag or
an unclosed stdin start a real `ollama serve` or `llama-server`. The port handed to
`--ollama`/`--llamacpp` is opened and immediately closed, so the connection is always
refused, and the run checks `pgrep -x ollama` before and after and warns if it changed.
"""

import argparse
import os
import shutil
import socket
import subprocess
import sys
import tempfile

NOT_COVERED = [
    "harvey: a session that starts cleanly and ends via /exit or Ctrl-D (needs a live REPL turn)",
    "harvey: a runtime error out of Agent.Run's later stages (needs a reachable backend that then fails mid-session)",
    "assay: Ollama reachable but registered with zero models (needs a real, empty Ollama instance)",
    "assay: a prompt's automatic checks fail (needs a real model response to check)",
    "assay: report.md/results.json cannot be written, isolated from a model-call failure "
    "(closed-port Ollama fails every call first, masking the write failure's own code; "
    "read cmd/assay/main.go:795,803 instead)",
]


def get_closed_port():
    """A local TCP port nothing is listening on: bind, learn the number, close it."""
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


def pgrep_ollama():
    p = subprocess.run(["pgrep", "-x", "ollama"], capture_output=True, text=True)
    return set(p.stdout.split())


# ─── fixtures ──────────────────────────────────────────────────────────────


def build_template(work):
    """A clean scratch workspace, plus the fixed inputs the command list reuses."""
    template = os.path.join(work, "template")
    shutil.rmtree(template, ignore_errors=True)
    os.makedirs(template)
    with open(os.path.join(template, "bad.yaml"), "w") as f:
        f.write("aliases: [this is not: valid: yaml\n")
    with open(os.path.join(template, "notadir.txt"), "w") as f:
        f.write("a file, not a directory\n")
    with open(os.path.join(template, "bad.spmd"), "w") as f:
        f.write("not a fountain session, just text\n")
    with open(os.path.join(template, "corpus.yaml"), "w") as f:
        f.write(
            "version: \"1\"\n"
            "description: fixture corpus for compare-exit-codes.py\n"
            "prompts:\n"
            "  - id: p1\n"
            "    category: fixture\n"
            "    description: a trivial prompt\n"
            "    language: text\n"
            "    prompt: say hello\n"
            "    checks:\n"
            "      contains: [\"hello\"]\n"
            "    human: []\n"
            "    notes: \"\"\n"
        )
    with open(os.path.join(template, "empty-corpus.yaml"), "w") as f:
        f.write("version: \"1\"\ndescription: no prompts\nprompts: []\n")
    with open(os.path.join(template, "bad-corpus.yaml"), "w") as f:
        f.write("prompts: [this is not: valid: yaml\n")
    outside = os.path.join(work, "outside")
    os.makedirs(outside, exist_ok=True)
    return template, outside


def seed_malformed_harvey_yaml(root):
    agents = os.path.join(root, "agents")
    os.makedirs(agents, exist_ok=True)
    with open(os.path.join(agents, "harvey.yaml"), "w") as f:
        f.write("ollama: [this is not: valid: yaml\n")


def seed_extracted_as_file(root):
    """Pre-make out/extracted as a plain file, so MkdirAll(out/extracted/MODEL) fails."""
    out = os.path.join(root, "out")
    os.makedirs(out, exist_ok=True)
    with open(os.path.join(out, "extracted"), "w") as f:
        f.write("blocking file\n")


def seed_report_md_as_dir(root):
    """Pre-make out/report.md as a directory, so WriteFile(out/report.md) fails."""
    out = os.path.join(root, "out")
    os.makedirs(os.path.join(out, "report.md"), exist_ok=True)


# ─── the command list (exit-codes-survey.md's baseline, by row) ────────────


def commands(closed, closed2, outside):
    cmds = []

    def h(name, args, setup=None):
        cmds.append({"prog": "harvey", "name": "[harvey] " + name, "args": args, "setup": setup})

    def a(name, args, setup=None):
        cmds.append({"prog": "assay", "name": "[assay] " + name, "args": args, "setup": setup})

    # harvey — exit-codes-survey.md "### harvey"
    h("--version", ["--version"])
    h("--help", ["--help"])
    h("help", ["help"])
    h("-l", ["-l"])
    h("help nosuchtopic", ["help", "nosuchtopic"])
    h("--bogus", ["--bogus"])
    h("-m with no argument", ["-m"])
    h("init with no argument", ["init"])
    h("init /nonexistent", ["init", "/nonexistent/zzz-harvey-hx"])
    h("init bad.yaml (malformed)", ["init", "bad.yaml"])
    h("-w /nonexistent", ["-w", "/nonexistent/zzz-harvey-hx-dir"])
    h("-w DIR outside cwd", ["-w", outside])
    h("-w FILE (not a directory)", ["-w", "notadir.txt"])
    h("--replay /nonexistent", ["--replay", "/nonexistent/zzz.spmd"])
    h("--replay malformed, no backend", ["--replay", "bad.spmd", "--ollama", closed])
    h("--continue /nonexistent", ["--continue", "/nonexistent/zzz-session.spmd"])
    h("--llamafile /nonexistent", ["--llamafile", "/nonexistent/zzz.llamafile"])
    h("--record-file uncreatable", ["--record-file", "/proc/nope/x", "--ollama", closed])
    h("harvey.yaml malformed", ["--ollama", closed], setup=seed_malformed_harvey_yaml)
    h("no backend reachable, stdin closed", ["--ollama", closed])
    h("-m nosuch, no server", ["-m", "nosuch", "--ollama", closed])

    # assay — exit-codes-survey.md "### assay"
    a("--version", ["--version"])
    a("--help", ["--help"])
    a("--bogus", ["--bogus"])
    a("--rag-compare without --rag-db", ["--rag-compare"])
    a("--guide-compare without --guide-file", ["--guide-compare"])
    a(
        "--guide-compare with --rag-compare",
        ["--guide-compare", "--rag-compare", "--rag-db", "x.db", "--guide-file", "g.txt"],
    )
    a("--llamafile with --llamacpp", ["--llamafile", "dummy.llamafile", "--llamacpp", "http://127.0.0.1:" + str(closed2)])
    a("--corpus missing", ["--corpus", "/nonexistent/zzz-corpus.yaml"])
    a("--corpus malformed", ["--corpus", "bad-corpus.yaml"])
    a("--corpus with no prompts", ["--corpus", "empty-corpus.yaml", "--models", "fake-model"])
    a(
        "--category matching nothing",
        ["--corpus", "corpus.yaml", "--models", "fake-model", "--category", "zzznotfoundzzz"],
    )
    a("Ollama unreachable while listing models", ["--corpus", "corpus.yaml", "--ollama", closed])
    a(
        "--llamacpp URL unreachable",
        ["--corpus", "corpus.yaml", "--llamacpp", "http://127.0.0.1:" + str(closed2)],
    )
    a(
        "--llamafile file missing",
        ["--corpus", "corpus.yaml", "--llamafile", "/nonexistent/zzz.llamafile"],
    )
    a(
        "--rag-db cannot be opened",
        ["--corpus", "corpus.yaml", "--models", "fake-model", "--rag-db", "/nonexistent/dir/rag.db"],
    )
    a(
        "--guide-file missing",
        ["--corpus", "corpus.yaml", "--models", "fake-model", "--guide-file", "/nonexistent/guide.txt", "--output", "out"],
    )
    a(
        "--output cannot be created",
        ["--corpus", "corpus.yaml", "--models", "fake-model", "--output", "/proc/nope/x"],
    )
    a(
        "every model call fails (server down)",
        ["--corpus", "corpus.yaml", "--models", "fake-model", "--ollama", closed, "--output", "out"],
    )
    a(
        "mkdir extracted fails",
        ["--corpus", "corpus.yaml", "--models", "fake-model", "--ollama", closed, "--output", "out"],
        setup=seed_extracted_as_file,
    )
    return cmds


# ─── running ─────────────────────────────────────────────────────────────────


def run_one(binaries_dir, cmd, template, work, timeout):
    root = os.path.join(work, "run")
    shutil.rmtree(root, ignore_errors=True)
    shutil.copytree(template, root)
    if cmd["setup"]:
        cmd["setup"](root)
    prog = os.path.join(binaries_dir, cmd["prog"])
    env = dict(os.environ)
    env["HOME"] = root
    env.pop("OLLAMA_HOST", None)
    try:
        p = subprocess.run(
            [prog] + cmd["args"],
            cwd=root,
            capture_output=True,
            text=True,
            stdin=subprocess.DEVNULL,
            timeout=timeout,
            env=env,
        )
        return p.returncode, p.stdout.replace(root, "W"), p.stderr.replace(root, "W")
    except subprocess.TimeoutExpired:
        return -1, "", "TIMEOUT"
    except FileNotFoundError:
        return -2, "", f"no binary at {prog}"


# ─── report ──────────────────────────────────────────────────────────────────


def report(results):
    print(f"commands: {len(results)}")
    changed = [r for r in results if r["old"][0] != r["new"][0]]
    print(f"\n== exit code changed (old -> new): {len(changed)}")
    for r in changed:
        print(f"  {r['old'][0]:>3} -> {r['new'][0]:>3}  {r['name']}")
    same_diff = [r for r in results if r["old"][0] == r["new"][0] and r["old"][1:] != r["new"][1:]]
    print(f"\n== same exit code, stdout/stderr differs: {len(same_diff)}")
    for r in same_diff:
        print(f"  {r['name']}")
        print(f"     old: {r['old'][1][:120]!r} / {r['old'][2][:120]!r}")
        print(f"     new: {r['new'][1][:120]!r} / {r['new'][2][:120]!r}")
    bad = [(r["name"], r["new"][0]) for r in results if r["new"][0] in (70, -1, -2)]
    print("\n== 70 (unclassified), timeout, or missing binary in the new build:")
    print("  none" if not bad else "\n".join(f"  {code}  {name}" for name, code in bad))
    print("\n== not covered by this harness (check by reading the code):")
    for line in NOT_COVERED:
        print(f"  - {line}")
    return 1 if bad else 0


def main():
    ap = argparse.ArgumentParser(description="Compare two harvey/assay builds against the exit-codes-survey.md baseline.")
    ap.add_argument("--base", required=True, help="directory holding the older harvey and assay binaries")
    ap.add_argument("--new", required=True, help="directory holding the harvey and assay binaries under test")
    ap.add_argument("--work", help="scratch directory (default: a temporary one, removed afterwards)")
    ap.add_argument("--timeout", type=int, default=15, help="seconds per command (default 15)")
    a = ap.parse_args()

    base, new = os.path.abspath(a.base), os.path.abspath(a.new)
    work = os.path.abspath(a.work) if a.work else tempfile.mkdtemp(prefix="harvey-compare-")
    os.makedirs(work, exist_ok=True)

    before = pgrep_ollama()
    closed, closed2 = get_closed_port(), get_closed_port()
    template, outside = build_template(work)
    cmds = commands("http://127.0.0.1:" + str(closed), closed2, outside)

    results = []
    for cmd in cmds:
        results.append({
            "name": cmd["name"],
            "old": run_one(base, cmd, template, os.path.join(work, "old"), a.timeout),
            "new": run_one(new, cmd, template, os.path.join(work, "new"), a.timeout),
        })

    code = report(results)
    after = pgrep_ollama()
    if after != before:
        print(f"\nWARNING: the ollama process set changed during the run ({before} -> {after}); "
              "the harness should never start or stop a real server.")
        code = 1
    if not a.work:
        shutil.rmtree(work, ignore_errors=True)
    return code


if __name__ == "__main__":
    sys.exit(main())
