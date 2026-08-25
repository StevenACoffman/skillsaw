#!/usr/bin/env bash
# Run the adversarial phrasing corpus against one skill and score the ordering.
#
# Nine phrasings of the same request, differing only in how hard each one pushes the agent
# to start working before it loads the skill. What is measured is not whether the skill
# triggered -- "activation" answers that from vocabulary -- but whether anything happened
# first, which only the event log can answer.
#
# Scoring is delegated to "skillsaw ordering" rather than re-grepped here. A second,
# line-oriented approximation of "before" would drift from the real one.
#
# The agent is Gemini CLI. It has no dedicated skill tool -- it loads a skill by reading the
# SKILL.md -- so "did the skill load" is a file read of that path, which skillsaw's
# transcript reader recognises. Nothing here depends on that; the same runner scores a
# Claude Code capture passed to --transcripts.
set -euo pipefail

# has_cmd NAME — true if NAME is an executable file on $PATH.
# Ignores shell functions, aliases, and builtins of the same name.
has_cmd() {
    if [ -n "${ZSH_VERSION:-}" ]; then
        builtin whence -p -- "$1" >/dev/null 2>&1
    elif [ -n "${BASH_VERSION:-}" ]; then
        builtin type -P -- "$1" >/dev/null 2>&1
    else
        command -v -- "$1" >/dev/null 2>&1
    fi
}

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROMPTS="$HERE/prompts"

SKILL=""
TASK="I've got a change ready to work through."
EXTENSIONS=""
TRANSCRIPTS=""
OUT=""
MODEL=""
REPEAT=1

usage() {
    cat <<'USAGE'
usage:
  run.sh --skill NAME [--task TEXT] [--extensions LIST] [--model M] [--out DIR]
  run.sh --skill NAME --transcripts DIR

  --skill        the skill the prompts request, by its declared name (required)
  --task         the work the request is about; substituted for {{TASK}}
  --extensions   passed to gemini -e, if the skill ships in an extension
  --model        passed to gemini -m; default is whatever gemini decides
  --out          where to write prompts and transcripts (default: a temp dir)
  --repeat N     run each phrasing N times and report whether the runs agree. A phrasing
                 whose runs disagree is one the skill does not reliably survive, which a
                 single run cannot distinguish from one it does.
  --transcripts  score transcripts already captured in DIR and do not run the agent

Run "gemini skills list" to see the names available to request.

Files in --transcripts are matched to prompts by name: 01-bare.json scores as 01-bare.
USAGE
}

while [ $# -gt 0 ]; do
    case "$1" in
        --skill)       SKILL="$2"; shift 2 ;;
        --task)        TASK="$2"; shift 2 ;;
        --extensions)  EXTENSIONS="$2"; shift 2 ;;
        --model)       MODEL="$2"; shift 2 ;;
        --repeat)      REPEAT="$2"; shift 2 ;;
        --transcripts) TRANSCRIPTS="$2"; shift 2 ;;
        --out)         OUT="$2"; shift 2 ;;
        -h|--help)     usage; exit 0 ;;
        *) echo "run.sh: unknown argument $1" >&2; usage >&2; exit 2 ;;
    esac
done

if [ -z "$SKILL" ]; then
    echo "run.sh: --skill is required; there is nothing to ask for without it" >&2
    exit 2
fi
if ! has_cmd skillsaw; then
    echo "run.sh: skillsaw is not on PATH; it does the scoring" >&2
    exit 2
fi
if [ -z "$TRANSCRIPTS" ] && ! has_cmd gemini; then
    echo "run.sh: gemini is not on PATH. Pass --transcripts DIR to score a captured run." >&2
    exit 2
fi

OUT="${OUT:-$(mktemp -d)}"
mkdir -p "$OUT/prompts" "$OUT/transcripts"

# render NAME — substitute the placeholders and drop the leading '#' description, which
# describes the pressure shape to a reader and must not reach the agent.
render() {
    sed -e "s|{{SKILL}}|$SKILL|g" -e "s|{{TASK}}|$TASK|g" -e '/^#/d' "$PROMPTS/$1.txt" \
        > "$OUT/prompts/$1.txt"
}

# capture NAME [RUN] — run the agent on one prompt and leave the event log in
# $OUT/transcripts/NAME.json, or NAME.RUN.json when repeating. skillsaw groups repeated
# runs by the stem before the first dot.
#
# Each run happens in its own empty working directory, so one phrasing cannot see what the
# last one wrote. HOME is deliberately *not* isolated: Gemini CLI discovers skills under
# $HOME/.gemini/skills, so a fresh HOME would hide the skill being asked for and every run
# would report not-triggered. That is a real departure from the reference harness, which
# isolates HOME precisely to keep the operator's context out -- here the operator's context
# is where the skill lives. --skip-trust is what keeps the run from stopping on an
# untrusted directory, and plan mode keeps it read-only.
capture() {
    local name="$1" run="${2:-}" work out
    out="$OUT/transcripts/$name${run:+.$run}"
    work="$(mktemp -d)"
    local args=(-p "$(cat "$OUT/prompts/$name.txt")"
                --output-format stream-json
                --skip-trust
                --approval-mode plan)
    if [ -n "$EXTENSIONS" ]; then args+=(-e "$EXTENSIONS"); fi
    if [ -n "$MODEL" ]; then args+=(-m "$MODEL"); fi
    # stdin from /dev/null: gemini appends stdin to --prompt "if any", so a terminal on
    # stdin leaves it waiting for an EOF that an interactive shell never sends. Closing it
    # is what makes the invocation headless in fact and not just by flag.
    #
    # stderr to its own file rather than into the transcript: gemini writes skill-conflict
    # banners there, and mixing them into the event log means a run that produced no events
    # still leaves a plausible-looking file. Kept, not discarded -- it is the only evidence
    # when a run goes wrong.
    (cd "$work" && timeout 300 gemini "${args[@]}") \
        < /dev/null \
        > "$out.json" \
        2> "$out.stderr" || true
}

# verdict FILE — the one-word ordering outcome for a captured transcript.
#
# "skillsaw ordering" exits non-zero for any verdict other than "first", which is the
# outcome this corpus is hunting for. Under `set -o pipefail` that exit would propagate into
# the assignment and kill the run at the first interesting result, so it is swallowed here
# and the verdict itself is what the caller reads.
verdict() {
    local f="$1" out
    out="$(skillsaw ordering --skill "$SKILL" --json "$f" 2>/dev/null || true)"
    printf '%s' "$out" | sed -n 's/.*"order": "\([^"]*\)".*/\1/p' | head -1
}

names() { find "$PROMPTS" -name '*.txt' -exec basename {} .txt \; | sort; }

transcript_for() {
    if [ -n "$TRANSCRIPTS" ]; then echo "$TRANSCRIPTS/$1.json"; else echo "$OUT/transcripts/$1.json"; fi
}

echo "skill:      $SKILL"
echo "transcripts: ${TRANSCRIPTS:-$OUT/transcripts}"
echo

# The control runs first and gates the rest. If the bare request does not load the skill,
# the adversarial phrasings failing tells you the skill does not answer to its own name --
# not that pressure defeated it, which is the only thing this corpus is for.
CONTROL=01-bare
if [ -z "$TRANSCRIPTS" ]; then
    render "$CONTROL"
    # The control is repeated with everything else. It used to run once, and a subject
    # measured at 62% "first" over 24 runs showed why that is not enough: a single control
    # can pass by luck, and a lucky pass silently licenses eight rows that should have been
    # refused.
    if [ "$REPEAT" -le 1 ]; then
        capture "$CONTROL"
    else
        for r in $(seq 1 "$REPEAT"); do capture "$CONTROL" "$r"; done
    fi
fi
CONTROL_SRC="${TRANSCRIPTS:-$OUT/transcripts}"
if [ "$REPEAT" -le 1 ]; then
    CONTROL_FILES=("$(transcript_for "$CONTROL")")
else
    CONTROL_FILES=("$CONTROL_SRC/$CONTROL."*.json)
fi
if [ ! -f "${CONTROL_FILES[0]}" ]; then
    echo "run.sh: no control transcript under $CONTROL_SRC" >&2
    exit 2
fi
# The control must be unanimously "first". A split control is exactly the case where the
# rest cannot be read: it says the skill does not reliably answer to its own name, so a
# phrasing failing under pressure cannot be told apart from one failing for that reason.
CONTROL_RUNS=0
CONTROL_FIRST=0
CONTROL_SEEN=""
for f in "${CONTROL_FILES[@]}"; do
    [ -f "$f" ] || continue
    v="$(verdict "$f")"
    CONTROL_RUNS=$((CONTROL_RUNS + 1))
    if [ "$v" = "first" ]; then
        CONTROL_FIRST=$((CONTROL_FIRST + 1))
    fi
    case "$CONTROL_SEEN" in *"$v"*) ;; *) CONTROL_SEEN="$CONTROL_SEEN $v" ;; esac
done

if [ "$CONTROL_RUNS" -eq 1 ]; then
    printf '%-24s %s (1 run — not repeated)\n' "$CONTROL (control)" "${CONTROL_SEEN# }"
else
    printf '%-24s %d/%d first over %d runs\n' \
        "$CONTROL (control)" "$CONTROL_FIRST" "$CONTROL_RUNS" "$CONTROL_RUNS"
fi

if [ "$CONTROL_FIRST" -ne "$CONTROL_RUNS" ] || [ "$CONTROL_RUNS" -eq 0 ]; then
    echo
    echo "CONTROL DID NOT HOLD — the bare request returned:${CONTROL_SEEN}"
    echo "The other eight are not interpretable: a failure under pressure cannot be told"
    echo "apart from a skill that does not reliably answer to its own name. Fix the"
    echo "trigger first, or repeat the control until it is unanimous."
    exit 1
fi

echo
FAILED=0
for name in $(names); do
    if [ "$name" = "$CONTROL" ]; then continue; fi
    if [ -z "$TRANSCRIPTS" ]; then
        render "$name"
        if [ "$REPEAT" -le 1 ]; then
            capture "$name"
        else
            for r in $(seq 1 "$REPEAT"); do capture "$name" "$r"; done
        fi
    fi
    if [ "$REPEAT" -le 1 ]; then
        f="$(transcript_for "$name")"
        if [ ! -f "$f" ]; then printf '%-24s %s\n' "$name" "no transcript"; FAILED=1; continue; fi
        v="$(verdict "$f")"
        printf '%-24s %s\n' "$name" "$v"
        if [ "$v" != "first" ]; then FAILED=1; fi
        continue
    fi
    # Repeated: agreement is the unit, so skillsaw groups the runs and reports the split.
    src="${TRANSCRIPTS:-$OUT/transcripts}"
    skillsaw ordering --skill "$SKILL" --agreement "$src/$name."*.json 2>/dev/null || FAILED=1
done

echo
if [ "$FAILED" -eq 0 ]; then
    echo "every phrasing loaded the skill before doing anything."
else
    echo "at least one phrasing started work before loading the skill."
fi
echo "output: $OUT"
exit "$FAILED"
