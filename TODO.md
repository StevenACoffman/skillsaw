# skillsaw — TODO

`skillsaw` is the deterministic core of the darwin-skill optimizer, reimplemented
in Go (Pattern B, `ff/v4`). It scores, diagnoses, and validates Agent Skills; a
model is reserved only for the irreducible judge-only rubric dimensions. It is
driven by the `skillsaw-skill`.

## Handoff context

The **book2skill** skill (via the **exegesis** CLI) produces a skill tree and
certifies its **structure**; skillsaw then optimizes each skill's **quality**.
See `../exegesis/TODO.md` for the producer side. The seam is the shared
`test-prompts.json` contract (below): today skillsaw does not read it — `judge`
requires a hand-written `--checks` file, and the `type` tags are ignored.

## Seam-closing work (this pass)

- [x] **S1 — `judge` reads `test-prompts.json` and derives checks from
      `expected`.** DONE: `internal/testprompts` + `skillsaw judge
      --from-test-prompts <file> --id <n>`; uses embedded `checks` if present,
      else `DeriveChecks(expected)`, else errors (never silently passes). Verified
      end-to-end against exegesis output.
- [x] **S2 — Accept the `type`-tagged composition.** DONE: the reader auto-detects
      the canonical `{tests}`, a bare array, and the legacy
      `{test_cases}`/`expected_behavior` shape (string ids fall back to position),
      and exposes `Behavioral()` / `Decoys()`.
- [x] **S3 — Surface the activation signal.** DONE: `internal/activation` +
      `skillsaw activation <skill-dir>` scores trigger vocabulary vs.
      should_trigger/should_not_trigger prompts, with per-case explanations and an
      optional `--min` CI gate. **Reported separately — NOT in the 9-dim total.**

## The shared `test-prompts.json` contract

One file, read by both tools. Each case carries the activation `type` **and**
optional per-case `checks` (what `judge` consumes):

```json
{"tests": [
  {"id": 1, "type": "should_trigger", "prompt": "...", "expected": "...",
   "checks": [{"op": "section_present", "arg": "Risks"}]}
]}
```

`checks` operators are exactly `internal/judge`'s closed set: `section_present`,
`regex`, `contains`, `tool_called`, `max_chars`, `min_chars`. The reader also
accepts a bare top-level array and the legacy `test_cases` key with
`expected_behavior`, mapping them into the canonical shape.

## Check-derivation heuristics (S1)

Deterministic `expected` → `[]Check`, conservative (only emit a check when the
signal is unambiguous, so `judge` never fails on a guess):

- `expected` names a section/heading ("a Risks section", "## Boundary") →
  `section_present`.
- A double-quoted phrase → `contains`.
- "≤ N chars" / "under N characters" → `max_chars`; "≥ N" → `min_chars`.
- Names a tool the output should call → `tool_called`.
- Nothing inferable → emit no checks and report the case as needing a hand-written
  check (never silently pass).

## Eval-methodology adoptions (from cc-thinking-skills `evals/`)

A survey of `/Users/steve/Documents/git/cc-thinking-skills/evals` (an outcome-based
eval harness) found deterministic pieces worth adopting into skillsaw's existing
model-free commands. skillsaw stays deterministic — the model tier stays external.

- [x] **Adopt-2 — `activation` reports FPR/FNR + net-utility + Wilson CI.** DONE:
      new `internal/stats.Wilson` (z=1.959964) + reworked `internal/activation`
      as a routing confusion matrix (TP/FP/TN/FN, TPR/FPR/FNR, net_utility =
      (TP-FP)/total) with Wilson 95% intervals on TPR and FPR. `activation --min`
      now gates on net_utility. (A balanced set caps net_utility at 0.5 — faithful
      to scoreDistractor.)
- [x] **Adopt-4 — `judge` gains objective answer-scorers.** DONE:
      `internal/judge/objective.go` adds `boolean`, `multiple_choice`,
      `numeric_order_of_magnitude` (last-ANSWER parsing faithful to objective.js;
      `evalObjective` dispatcher keeps `eval` under the cyclop limit). Skipped
      brier/synthesis/file_localization.
- [x] **Adopt-5 — `gate` separates the measured axis from the disposition axis.**
      DONE: `gate.Result` gains `Delta` and `Status` (improved/tie/regressed);
      `Action` stays the disposition. Human output reads e.g.
      `accept (improved, delta +4.0)`. No statistical/replication axes (score is
      deterministic).

## Deliberately NOT absorbed (kept in the separate outcome tier)

- `droid` transport + live `run-routing`/`run-objective`/`run-pairwise`/
  calibration/experiments: the model-calling tier. Folding them in breaks
  skillsaw's "never calls a model" contract. cc-thinking-skills already is that
  tier; a blind-pairwise judged comparison belongs to the agent layer, not the
  gate CLI.
- Cluster bootstrap / Holm / power analysis / judge-panel Cohen's-kappa: no
  variance to test in a deterministic rubric score; only proportion-based
  activation warrants CIs (hence Adopt-2's Wilson, nothing heavier).

## Cross-repo (shared with exegesis)

- [x] Extract the `test-prompts.json` + `checks` schema and the agentskills.io
      frontmatter lint into a **shared Go module** so skillsaw and exegesis cannot
      drift. DONE (2026-08-03): the schema was already shared via `skillet/testprompts`
      + `skillet/judge`; the frontmatter spec now lives in `skillet/speclint`. The
      rubric's dim-1 `checkFrontmatter` sources the description cap from
      `speclint.DescriptionMaxRunes` (its own local `descCharLimit` is gone), so the
      1024-rune cap can't diverge from exegesis by hand. The rubric's scoring weights
      (penalties/flags) stay in skillsaw — they're rubric policy, not the spec.

## Housekeeping

- [x] `internal/rubric/rubric_test.go` pre-existing `golines` lint hit — fixed by
      `golangci-lint run --fix`.
- [x] Make an unknown subcommand exit non-zero with usage. DONE: the dispatcher
      detects a selected group parent (`Exec == nil`) with a leftover positional
      after Parse and returns `"<cmd>: unknown subcommand \"x\""` (exit 1); a bare
      invocation still returns `ff.ErrNoExec` → exit 0.
- [x] Fill in the root `ShortHelp` — DONE: `cmd/root/root.go` now has a real ShortHelp
      ("deterministically score, diagnose, and validate Agent Skills") and a `LongHelp`
      listing the subcommands, mirroring exegesis. `climax lint` no longer flags it.
- [x] Doc-sync: `README.md` and `improvements_plan.md` — DONE. The README package map
      now lists only the local `internal/{rubric,edit}` and a separate block for the
      extracted `skillet/{skill,neutrality,judge,ratchet,auditlog,speclint,testprompts}`
      (`gate`→`ratchet`, `store`→`auditlog`); the pure-core note spans both.
      `improvements_plan.md` carries a banner mapping its historical `internal/*` paths
      to their skillet homes rather than rewriting the plan's steps.

## Cross-repo alignment (2026-08-05 survey)

- [ ] **Bump skillet v0.1.0 → v0.5.0 — highest-priority drift exposure in the family.**
      skillsaw is five minor versions behind the shared kernel and shares
      `speclint`/`judge`/`testprompts` with exegesis (on v0.4.0). Sharing those across
      four versions is exactly the frontmatter/scoring drift skillet was extracted to
      prevent, so this is the most important alignment step, not a routine bump. v0.5.0
      also brings `ratchet`/`stats` refinements, `ruleset` (+`SourceAnchor`), and the
      `toerr.WrapWithMessage`/`wrapcheck` integration. Re-run the suite after the bump —
      `speclint.DescriptionMaxRunes` and the `judge`/`testprompts` shapes are the seams
      most likely to have moved.
- Note: `RULES.md` (138 KB) and `improvements_plan.md` describe the pre-migration
      `internal/*` layout; both are historical references (the latter already banners
      this), not the current package map — the README package map is authoritative.
