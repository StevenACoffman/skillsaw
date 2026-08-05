# skillsaw improvements plan

Implements the six follow-ups identified by comparing skillsaw against the darwin
spec and the SkillOpt / SkillLens sources, and brings the whole module to a clean
`golangci-lint run ./...` **without relaxing any rule** in `.golangci.yaml`.

> **Note (2026-08-05):** the `internal/*` packages this plan creates below —
> `skill`, `neutrality`, `judge`, `gate`, and `store` — were subsequently extracted
> to the shared `skillet` module and no longer exist under `internal/`. Read them as
> `skillet/{skill,neutrality,judge,ratchet,auditlog}` (`gate`→`ratchet`,
> `store`→`auditlog`). Only `internal/rubric` and `internal/edit` remain local.

Guiding rules (from `~/Documents/agent-orange/go-advice/summary_rules.md`), applied
throughout:

- **Functional core / imperative shell (§5).** All new domain logic (rule
  judging, parsing, hashing, size/idempotence checks, score arithmetic) is pure:
  values in, values out, no I/O, no globals. File reads/writes and stdout live only
  in command `exec` methods and thin loaders.
- **Deep, minimal interfaces (§4).** No interface is introduced until two callers
  need it. Functions take the narrowest input they use.
- **Errors (§3).** Static messages use `errors.New`; wrapped errors carry an
  operation prefix and `%w`; boundary errors from other packages are wrapped
  (`wrapcheck`); never return `(nil, nil)` (`nilnil`).
- **Testing as design (§9–10).** Table-driven, stdlib `testing` only, `t.Helper()`
  in helpers, public-API tests in `_test` packages, property tests for pure
  functions with clear invariants (idempotence, monotonicity, round-trip).
- **CLI ff/v4 (§18 Pattern B).** Every knob is a flag bound in `New`; write to
  `cfg.Stdout`/`cfg.Stderr`; return `root.ExitError(n)`; never `os.Exit`.
- **Declaration hygiene.** `const` → `var` → `type` → `func` per file (`decorder`);
  no mutable package globals (`gochecknoglobals`); heavy structs passed by pointer
  (`gocritic hugeParam`); JSON structs carry tags (`musttag`).

Verification protocol after **every** step:

```sh
gofmt -l . && go build ./... && go vet ./...
go test ./... -race -count=1
golangci-lint run --fix ./...   # then re-run without --fix; must report 0 issues
```

`--fix` must never be paired with a `.golangci.yaml` edit that disables or loosens a
linter. When `--fix` leaves the tree un-buildable (its `use-errors-new` /
`perfsprint` rewrites can drop an import), repair imports with `goimports` + `gci`,
never by reverting the lint fix.

______________________________________________________________________

## Step 0 — Lint-clean the existing baseline

The six commands written earlier trip 24 checks. Fix them first so later steps land
on a clean base. No behavior changes; pure refactors verified by the existing tests.

| Issue (linter) | Files | Fix |
| --- | --- | --- |
| declaration order (`decorder`) | gate, rubric, diagnose, neutrality, skill, scan, root, rubric_test | Reorder each file to `const`→`var`→`type`→`func`. |
| heavy value param (`gocritic hugeParam`) | rubric.go (`cfg Config` ×5), rubric_test (`DimScore` ×1) | Pass `*Config` / `*DimScore`; treat as read-only. |
| package globals (`gochecknoglobals`) | rubric `descCharLimit`, history `columns` | `descCharLimit` → `const`; `columns` → `func columns() []string`. |
| complexity (`cyclop`, `nestif`, revive cognitive) | rubric `Evaluate` (11), skill `parse`, history `exec` (17) | Extract helpers: a dimension-check dispatch table; a `splitFrontmatter` helper; a `renderRows` helper. |
| exported const needs doc (`revive`) | gate `Metric`, `Action` const blocks | Add block doc comments. |
| `os.Getwd` forbidden (`forbidigo`) | eval, scan `resolveDirs` | Drop `os.Getwd`; discover relative to `"."` (a `--base` flag, default `.`). |
| unwrapped error (`wrapcheck`) | eval, scan `resolveDirs` | Wrap: `fmt.Errorf("discover skills: %w", err)`. |
| missing JSON tags (`musttag`) | gate `Result`, neutrality `Hit` | Add `json:"..."` tags (both are JSON-encoded by commands). |

Exit criterion: `golangci-lint run ./...` reports 0; `go test ./... -race` passes.

______________________________________________________________________

## Step 1 — Rule-judge for behavioral (dim 8) scoring

**Why:** the darwin spec §8.5 says dim 8 should lead with deterministic rule judges
(SkillOpt-Sleep `judges.py`: `hard = all-pass`, `soft = passed/total`), reserving a
model only for the irreducible remainder. skillsaw currently marks dim 8 entirely
needs-judge.

**New package `internal/judge` (pure core):**

```go
// Op is a rule-check operator.
type Op string
const (
    OpSectionPresent Op = "section_present" // a "## <arg>" heading exists
    OpRegex          Op = "regex"           // arg matches (RE2)
    OpContains       Op = "contains"        // arg is a substring
    OpMaxChars       Op = "max_chars"       // len(output) <= atoi(arg)
    OpMinChars       Op = "min_chars"       // len(output) >= atoi(arg)
    OpToolCalled     Op = "tool_called"     // output names the tool (heuristic: contains arg)
)

type Check struct { Op Op `json:"op"`; Arg string `json:"arg"` }

type Result struct {
    Hard  float64  `json:"hard"`   // 1.0 iff every check passes, else 0.0
    Soft  float64  `json:"soft"`   // passed / total
    Why   []string `json:"why"`    // one line per check: pass/fail + reason
}

// Score evaluates output against checks. Pure: no I/O, deterministic.
// Requires: len(checks) > 0. Empty checks -> Result{} and a sentinel error
// (define-errors-out-of-existence: caller must not ask to judge with no checks).
func Score(output string, checks []Check) (Result, error)
```

**Compilation of a regex check** is the only fallible per-check step; a bad pattern
is a check *authoring* error surfaced as a failed check with a `why` reason, not a
panic (no `MustCompile` on caller data).

**Command `skillsaw judge`:** `skillsaw judge --checks checks.json --output out.txt`
(or `--output -` for stdin). Prints `hard`/`soft`/`why`; `--json` for machine use.
Exit 1 when `hard == 0` so a harness can branch. This is the standalone deterministic
scorer a harness (or a human pasting an LLM output) feeds; it does **not** fake dim 8
inside `eval` (skillsaw has no model to produce the output under test).

**Tests:** table over each Op (pass + fail), `soft` fraction, all-pass ⇒ `hard==1`,
empty-checks error, bad regex ⇒ failed check not panic.

______________________________________________________________________

## Step 2 — Tests for `internal/skill` and `internal/neutrality`

**Why:** these two packages are untested; SkillOpt's `test_scoring.py` shows the bar
for the hash/identity code in particular.

- `skill_test.go` (external `skill_test`): `Hash` (determinism, unicode, empty,
  length 16, hex-only, distinct inputs differ — mirrors `TestSkillHash`); `Load`
  (frontmatter/body split, name/description extraction, missing-frontmatter file,
  missing file ⇒ wrapped error); `Discover` (finds SKILL.md dirs under roots, skips
  missing roots, dedups). Uses `t.TempDir()`; no network, no fixtures outside the
  package dir.
- `neutrality_test.go`: table with one case per red-light alternation (each must
  hit), a clean case (0 hits), and multi-line line-number correctness.

______________________________________________________________________

## Step 3 — Harden the frontmatter parser + slug validation

**Why:** `frontmatterValue` handles only single-line `key: value`. SkillLens skills
(and the Agent Skills standard) permit YAML block scalars (`description: >` / `|`)
and quoted multi-line values; a block scalar currently yields an empty description
→ a spurious dim1 penalty. SkillLens also defines a canonical `slugify`.

- Extend `frontmatterValue` to a small pure `parseFrontmatter(fm string) map[string]string`
  that handles: single-line scalars, `"`/`'` quoted values, and `>`/`|` block
  scalars (fold/literal, indentation-stripped). Keep it a table-scanned state
  machine — no YAML dependency (stdlib-only per §10), because the surface is tiny.
- Add `func Slug(s string) string` matching SkillLens: NFKD-fold → lowercase →
  non-`[a-z0-9]`→`-` → collapse/trim `-` → cap 64 → `"skill"` if empty. Rubric dim1's
  kebab check uses `Slug(name) == name` instead of a bare regex, so the check matches
  the standard's own normalization.
- **Tests:** block-scalar and quoted descriptions parse; `Slug` table (unicode,
  spaces, punctuation, over-64, empty). Round-trip property: `Slug(Slug(x)) == Slug(x)`
  (idempotent).

______________________________________________________________________

## Step 4 — `internal/store`: read *and write* the results.tsv log

**Why:** skillsaw reads `results.tsv` but cannot append to it; darwin §13 and SkillOpt
persist every experiment. This is the write half of the ratchet log, needed before any
optimize loop can exist.

```go
type Row struct { // 9 columns, spec §13
    Timestamp, Commit, Skill, OldScore, NewScore, Status, Dimension, Note, EvalMode string
}
func Columns() []string                       // canonical header
func (r Row) Fields() []string                // pure: Row -> 9 fields, validated
func Read(rd io.Reader) ([]Row, error)        // pure over a reader; skips header
func Append(w io.Writer, rows ...Row) error   // writes header iff w is empty-aware caller
```

- `Read`/`Fields` are pure (take `io.Reader`/return values); the *file* open/append
  is the command shell (`history` refactors to `store.Read`; a thin `openAppend`
  helper handles "create with header if absent").
- `Status` is a typed enum (`baseline|keep|revert|error`) validated on write; unknown
  status is an `EINVALID`-style error, not silently written.
- **Tests:** round-trip (`Append` then `Read` recovers rows), header handling,
  malformed-row rejection, `Status` validation. Refactor `history` to use `store.Read`
  and keep its existing behavior (verified by a new `history` smoke path if cheap).

______________________________________________________________________

## Step 5 — `eval --scores`: judge-supplied bases → full rubric total

**Why:** with no model, `eval` can only compute the deterministic floor. Accepting a
judge's per-dimension bases (dims 1,2,3,5,7,8) lets it compute the **full** rubric
total (spec §8.3). The file is untrusted input, so parse defensively (SkillOpt
`extract_json`: prefer an error over an ambiguous/partial object; never fabricate).

- Pure `func ParseScores(data []byte) (map[int]int, error)`: strict JSON object
  `{"2": 8, "3": 7, ...}`; reject unknown keys, out-of-range (must be 1..10), and
  malformed input with a specific error (no silent defaulting).
- `rubric.Evaluate` gains an optional `bases map[int]int` (nil = today's behavior):
  when a base is supplied for a needs-judge dim, use it (still minus deterministic
  penalty) and mark `HasBase`. When **all** needs-judge dims have bases, the
  evaluation reports a `FullScore` alongside `DeterministicScore`.
- `eval --scores <file>`: load, parse, pass to `Evaluate`; print the full total and
  flag any dims still missing a base. Flag is a normal ff/v4 knob.
- **Tests:** `ParseScores` table (valid, unknown key, out-of-range, bad JSON, empty);
  `Evaluate` with full bases ⇒ `FullScore` set and equals hand-computed total;
  partial bases ⇒ `FullScore` absent.

______________________________________________________________________

## Step 6 — Idempotence + size-budget guards (pure, ready for an edit step)

**Why:** darwin §11.3 rejects an edit that grows a skill past 1.5×; SkillOpt's
`test_no_op_when_already_optimal` asserts re-optimizing a converged skill changes
nothing. Neither matters until an `apply`/`optimize` command exists, but both are
cheap pure invariants worth having (and testing) first.

- New package `internal/edit` (pure):
  ```go
  // WithinSizeBudget reports whether newBytes stays within ratio*origBytes.
  func WithinSizeBudget(origBytes, newBytes int, ratio float64) bool
  // IsNoOp reports whether two skill contents are byte-identical (hash-equal).
  func IsNoOp(before, after string) bool   // uses skill.Hash
  ```
- **Tests:** boundary cases for `WithinSizeBudget` (exact 1.5×, just over/under, zero
  orig); `IsNoOp` (identical, whitespace-differing, empty). Property: `IsNoOp(x, x)`
  is always true (idempotence of identity).

______________________________________________________________________

## Rule-alignment pass (revisions made to this plan per summary_rules.md)

1. **Split "judge" into two decoupled concepts** to avoid the overloaded-name trap
   (§4 vague names): Step 1 `judge` = deterministic rule scorer of an *output*;
   Step 5 flag named `--scores` (not `--judge`) = per-dimension base overrides. They
   never share a symbol.
2. **No faking dim 8 in `eval`** (§9, tests as honesty): Step 1 delivers a standalone
   scorer + `judge` command rather than inventing outputs inside `eval`. The
   determinism boundary stays truthful.
3. **Pure core everywhere** (§5): every new unit is a value→value function; I/O is
   pushed to command `exec`. This is why `store.Read` takes `io.Reader` and
   `judge.Score`/`ParseScores`/`WithinSizeBudget` take plain values — each is unit
   testable with no fixtures.
4. **Define errors out of existence** (§3): `judge.Score` requires non-empty checks
   (a no-check judge is meaningless) rather than silently returning `hard=1`; a bad
   regex becomes a failed check, not a panic.
5. **Model constraints in types** (§4): `store.Status` and `judge.Op` are typed enums
   (`exhaustive`-checked) instead of free strings.
6. **Narrow inputs, no premature interfaces** (§4, §10): no interface is added; if an
   optimize loop later needs to swap a judge, the interface is defined *then*, at the
   point of use.
7. **Step 0 first** (§ lint discipline): the plan front-loads the lint baseline so
   every feature step ends at exactly 0 issues, making the per-step
   `golangci-lint run --fix` a true gate rather than a diff against pre-existing noise.
