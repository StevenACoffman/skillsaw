# skillsaw — TODO

`skillsaw` is the deterministic core of the darwin-skill optimizer, reimplemented
in Go (Pattern B, `ff/v4`). It scores, diagnoses, and validates Agent Skills; a
model is reserved only for the irreducible judge-only rubric dimensions. It is
driven by the `skillsaw-skill`.

## Handoff context

The **book2skill** skill (via the **exegesis** CLI) produces a skill tree and
certifies its **structure**; skillsaw then optimizes each skill's **quality**.
See `../exegesis/TODO.md` for the producer side.

The seam is the shared `test-prompts.json` contract (below), which `judge` now
reads directly — `--from-test-prompts` uses a case's embedded `checks` or derives
them, and the `type` tags drive `activation`. The structural half of the seam is
shared as code: `skillet/speclint` and `skillet/redlines` are the one definition
of frontmatter and body structure that exegesis gates on and skillsaw rejects on,
so the two cannot drift by hand.

## Seam-closing work (2026-08-03, complete)

- [x] **S1 — `judge` reads `test-prompts.json` and derives checks from
      `expected`.** DONE: `internal/testprompts` + `skillsaw judge
      --from-test-prompts <file> --id <n>`; uses embedded `checks` if present,
      else `DeriveChecks(expected)`, else errors (never silently passes). Verified
      end-to-end against exegesis output.
- [x] **S2 — Accept the `type`-tagged composition.** DONE: the reader auto-detects
      the canonical `{tests}`, a bare array, and the legacy
      `{test_cases}`/`expected_behavior` shape (string ids fall back to position),
      and exposes `Behavioral()` / `Decoys()`.
- [x] **S3 — Surface the activation signal.** DONE: `skillsaw activation <skill-dir>`
      scores trigger vocabulary vs. should_trigger/should_not_trigger prompts, with
      per-case explanations and an optional `--min` CI gate. **Reported separately — NOT
      in the 9-dim total**, which is still the load-bearing part of this entry: activation
      measures routing, not quality, so folding it into the rubric would mix two axes.
      Location correction (2026-08-07): this originally landed as `internal/activation`,
      which no longer exists. The confusion matrix moved to `skillet/ratchet` when Adopt-2
      reworked it (so adh and skillsaw share one implementation) and the reporting stayed
      in `cmd/activation`. `internal/` now holds only `edit` and `rubric`.

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

- [x] **Bump skillet v0.1.0 → v0.5.0 — the family's biggest drift exposure, now closed.**
      DONE (2026-08-05): go.mod/go.sum only — no code change even across the four-minor jump,
      4 packages test green, `golangci-lint` clean. The seams most likely to have moved —
      `speclint.DescriptionMaxRunes`, the `judge`/`testprompts` shapes, and the
      `ratchet`/`stats` refinements — were all additive, so the suite passed untouched.
      `go mod tidy` did not add `toerr` (skillsaw imports nothing that reaches
      `ruleset/synthesize`). skillsaw now shares the same `speclint`/`testprompts` revision
      as exegesis, closing the drift skillet was extracted to prevent.
- [x] **Bump skillet v0.5.0 → v0.9.0.** DONE (2026-08-06), in two steps for two reasons.
      **v0.8.0** added `skillet/redlines`, promoted out of `exegesis/internal/lint` because
      skillsaw was the second consumer that justified it — the structural gate below is built
      on it. **v0.9.0** added `Skill.FrontmatterErr`, which is what makes `preflight` honest:
      before it, a frontmatter block that failed to parse was reported as an empty
      description, so the gate blamed the wrong line. `toerr` arrives as an indirect
      dependency via `errs`; no skillsaw code changed for either bump.
- Note: `RULES.md` (138 KB) and `improvements_plan.md` describe the pre-migration
      `internal/*` layout; both are historical references (the latter already banners
      this), not the current package map — the README package map is authoritative.

## Convenience gaps (from the gemini_skills gap analysis, 2026-08-05)

Source: `~/Documents/agent-orange/gemini_skills/processing/gap_analysis.md`.

- [x] **Closed-loop structural pre-flight before adopting an edit (double-gated pipeline).**
      DONE (2026-08-06), via the shared-package path the note below preferred.
      `skillet/redlines` was promoted out of `exegesis/internal/lint` (skillet v0.8.0,
      skillet#9), so both tools now gate structure from one definition instead of skillsaw
      shelling out to the `exegesis` binary.
      `internal/edit.StructuralDefects(s, withRedlines)` composes `speclint.Frontmatter` with
      `redlines.Check` and is the gate's single definition of "structurally sound"; new
      `skillsaw preflight [--redlines] [--json] SKILL_DIR ...` exits non-zero on any defect,
      so the optimize loop can run it between writing an edit and deciding to keep it. This is
      the structural half of the double gate — `gate` decides on score, `preflight` on
      structure — and it is deliberately stricter than `eval`, which only *penalises* a blown
      description cap so a gain elsewhere can outweigh it.
      **The red lines are opt-in, which the plan did not anticipate.** Testing against real
      skills showed the RIA-TV++ segment check rejects every hand-written skill: 13 of 20 in
      `~/.claude/skills` fail `--redlines` purely for not being book2skill output. Enforcing
      them by default would have made the gate useless for most of what skillsaw optimizes, so
      the default is spec-only (18 of those 20 pass; the 2 rejections are genuine disallowed
      frontmatter keys) and `--redlines` is for book trees, mirroring
      `exegesis lint --check redlines`.
      Skillet bumped v0.5.0 → v0.8.0 in the same pass; `toerr` arrives as an indirect
      dependency via `errs`, and no skillsaw code needed changing.
      Not done: the agent-facing loop in `~/skills/skillsaw-skill/SKILL.md` still does not call
      `preflight`. It should run between STEP 2's edit and the `gate` decision, with
      `--redlines` when the tree is a book. That file is outside this repo.
- [x] **Surface a frontmatter parse failure as itself.** DONE (2026-08-06), shipped and live
      here on skillet v0.10.0. `Skill.FrontmatterErr` records the YAML error, and every check
      that reads a field out of the parsed block now declines to speak: `speclint.Frontmatter`
      reports the error instead of an empty description, and `redlines.Check` no longer
      demands a trigger condition of prose the parser could not reach.
      Checks that read the *body* were deliberately left running — `splitFrontmatter`
      produces it before the parse is attempted — which is why the real 219-word quotation on
      the skill that exposed this survives.
      Measured: `preflight --redlines` on
      `books/site-reliability-engineering/blameless-postmortem-process` reports **2** defects,
      both true — the YAML error at `[10:45]` and that quotation. Before the chain it produced
      four, two of which were consequences of the one syntax error. The residual noted here
      earlier — exegesis's own name/folder check firing on a name it could not read — closed
      in exegesis#13.
- [ ] **Consume `skills-manifest.json`: a hash-keyed skip list for the *agent's* judge
      pass (not an `--incremental` flag on the deterministic CLI).**
      **Correction to the premise:** the analysis attributes "up to 90% in LLM API costs"
      to skipping `skillsaw`'s own auditing. skillsaw never calls a model — that is a
      standing design contract (see "Deliberately NOT absorbed" above). `eval`, `scan`, and
      `diagnose` are cheap local computation; making them incremental saves no API spend and
      little wall clock. The real cost is one tier up, in `skillsaw-skill`: Phase 1 hand-
      scores the six judge-only dims per skill into `scores.json`, and Phase 2 runs the
      skill for dim 8. *That* is what a hash pin can skip.
      **What's genuinely missing.** The hash primitive is already shared and already
      byte-identical on both sides — `skillet/identity.Hash` backs both `skillsaw hash` and
      the manifest's `sha256` field (`skillet/manifest`) — so nothing needs to be computed,
      only *read*. skillsaw today rediscovers the tree via `skill.DiscoverRoots` and ignores
      the manifest entirely. Add:
      1. ~~a manifest reader + a way to ask "which skills changed since this base
         manifest?"~~ **Done upstream in skillet (2026-08-07):** `manifest.Parse`,
         `manifest.Diff(base, cur) Delta`, and `Delta.Stale()`. What remains here is the
         `skillsaw changed --manifest base.json --tree DIR` command wrapping them — scan
         the tree into a `manifest.Manifest` literal (`Tree` + `Skills`, hashing each via
         `skill.Load(...).Hash()`), call `Diff`, print `Stale()`. Two corrections to the
         shape assumed above: it must list **tree-relative dirs, not slugs** (`DiscoverRoots`
         scans four roots, so `.claude/skills/foo` and `.cursor/skills/foo` share a slug and
         a slug-keyed listing is ambiguous — `Diff` already keys on location), and the
         `--tree` spelling must not matter (`Diff` relativizes to each manifest's `Tree`,
         so don't re-derive paths around it). skillet deliberately does **not** ship the
         tree-scan helper — one consumer, held by promote-on-second-consumer — so the walk
         lives here. **DONE (2026-08-07): `skillsaw changed --manifest base.json
         [--tree DIR] [--json]`.** No `internal/` package: the pure core is already
         upstream (`Parse`/`Diff`/`Stale`), and a local one that only forwarded to it
         would be a layer to see through. Scans into a `manifest.Manifest` struct literal
         rather than `manifest.Build`, because `Build` also takes the emitting tool and
         whether every gate passed and neither means anything for a freshly-walked tree.
         A skill whose `SKILL.md` is present but unreadable is recorded with **no hash**
         rather than skipped, so `Diff`'s unknown-hash rule lands it in the campaign;
         skipping would drop it from the tree's inventory for good, and one bad skill must
         not abort the walk. A *deleted* `SKILL.md` is a different case -- discovery does
         not see the directory at all, so it is correctly reported as removed. Exits 0
         whether or not anything is stale: it is a query, and `verified` is the gate.
         `--all`/`--roots` deferred -- `Diff` needs one `Tree` to relativize against, and
         location-keying already makes the multi-root case correct when it is added.
         Verified on the real 233-skill tree against a manifest written by the released
         `exegesis verify`: untouched reports 0 to reprocess / 233 unchanged; after one
         edit, one deletion and one addition it names exactly those two as stale with 1
         removed; and absolute and relative `--tree` spellings agree.
      2. a hash-keyed `scores.json` cache so a `--scores` file records which hash it was
         judged against, and a stale entry is an error rather than a silent reuse;
      3. ~~gate on `structure_verified`~~ **DONE (2026-08-07): `skillsaw verified
         MANIFEST`**, exit 0 only when the manifest says the structure passed. A separate
         command, not a flag on `changed`, because the TODO is right that it is
         independent: an unverified tree is unfit to optimize whether or not anything in
         it changed. skillsaw has no `optimize` subcommand to hang it on -- the loop lives
         in `skillsaw-skill` -- so the skill calls it and checks the exit code.
         It prints the tool, tree and skill count beside the verdict: a bare pass/fail
         leaves the reader unable to tell whether they gated the manifest they meant.
         A file that is not a manifest is an **error**, not a quiet "unverified" --
         `manifest.Parse` already rejects a document with no `tool` field, and reporting a
         zero-valued manifest as a failed gate would send the reader hunting for
         structural defects in a tree that was never examined.
      (3) is a free correctness win independent of the caching. Drop the "90%" figure; the
      real saving is "one judge pass per *changed* skill per campaign", unmeasured.
- [ ] **Serialize derived checks back to `test-prompts.json` — but as a `tests` command,
      not a `judge` flag.** Confirmed real: `judge --from-test-prompts` calls
      `testprompts.ChecksFor`, and when checks are derived it only prints a stderr note
      (`cmd/judge/judge.go:157-161`); the derived array never reaches disk, so nobody can
      review, hand-tune, or commit it. `skillet/testprompts.Write` already exists, so
      serialization is nearly free.
      **Three corrections to the shape.** (a) Partly solved upstream already: `exegesis
      scaffold`'s `BuildTests` seeds `Checks: DeriveChecks(p.Expected)` and writes them, so
      scaffolded files already carry explicit checks — the gap only bites files from
      `distill`, hand-authoring, or the legacy shape. (b) `judge` scores exactly one case
      (`--id N`) and its job is to score an output; mutating its input file as a side effect
      is the wrong ergonomics for a gate CLI. Put it on a whole-file command instead — a
      `skillsaw tests --write-checks` mirroring `exegesis tests --scaffold`, filling checks
      for every case that has none. (c) **Write-back is a normalizing rewrite**: the reader
      accepts three on-disk shapes (canonical `{tests}`, a bare array, legacy
      `{test_cases}`/`expected_behavior`) and `Write` emits only the canonical one, so any
      legacy file is silently migrated by the same command. Either refuse to write back over
      a non-canonical file or say plainly that it converts.
      **(c) is unblocked upstream (available since the v0.11.0 bump):** the "refuse" option was
      unimplementable because `Parse` discarded what it matched. `File.Rewrites []string` now
      lists every difference from canonical form — `len()==0` means the file was canonical,
      and the strings are written for a message (`case 3: legacy "expected_behavior" field;
      canonical is "expected"`). Print them and convert, or refuse when non-empty; either is
      now a two-line decision. Enumerating them surfaced a fourth case worth surfacing to the
      user: **when a file has both `tests` and `test_cases`, the reader has always preferred
      `tests` and dropped the rest**, so a write-back deletes cases still visible on disk.
      Cross-repo: derived checks are a producer concern as much as a consumer one — decide
      whether this lands here or as `exegesis tests --derive-checks` (see
      `../exegesis/TODO.md`); duplicating it in both is the outcome to avoid.

## Reasoning-toolkit survey (unified-thinking, 2026-08-05)

Source: a survey of `~/Documents/git/unified-thinking` (a deterministic Go reasoning
toolkit) for reusable techniques.

- [x] **Calibrate the judge scores.** DONE (2026-08-07): `internal/calibrate` +
      `skillsaw calibrate [--json] JUDGMENTS.json` over `skillet/calibration`
      (ECE/MCE/Brier, ten equal-width bins). Input is
      `{"judgments":[{skill, dim, base, passed}]}` — the 1-10 confidence stated before an
      outcome was known, and what the outcome turned out to be.
      **The subject is a prediction the outcome cannot see:** the agent's stated confidence
      that an edit will pass the validation gate. The confidence is fixed at STEP 3 when the
      edit is proposed; `gate` decides at STEP 6 after an independent re-score.
      **Correction (2026-08-07):** an earlier draft named dim 8 as the source. That was
      wrong and is fixed in the help text, the package doc and here. The dim-8 base is
      *computed from* the judge output (`round(10 × mean soft)`), so scoring it against
      `judge`'s hard verdict measures how the checks were written, not how well the agent
      judges. `DET.SCORE` is not the subject either — a deterministic total is a function of
      the file and has no variance to calibrate.
      **Interpretation limit, stated in the help text and the code:** the 1-10 → [0,1] map
      is a convention, not a probability — a base of 8 does not assert an 80% chance of
      passing. What the report supports is the *relative* signal: accuracy sitting below
      confidence across bins means systematic overconfidence. Do not read Brier as a
      probability claim.
      Reports, does not gate — no `--max-ece`, because no threshold has been calibrated
      and a knob nobody can value is worse than none. A base outside 1-10 is dropped
      rather than clamped (clamping would invent a judgment) and the drop is reported, as
      is a thin sample, since a ten-bin report over a handful of judgments is mostly noise.
      Not wired into `skillsaw-skill`: the agent loop does not yet record these judgments,
      and documenting a workflow nobody runs is how merge-skills ended up instructing
      agents to call commands that do not exist. Wiring it up is the natural follow-on —
      the loop already computes both halves at STEP 5.
- [ ] A timeseries **regression gate** for rubric quality across skill versions — a CI check
      that fails on a quality *drop* vs. the recent baseline, not just a fixed threshold.
      **The math now lives in `skillet/timeseries`** (2026-08-07, available since the v0.11.0 bump): wanting it
      here *and* in exegesis was the 2nd consumer that promoted it.
      `Detect(history, current, Config) Verdict`; what remains here is reading the history out
      of `results.tsv` (`skillet/auditlog` already parses it) and choosing a `Tolerance`.
      Two things to get right when wiring it: `Verdict.Compared` is false when history is too
      short and must **not** be read as "passed" — a gate that fails a metric's first run can
      only be fixed by not measuring; and `Tolerance` is absolute, in the rubric's own units,
      so a zero tolerance calls any dip below the mean a regression.
      Correction to the note below: unified-thinking's `DetectRegression` is *not* a
      rolling-window baseline — it uses the single most recent entry, treats a zero baseline
      as an absent one, and divides by the baseline. skillet's version fixes all three.
- Note: skillsaw's TP/FP/TN/FN activation confusion matrix (with Wilson intervals) is
      already more rigorous than unified-thinking's binary exact/contains/tolerance
      evaluators — nothing to adopt there. Calibration is the one real gap; its keyword
      bias/fallacy detectors are the "instruct, don't enforce" shape skillsaw rejects.
