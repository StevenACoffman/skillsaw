# skillsaw

A deterministic command-line tool for **scoring, diagnosing, and validating agent
"skills"** (`SKILL.md` files in the [Agent Skills](https://agentskills.io) format).

skillsaw is the deterministic core of the [darwin-skill](https://github.com/alchaincyf/darwin-skill)
optimizer, reimplemented in Go. It follows one rule: **do everything that can be
done without a model in ordinary code, and delegate to an LLM only the irreducible
qualitative judgments.** Every command here is fully deterministic — the parts a
model would score are surfaced explicitly rather than guessed.

It draws on two Microsoft Research frameworks for its scoring machinery:

- **SkillLens** — the 3-dimension skill-quality rubric (failure-mode encoding,
  actionable specificity, risk-action blacklist).
- **SkillOpt** — the validation-gate ratchet (`sha256[:16]` identity, strict-`>`
  accept/reject) and the rule-judge operators.

---

## Install

Requires Go 1.26+.

```sh
go install github.com/StevenACoffman/skillsaw@latest
```

Or build from a checkout:

```sh
go build -o skillsaw .
```

---

## Quick start

```sh
# Score a skill against the 9-dimension rubric
skillsaw eval ~/path/to/my-skill

# What should I improve next, and why?
skillsaw diagnose ~/path/to/my-skill

# Fail CI if a skill is bound to a single runtime
skillsaw scan --all
```

```text
$ skillsaw eval ~/skills/darwin-skill
SKILL         DET.SCORE  FULL  RUNTIME  WEAKEST                  NEEDS-JUDGE
darwin-skill  87.7/100   -     warn=5   P0 runtime drift repair  dims 1,2,3,5,7,8

DET.SCORE = deterministic lower bound (NEEDS-JUDGE dims assume a perfect base;
only detectable defects are docked). FULL = total with --scores judge bases.
```

---

## Concepts

A **skill** is a directory containing a `SKILL.md`: YAML frontmatter (`name` +
`description`) followed by a Markdown body, optionally with `references/`,
`scripts/`, `assets/`, and `test-prompts.json`.

skillsaw scores it against a **9-dimension rubric** (weights sum to 100):

| # | Dimension | Weight | Source |
|---|---|---:|---|
| 1 | Frontmatter quality | 7 | judge + deterministic penalties |
| 2 | Workflow clarity | 12 | **judge** |
| 3 | Failure-mode encoding | 12 | **judge** + deterministic penalty |
| 4 | Checkpoint design | 6 | deterministic (marker count) |
| 5 | Actionable specificity | 17 | **judge** + deterministic penalty |
| 6 | Resource integration | 5 | deterministic (link reachability) |
| 7 | Overall architecture | 12 | **judge** + deterministic penalty |
| 8 | Real-world test performance | 23 | **judge** (or rule-judge, see `judge`) |
| 9 | Counter-examples / blacklist | 6 | deterministic (section presence) |

**DET.SCORE** is a lint-style lower bound: it computes the total assuming every
judge dimension is perfect, docking only objectively detectable defects
(banned phrases, missing markers, broken links, runtime-binding wording). Supply
a model's per-dimension scores with `eval --scores` to get the **FULL** total.

---

## Commands

Run `skillsaw <command> -h` for full help on any command. Every flag can also be
set via a `SKILLSAW_`-prefixed environment variable.

### `eval` — score skills against the rubric

```sh
skillsaw eval [FLAGS] [SKILL_DIR ...]
```

| Flag | Meaning |
|---|---|
| `-a, --all` | scan skill roots instead of listing directories |
| `--roots` | comma-separated roots for `--all` (default: `.claude/skills,.cursor/skills,.codex/skills,.agents/skills`) |
| `-s, --scores` | path to judge-supplied per-dimension bases (JSON), enabling the FULL total |
| `-v, --verbose` | show the per-dimension breakdown |
| `--json` | emit evaluations as JSON |

`--scores` takes a JSON object of dimension→base, e.g. `{"1":8,"2":8,"3":7,"5":8,"7":6,"8":7}`.
When it covers every judge dimension, `eval` reports the full rubric total.

### `diagnose` — recommend the next dimension to improve

```sh
skillsaw diagnose SKILL_DIR [SKILL_DIR ...]
```

The deterministic half of an optimization round: names the weakest dimension, its
strategy-library priority (P0–P3), and — for the correlated dim2/3/4 cluster — a
note to inspect all three together. A runtime-neutrality hit forces a P0 target.

```text
$ skillsaw diagnose ~/skills/darwin-skill
darwin-skill
  target:    P0 runtime drift repair  [P0]
  rationale: runtime-neutrality gate hit; must be fixed before any dimension
```

### `scan` — runtime-neutrality red-light scan (CI gate)

```sh
skillsaw scan [FLAGS] [SKILL_DIR ...]
```

Flags wording or paths that bind a skill to a single agent runtime (e.g.
"在 Claude Code", a single-runtime badge, a hard-coded `~/.claude/skills/` path) —
which cause other agents to refuse to install it. **Exit code 1** on any hit, so
it works as a CI gate. Supports `--all`, `--roots`, and `--json`.

### `gate` — keep/revert decision (validation gate)

```sh
skillsaw gate --candidate N --current N [--best N]
```

The ratchet decision: strict `>` at both comparisons — a candidate is accepted
only if it beats the current score, and becomes the new best only if it also beats
the best. **Exit 0 on any accept, 1 on reject**, so a script can branch on it.

```text
$ skillsaw gate --candidate 88 --current 84 --best 84
accept_new_best
  current -> 88.0
  best    -> 88.0 (step 0)
```

### `judge` — score an output against rule checks

```sh
skillsaw judge --checks checks.json [--output out.txt]
```

The deterministic, first-line dim-8 mechanism: score a model's output against a
JSON array of rule checks. `hard` is 1.0 iff every check passes; `soft` is
passed/total. **Exit 1 when `hard` is 0.** The output under test is read from
`--output` (default stdin). Operators: `section_present`, `regex`, `contains`,
`tool_called`, `max_chars`, `min_chars`.

```text
$ printf '## Key Risks\nConfidence: High\n' \
    | skillsaw judge --checks checks.json
hard: 1  soft: 1.00
  - section_present: found heading containing "Key Risks"
  - contains: pass Confidence
```

### `hash` — content identity hash

```sh
skillsaw hash SKILL_DIR|SKILL.md [...]
```

Print the first 16 hex chars of `sha256(content)` (identical to SkillOpt's
`skill_hash`). Two skills with the same hash are byte-identical — use it as a cache
key or to confirm an edit changed anything.

### `history` — show the optimization log

```sh
skillsaw history [--file results.tsv] [--skill NAME]
```

Render the `results.tsv` optimization log: one `baseline`/`keep`/`revert`/`error`
row per experiment, nine tab-separated columns. `--file` is configurable (default
`./results.tsv`); `--skill` filters to one skill.

### `version`

```sh
skillsaw version [--json]
```

Print build and VCS metadata.

---

## Determinism, and where a model fits

skillsaw never calls a model itself. The dimensions marked **judge** in the table
above are the qualitative textual assessments a model must supply; skillsaw makes
that boundary explicit and composable:

- `eval` reports which dimensions are `NEEDS-JUDGE` and computes a deterministic
  floor without them.
- `eval --scores <file>` folds a model's per-dimension scores back in to produce
  the full total.
- `judge` converts a skill's `test-prompts.json` expectations into deterministic
  pass/fail checks, so much of dim-8 behavioral scoring needs no model at all.
- `gate`, `hash`, `diagnose`, `scan`, and `history` are fully deterministic.

This mirrors darwin-skill's own thesis: model judgment is unreliable, so lean on
measurable signals first and reserve the model for what genuinely can't be measured.

---

## Architecture

A [climax](https://github.com/StevenACoffman/climax) / [ff/v4](https://github.com/peterbourgon/ff)
CLI: `main.go` wires nothing but the dispatcher; `cmd/<name>/` holds one command
each; the pure domain logic lives in `internal/` and in the shared `skillet` module.

```text
main.go                  entry point (signal handling, exit codes)
cmd/cmd.go               dispatcher — registers every command
cmd/<name>/<name>.go     one command per package (thin I/O shell)
internal/rubric/         9-dimension scoring, deterministic checks, diagnosis
internal/edit/           size-budget + no-op guards for an edit step
```

The rest of the domain logic was extracted to the shared `skillet` module so
skillsaw and exegesis cannot drift:

```text
skillet/skill            parse SKILL.md, content hash, slug, discovery
skillet/neutrality       runtime-neutrality red-light scan
skillet/judge            rule-judge operators (behavioral dim-8 scoring)
skillet/ratchet          validation-gate accept/reject decision
skillet/auditlog         read/write the results.tsv log
skillet/speclint         agentskills.io frontmatter spec (rubric dim-1)
skillet/testprompts      the shared test-prompts.json contract
```

The design keeps a **pure core / imperative shell** split: everything in `internal/`
and the shared `skillet` domain packages is value-in, value-out with no I/O; file and
stdout access lives only in the command `exec` methods.

---

## Development

```sh
go build ./...
go test ./... -race -cover
golangci-lint run ./...        # strict; see .golangci.yaml
```

The linters (gci, gofumpt, golines, plus a strict correctness/style set) are
enforced and not relaxed. `golangci-lint run --fix ./...` auto-applies the
formatters; re-run without `--fix` to confirm a clean tree.

---

## Credits

- **darwin-skill** — the autonomous skill optimizer this reimplements the deterministic core of.
- **SkillLens** & **SkillOpt** (Microsoft Research) — the quality rubric and validation-gate machinery.
