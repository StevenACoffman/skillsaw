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
# yolo, not plan, and not default. See the note above capture() for why an ordering harness
# needs an agent that can act, and what that admits.
APPROVAL="yolo"
# Empty means uncontained, which this refuses to do by accident -- see the check below.
# macOS has seatbelt built in, so there is a working default on the platform this runs on.
SANDBOX=""
if [ "$(uname -s)" = "Darwin" ]; then SANDBOX="sandbox-exec"; fi

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
  --approval-mode MODE
                 passed to gemini; default "yolo". The agent must be able to act for the
                 ordering question to have an answer, and "plan" withholds activate_skill
                 and denies the shell fallback, so under it a skill outside the workspace
                 cannot be loaded at all. Pass "plan" to reproduce runs captured before
                 2026-08-27; the two are not comparable.
  --sandbox MODE passed to gemini as GEMINI_SANDBOX; default "sandbox-exec" on macOS.
                 Under yolo the agent can act, and an uncontained one has been observed
                 editing this script mid-run and re-invoking it. Pass "none" to run
                 uncontained, deliberately. Do not use a strict seatbelt profile: those
                 restrict reads too, and the skill being measured lives outside the
                 workspace, so the agent could not load it and every run would report
                 unmeasurable.
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
        --approval-mode) APPROVAL="$2"; shift 2 ;;
        --sandbox)     SANDBOX="$2"; shift 2 ;;
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
# Refusing rather than warning. An uncontained run under yolo rewrote this script's own
# names() to return nothing and then re-invoked the harness; the running instance had
# already parsed the function, so the output looked normal and the sabotage sat on disk.
# A warning would have scrolled past exactly like the stale-binary line did.
if [ -z "$TRANSCRIPTS" ] && [ -z "$SANDBOX" ]; then
    echo "run.sh: no sandbox available and none requested. The agent runs with your" >&2
    echo "privileges and has been observed editing this script while being measured by it." >&2
    echo "Pass --sandbox MODE (docker, podman, sandbox-exec), or --sandbox none to accept" >&2
    echo "that deliberately." >&2
    exit 2
fi

# A named, predictable directory rather than mktemp -d. The problem was never that the
# temp directory got reaped -- it survives until reboot -- but that its path was unguessable,
# so a run was findable only by scrolling back to the line that printed it, and a second run
# of the same subject landed somewhere unrelated. Four findings in three days were blocked
# on transcripts that had been captured and then could not be found again. Runs accumulate
# here on purpose: the transcripts are the evidence, and this is not a scratch directory.
OUT="${OUT:-${SKILLSAW_RUNS:-$HOME/.cache/skillsaw/runs}/$SKILL-$(date +%Y%m%d-%H%M%S)}"
mkdir -p "$OUT/prompts" "$OUT/transcripts"

# mtime PATH — the file's modification time, or nothing if it cannot be read.
#
# BSD and GNU stat disagree on the flag, so both are tried. A failure here prints an empty
# date rather than an error: provenance that cannot be gathered must not take down the run
# it was added to describe.
mtime() {
    stat -f '%Sm' -t '%Y-%m-%d %H:%M' "$1" 2>/dev/null \
        || date -r "$(stat -c '%Y' "$1" 2>/dev/null)" '+%Y-%m-%d %H:%M' 2>/dev/null \
        || true
}

# provenance — the instruments this run was measured with.
#
# A run scored by an Aug 24 binary three days after two scoring fixes landed reported
# numbers that changed when the same transcripts were re-scored, and nothing in the output
# said which skillsaw had produced them. A measurement whose instrument is unrecorded cannot
# be compared with another one.
#
# The binary's mtime is here because "skillsaw version" is not enough on its own: BuildDate
# comes from the module pseudo-version rather than the build, so two builds days apart from
# the same dirty tree print identical version blocks. The mtime is the field that differs.
provenance() {
    echo "skill:      $SKILL"
    local bin
    bin="$(command -v skillsaw)"
    echo "scorer:     $bin"
    local ver
    ver="$(skillsaw version 2>/dev/null | awk '/^GitVersion:/ {print $2}' || true)"
    local tree
    tree="$(skillsaw version 2>/dev/null | awk '/^GitTreeState:/ {print $2}' || true)"
    echo "            ${ver:-unknown} built $(mtime "$bin")"
    if [ "$tree" = "dirty" ]; then
        echo "            (tree was dirty at build; this scorer is not reproducible)"
    fi
    # Only when the agent actually ran. Scoring a captured run does not involve gemini, and
    # naming a version that took no part in the measurement is worse than naming none.
    #
    # The approval mode is recorded beside the version because it decides what the run is
    # able to observe, not merely how it behaves: under plan the skill cannot be loaded at
    # all. Two runs of one subject under different modes are not comparable, and a reader
    # holding two reports needs to be able to see that without being told.
    if [ -z "$TRANSCRIPTS" ]; then
        echo "agent:      gemini $(gemini --version 2>/dev/null | head -1 || true)"
        echo "            --approval-mode $APPROVAL, sandbox $SANDBOX"
    fi
}

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
# is where the skill lives. --skip-trust is what keeps the run from stopping on an untrusted
# directory.
#
# The approval mode defaults to yolo, and this ran under plan until 2026-08-27. Plan mode
# withholds activate_skill from the tool set and denies the shell fallback as script
# execution, so a skill living outside the workspace could not be reached at all: across 27
# captured runs the agent asked for it and was refused in 20 of them, and every "first"
# verdict the corpus had produced was awarded for a failed read. Plan mode was not only
# blocking the skill; it was blocking the failure mode, since an agent that cannot act
# cannot be observed acting before it loads, which is the one thing measured here.
#
# What that admits, stated rather than left to be discovered: the agent can write, edit and
# run shell commands with the operator's privileges. Containment is this function's fresh
# working directory and the timeout below -- neither of which confines an agent that uses
# an absolute path. That is an accepted cost of the measurement, not an oversight. Pass
# --approval-mode plan to get the old behaviour back.
capture() {
    local name="$1" run="${2:-}" work out
    out="$OUT/transcripts/$name${run:+.$run}"
    work="$(mktemp -d)"
    local args=(-p "$(cat "$OUT/prompts/$name.txt")"
                --output-format stream-json
                --skip-trust
                --approval-mode "$APPROVAL")
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
    # GEMINI_SANDBOX rather than -s, because -s is a boolean and the mode has to be
    # nameable. "none" means the operator asked for no containment and gets none.
    local env_sandbox="$SANDBOX"
    if [ "$env_sandbox" = "none" ]; then env_sandbox=""; fi
    (cd "$work" && GEMINI_SANDBOX="$env_sandbox" timeout 300 gemini "${args[@]}") \
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

provenance > "$OUT/provenance.txt"
cat "$OUT/provenance.txt"
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
    # The tally line above counts verdicts; this one names their cause, which is the part
    # that says what to change. Failing to print it would leave the advice below pointing
    # at doors the reader cannot see.
    skillsaw ordering --skill "$SKILL" --agreement "${CONTROL_FILES[@]}" 2>/dev/null || true
    # Two very different causes wear the same failure, and the advice for one is actively
    # misleading for the other. A control that came back "unmeasurable" means the agent
    # asked for the skill and could not reach it: telling the author to fix the trigger
    # sends them to rewrite a description that was never read. This ran for three days
    # against a corpus in which 20 of 27 runs never loaded the skill.
    case "$CONTROL_SEEN" in
        *unmeasurable*)
            echo "This is not a finding about the skill. The agent asked for it and every"
            echo "attempt to reach it was refused — the run above names which."
            echo "Nothing measured under these conditions is interpretable, including a"
            echo "phrasing that passed."
            echo
            # Naming the mode rather than assuming one. Under plan the cause is known and
            # the fix is this flag; under anything else the flag has already been ruled out
            # and guessing further would send the reader down the path just eliminated.
            #
            # Scoring captured transcripts is the third case and the easiest to get wrong:
            # --approval-mode describes this invocation, which ran no agent, and says
            # nothing about the run that produced the files. Claiming otherwise would be
            # confidently wrong for every transcript captured before 2026-08-27.
            if [ -n "$TRANSCRIPTS" ]; then
                echo "These transcripts were captured elsewhere, so this run's"
                echo "--approval-mode says nothing about what produced them. Check the"
                echo "provenance.txt beside them, if the run that wrote them left one."
            elif [ "$APPROVAL" = "plan" ]; then
                echo "The mode was --approval-mode plan, which withholds activate_skill from"
                echo "the tool set and denies the shell fallback as script execution. Re-run"
                echo "without it; yolo is the default for this reason."
            else
                echo "The mode was --approval-mode $APPROVAL, so approval is not the cause."
                echo "The remaining boundary is the workspace: skills under ~/.agents/skills"
                echo "sit outside it, and read_file refuses paths that do. Check the stderr"
                echo "beside the transcript for which tool said what, and consider"
                echo "--include-directories on the skills root if read_file is the refusal."
            fi
            ;;
        *unreadable*)
            echo "Nothing is wrong with the skill: the control's transcripts could not be"
            echo "parsed, so nobody scored them. The usual cause is a single event larger"
            echo "than skillsaw's line cap — an agent that greps a large tree returns the"
            echo "matches, and one tool result has reached 17MB. Check the transcript sizes"
            echo "under the output directory before concluding anything about the skill."
            ;;
        *)
            echo "The other eight are not interpretable: a failure under pressure cannot be told"
            echo "apart from a skill that does not reliably answer to its own name. Fix the"
            echo "trigger first, or repeat the control until it is unanimous."
            ;;
    esac
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
    # Not "started work before loading the skill". FAILED is set by any verdict that is not
    # "first", and three of the four are not that: unmeasurable means the runtime refused
    # every attempt to reach the skill, unreadable means nobody scored the transcript, and
    # not-triggered means the skill was never referred to. Naming the wrong one sends the
    # reader to the wrong fix -- which is the whole reason those verdicts were separated.
    echo "at least one phrasing did not load the skill first. The rows above say which and"
    echo "why; only \"after-action\" means work began before the skill loaded."
fi
echo "output: $OUT"
exit "$FAILED"
