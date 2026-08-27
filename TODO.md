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
- [x] **Consume `skills-manifest.json`: a hash-keyed skip list for the *agent's* judge
      pass (not an `--incremental` flag on the deterministic CLI).** DONE (2026-08-08) —
      all three sub-items below are complete. What remains is wiring `verified` and
      `changed` into `skillsaw-skill`, tracked in that skill's own TODO.
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
      2. ~~a hash-keyed `scores.json` cache~~ **DONE (2026-08-08): `internal/scores` +
         `eval --scores` accepts a hash-bound shape.** A base is a number someone assigned
         after reading a particular version of a skill, and nothing about it survives an
         edit; each entry now names the content hash it was judged against and is only
         applied to a skill that still hashes to it.
         **The failure was live, not hypothetical.** Demonstrated against the released
         binary: judge a real skill, edit it, re-run `eval --scores` with the same file --
         the old binary silently reports the same FULL 78.0/100 for the changed text. In
         the optimize loop that total is `NEW` at STEP 5 and the input to `gate` at STEP 6,
         so a wrong number decides keep-or-revert and is logged to `results.tsv` as
         comparable. The documented loop avoids it by re-judging into `newscores.json`, but
         nothing enforced that, and a deterministic gate should not rely on care.
         **A second defect found while reading `eval`:** one `bases` map was applied to
         every directory in the run, so `--all --scores` gave all 233 skills one skill's
         judge bases and called every FULL total comparable. Bases are now matched per
         skill; verified with two skills judged 10s and 2s, which the old binary scored
         identically at 91.6 and the new one scores 91.6 and 22.8.
         A skill judged at another version is **reported and not scored**, after the rest
         of the run is scored -- with `--all` one stale entry must not hide the other
         verdicts, but a total built on stale bases is not comparable to the ones beside
         it, so the run still exits non-zero. "Judged at another version" is kept distinct
         from "never judged": both yield no total, but one means re-judge and the other
         means judge.
         **The legacy flat shape still works**, byte-identical against the released binary
         on a real skill (FULL 78.0/100 both ways). It records no version so it cannot be
         checked against one -- unverifiable rather than stale.
         `rubric.ParseScores` moved to `internal/scores`: the dimension-key and 1-10
         validation belongs with the format that carries those numbers, and `rubric` has
         no business owning a file layout.
         **Follow-up, in another repo:** `skillsaw-skill` Phase 1 step 4 still writes the
         flat shape. It should write the hash-bound one -- otherwise the protection is
         available but unused by the loop that needs it most.
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

## Deterministic work still left to the agent (survey 2026-08-08)

Extracted every shell line `skillsaw-skill` tells the agent to run that is *not* a
`skillsaw` call. What follows is the deterministic remainder — arithmetic, formatting and
file construction the CLI could own. Designing test prompts, scoring the judge-only
dimensions, proposing the edit and running the skill stay with the agent; those have no
deterministic form, and skillsaw not calling a model is a standing contract.

None of these need a skillet change. Where one would have, skillet already has it.

- [x] **`skillsaw log` — write a results.tsv row.** DONE (2026-08-08). The skill hand-builds **two** different
      nine-field tab-separated `printf`s (baseline, and per-round at STEP 6). Column-order
      drift between those and `auditlog.Columns()` is silent — nothing would catch it.
      **`internal/auditlog` already has the writer**: `Append(w, rows...)`, `Row` with all
      nine fields, and `Columns()` for the header. It has sat unused since it was written;
      only `history` (the reader) is exposed. This is the best ratio of the survey — a
      command over code that already exists and is tested, reusing the exact `Row` type
      `history` reads back.
      **Outcome.** All three were exposure rather than construction, as the survey said:
      `auditlog.Append` and `edit.{IsNoOp,WithinSizeBudget}` existed, were tested, and had
      zero production callers. What landed is one command, one flag pair, and one doc change.
      `skillsaw log` writes the header from `auditlog.Columns()` when the file is new, so the
      skill's hand-written header line is gone too — that was the one place the column order
      was spelled out a second time. Status validation stays in `auditlog.Append` ("validate
      on write"); the command does not re-check it.
      `preflight --against` composes the two existing helpers through a new pure
      `edit.AgainstOriginal(orig, edited, maxGrowth)`, and requires exactly one SKILL_DIR --
      one original cannot describe several edits. `--max-growth` defaults to darwin's 1.5.
      The guards now **fail the command**; in the skill they only ever `echo`ed.
      Verified by running the resulting snippets verbatim, including the case that would have
      poisoned the gate: bases missing a `needs_judge` dim make `full_score` absent, so a bare
      `jq -r` yields `null`. The documented form guards on `has_full_score` and fails with a
      message. A row written by `log` reads back through `history` unchanged.

      **Outcome.** `scores.Aggregated(softs)` owns the arithmetic — `internal/scores`
      already knows what a base is (`MinBase`/`MaxBase`), so putting it there keeps the
      1-10 scale in one package. A zero mean clamps to `MinBase`: `10 × 0` rounds to 0,
      which is not a point on the scale, and failing every check genuinely *is* the floor.
      That clamp is legitimate where `internal/calibrate`'s refusal to clamp was not —
      there it would invent a judgment nobody made.
      Only `Behavioral()` cases are scored. The skill said "mean soft over all prompts";
      a `should_not_trigger` decoy has no good output to score, so counting it would lower
      the base for a skill correctly declining to fire. That is a correction to the skill's
      wording, not just an implementation choice.
      `--all` **reports rather than gates** — exits 0 even when cases fail, because most
      skills fail some checks and that is exactly why the base is below 10. Single-case
      `judge` keeps its `hard == 0 → exit 1` contract.
      **Real data changed the error handling.** Measured across the corpus: **0 of 183**
      skills carry embedded checks on every behavioral case, and the sampled skill yielded
      no derivable checks either — 0 of 4 scorable. Failing at the first case read as a
      per-case slip when the file as a whole specifies nothing to check, so every
      unscorable case is now named and the error counts them ("4 of 4 … add checks to
      those cases first"). A *partially* scorable file is refused too: a base averaged over
      an unstated subset is not comparable to one averaged over every case, and
      comparability is the point of the total it feeds.
      **`judge --all` is therefore unusable on the corpus as it stands** — which is the
      open "serialize derived checks back to test-prompts.json" item, now with a measured
      cost attached rather than a hypothesis.
      `calibrate` tells the two shapes apart by probing for the `judgments` key, the way
      `manifest.Parse` probes for `tool`. Trying the wrapper first cannot work: a
      single-line JSONL file is itself valid JSON and unmarshals into the wrapper with zero
      judgments, so it would read as empty. A malformed line is an error naming its number,
      never a skip — a dropped judgment skews the report toward whatever survived.

- [x] **`skillsaw scores` — emit the hash-bound scores file.** DONE (2026-08-08). The skill hand-writes JSON in
      a heredoc, extracts the hash with `awk` (`skillsaw hash` prints `<hash>  <path>`), and
      must pick `$AFTER` over `$BEFORE` at STEP 5. Evidence it is error-prone: writing that
      snippet on 2026-08-08 got it wrong twice — first embedding the path in the JSON, then
      with a `for kv in $BASES` loop that does not word-split under zsh and silently emitted
      a valid-but-wrong file. A `--skill DIR --dim 1=8 --dim 2=7 …` form captures the hash
      itself and removes all three hazards.
- [x] **`judge --all` — aggregate the dim-8 base.** DONE (2026-08-08). `judge` scores exactly one case
      (`--id N`), so the skill has the agent loop over prompts and compute
      `round(10 × mean(soft))` by hand. That number feeds `scores.json`, the FULL total and
      therefore the keep/revert gate, and every step of the aggregation is arithmetic.
      The agent still *produces* the outputs — that is execution, and irreducible — so the
      shape is something like `--from-test-prompts tp.json --all --outputs DIR/`, one
      output file per case id. Highest value of the survey, and the largest, because it
      needs a calling convention for multiple outputs.
      The `soft → 1-10 base` mapping stays here, not in `skillet/judge`: the rubric scale is
      skillsaw's policy, not a property of scoring an output.
- [x] **`preflight --against ORIGINAL` — fold in the F5 and F6 guards.** DONE (2026-08-08). STEP 4 does the
      no-op check (`[ "$BEFORE" = "$AFTER" ]`) and the size check
      (`[ "$new" -le $(( orig * 3 / 2 )) ]`) as bash arithmetic, and both only `echo` a
      warning rather than failing. `preflight` is already the structural gate at that exact
      point and already loads the skill; it just cannot see the original. Folding them in
      makes them exit non-zero like the structural checks beside them.
- [x] **`calibrate` should read JSONL directly.** DONE (2026-08-08). Phase 3 assembles the file with
      `printf '{"judgments":[%s]}' "$(paste -sd, - < judgments.jsonl)"`. Pure plumbing.
      Accepting the JSONL the loop already appends deletes the step rather than moving it.
- [x] **A machine-readable score for the loop.** DONE (2026-08-08), doc-only as predicted. The skill says "compute BASE = the FULL
      total, one decimal", i.e. scrape a table column for `BASE`/`OLD`/`NEW`.
      **Checked 2026-08-08: no code needed.** `eval --json` already emits
      `full_score` (and `has_full_score`, and `deterministic_score`) — the loop's most
      important number is available structured and the skill simply does not use it. This
      is a documentation change to `skillsaw-skill`, not a `--score-only` flag.
- [x] **The root `LongHelp` carried a hand-written command list that had drifted.** DONE (2026-08-08).
      `cmd/root/root.go` lists commands in its `LongHelp` *and* ff renders `SUBCOMMANDS`
      from the ones actually registered, so there are two lists of the same thing and only
      one of them can go stale. It has: the hand-written block is missing `preflight`,
      `calibrate`, `verified` and `changed`.
      The identical defect was removed from exegesis's root help (exegesis PR #16) by
      deleting the hand-written list and pointing at SUBCOMMANDS — one list that cannot go
      stale beats two that can. Same fix applies here; it is a deletion, not a rewrite.
      Noticed while adding `verified` and `changed` (2026-08-08), which made it staler.
      **Outcome.** `scores.Marshal` lives beside `Parse`, so the document has one
      definition and the round trip is the test that keeps it that way. Validation is in
      `Marshal`, not the command: an out-of-range base must be *unwritable*, not merely
      rejected later by whatever reads it back. The hash comes from the skill rather than a
      flag — accepting one would reintroduce the mistake the binding exists to catch.
      **Three lines of the loop disappeared, not just changed.** With `scores` reading the
      skill itself and `preflight --against` owning the no-op check, the `BEFORE`/`AFTER`
      hash captures at STEP 2 and STEP 4 had no remaining reader; their comment still said
      "needed by STEP 5", which was no longer true. Removed rather than left as
      instructions nothing acts on.
- [x] **A report of which cases still lack `checks`, scoped to a directory.** DONE:
      `skillsaw checks [--tree DIR] [--json]` (`cmd/checks`) walks the tree, loads each
      `test-prompts.json`, counts the `Behavioral()` cases whose `ChecksFor` returns none,
      and exits non-zero while any remain — a campaign gate like `verified`. Decoys are
      not counted. No `internal/` package: the pure core (`Load`/`Behavioral`/`ChecksFor`)
      is upstream, so it is a walk and a tally in the command. An unreadable
      `test-prompts.json` is noted, not fatal (presence is `verified`'s gate). Verified on
      the real corpus: e.g. `books/hashimoto` reports `table-driven-named-cases: 8/8` and
      exits 1. Original entry follows.
- [x] **A report of which cases still lack `checks`, scoped to a directory.** **Shipped as
      `skillsaw checks --tree DIR [--json]`**, which is exactly what this entry describes,
      down to counting behavioral cases only and exiting non-zero while any remain. It was
      struck through in prose and the checkbox was never ticked, so it read as outstanding
      for weeks. Original entry: Follows from
      the 2026-08-08 decision to author checks corpus-wide *with a directory limit* (see
      `skillsaw-skill/TODO.md`): the work is taken in parts, so "how much is left here"
      has to be answerable without re-running the ad-hoc measurement that produced the
      1275 figure.
      Shape: walk a tree, load each `test-prompts.json`, and report per skill how many
      behavioral cases carry no checks and none derivable — the same question `judge --all`
      answers for one skill, asked across many. Something like
      `skillsaw checks --tree DIR [--json]`, exiting non-zero while any remain so it can
      gate a campaign the way `verified` does.
      Count **behavioral** cases only (`Behavioral()`), not every case: a
      `should_not_trigger` decoy needs no checks, and counting decoys would make the
      remaining work look permanently unfinishable.
      Cheap to build — `testprompts.Load`, `Behavioral()` and `ChecksFor` already do all
      of it; this is a walk and a tally, with no new logic. Note it reports a *gap*, it does
      not fill one: writing checks is authoring work no tool can derive (see the entry
      below for the measurement behind that).
- [x] **MEASURED 2026-08-08: do not build this as written — it would write empty arrays.**
  *Recorded finding, not a task.* Nothing is planned here and nothing should be: the
  measurement's conclusion is that the work is authoring, which no tool derives. The residue
  it leaves — how much remains — is reported by `checks --tree`, closed above. Kept in full
  because the measurement is the reason a `--write-checks` command must not be built, and a
  deleted measurement gets re-proposed.
      Over the real corpus, of **1275 behavioral cases in 183 skills**: 0 carry embedded
      checks, `DeriveChecks` derives checks for **0**, and 1275 yield nothing at all. No
      skill has a single fully scorable case. A `--write-checks` command would write
      `"checks": []` 1275 times and change nothing.
      **The reason is in the data, and it is not a bug in `DeriveChecks`.** `expected`
      holds *activation* prose — "Invokes 10x9-cost-reliability, applies the 10x/9 rule: …",
      and in two of six sampled skills literally `"trigger"` or `"invoke"`. `DeriveChecks`
      only emits on an unambiguous signal (a heading name, a quoted phrase, a character
      bound, a named tool); it is behaving as designed by finding nothing.
      **These files were authored for activation testing, not behavioural scoring.** `type`
      + `prompt` is what `skillsaw activation` consumes, and that is all they carry. Writing
      checks is authoring work; no CLI can derive them from prose that does not state them.
      This is why `judge --all` cannot compute a dim-8 base for any skill in the tree.
      The exegesis-vs-skillsaw ownership question is moot for *derivation*. It returns only
      if a command is wanted to persist checks an agent **authored** — a different item, and
      one worth filing separately if that is the direction.
      Original entry, kept for context:
      **Serialize derived checks back to `test-prompts.json` — but as a `tests` command,
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

## SkillLens dimensions: shared, not private (2026-08-08)

- [ ] **dim 9 recommends the change that harms a wrong-shape skill, and it is reproducible.**
  `skilllens`' three detectors were validated on one class of skill — the discipline skill,
  where the failure is *skips a rule under pressure* and a boundary section is the right
  signal. skillet's package doc now says so (2026-08-23). This entry records the half a doc
  comment cannot reach: **the author reading a diagnosis.**
  **Reproduced, not inferred.** A purpose-built wrong-shape skill — a positive recipe, *"A
  release note IS, in order: 1… 2… 3… 4…"*, which is exactly the form superpowers prescribes
  for that class — yields:
  `target: Counter-examples / blacklist [P3] … add counter-examples`.
  The mechanism is `deriveBlacklist` setting a **base**, not a penalty:
  `blacklist_empty_base = 2` makes dim 9 the weakest dimension, and `Diagnose` targets the
  weakest. superpowers' head-to-head reports a prohibition list for this class as *worse than
  no guidance at all*, so the tool is not merely unhelpful here — it advises a change that
  makes the artifact worse.
  **Scoped by measurement: dim 9 only.** dim 5 was checked and is safe — the softening
  vocabulary is thirteen genuine hedges, and a conditional on an observable
  (*"if the response has a non-empty `next` cursor, page again"*) scores dim 5 = 10 with no
  flag. Do not widen this to the other SkillLens dimensions.
  **No fix is proposed, because none is available.** Gating dim 9 needs a predicate that
  distinguishes a skill wanting prohibitions from one spoiled by them, and none exists:
  `markdown.Doc` exposes `HasCodeBlock` and `HasOrderedList`, both orthogonal to failure
  class. `HasOrderedList` is the near-miss and must not be reached for — plenty of discipline
  skills number their steps.
  **Trigger: a derived predicate that distinguishes the two.** Until then this stays open
  honestly rather than being closed by inventing a classifier from prose, which is the shape
  refused for dim 3, dim 8 and the ruleset subject slot.

Source: `~/Documents/agent-orange/skillopt_changes_findings.md`, a survey of what
`microsoft/SkillOpt` and `microsoft/SkillLens` actually contribute to this family.

**Correction to a premise worth recording, because the README could be misread as
implying otherwise.** SkillOpt does **not** incorporate SkillLens — checked across the
whole repo and all 462 commits, the only SkillLens presence there is two hyperlinks on
its project page, and the four commits naming it are webpage restyling. The two
frameworks were fused *here*, by this repo. The README's attribution (SkillLens = the
3-dimension rubric, SkillOpt = the gate ratchet and rule-judge operators) is accurate and
should stay; what is worth adding is that skillsaw is the only place that fusion exists.

- [x] **Move dims 3/5/9's detectors to `skillet/skilllens` and delete the private copies.**
      DONE (on skillet v0.14.0). `checkFailure`, `checkSoftening`, and `deriveBlacklist`
      now read `skilllens.{FailureMechanisms,SofteningPhrases,BlacklistSections}` (dim 3
      filters `KindProse` for its inline-branch count and `KindSection` for the
      has-a-failure-section check; dim 9 takes the richest span's `Units`). The private
      failure regexes, the `Softening`/`BlacklistHeadings`/`FailureSections` Config lists,
      and the whole `matchesSignal` matcher are deleted; only the dim-3 `workflowMark`
      signal and the weights/1-10 mapping stay local.
      **The first attempt caught a real defect, which is why this waited on a release.**
      The score spot-check the entry demands showed ~30 skills moving because skilllens's
      plain `strings.Contains` missed the plural `## B — Boundaries` heading that
      skillsaw's ies-plural matcher caught. Fixed upstream (skillet#15, released v0.14.0:
      skilllens now uses the same word-boundary + ies-plural matcher). **Re-verified: the
      migrated binary scores all 277 corpus skills identically to the pre-migration
      binary — 0 base/penalty movement on dims 3/5/9.** The only remaining differences are
      informational flag text (dim-3 branch counts no longer add bare `fallback`/`兜底`;
      dim-5 softening is now case-insensitive), none of which move a base, penalty, or
      total. adh and skillsaw now share one definition of these three dimensions.
      `skillet/skilllens` exists with `FailureMechanisms`, `SofteningPhrases` and
      `BlacklistSections` over `*markdown.Doc`, plus `FailureSectionTitles()`,
      `SofteningTerms()` and `BlacklistTitles()` for `Config` to source.
      Two things to know when wiring it: the detectors return `[]Span` with a `Kind`, so
      dim 3 filters `KindProse` for its inline-branch count and `KindSection` for the
      has-a-failure-section check — it must not just take `len()`. And `Span.Units` carries
      the section's content count, so dim 9's "out-weighs the body" threshold stays here
      rather than moving up.
      Equivalence was proved before landing: the promoted detectors and the copies below
      agree on all four counts across the real 233-skill corpus, 0 mismatches. So deleting
      the private copies should move no score — if a score does move, the wiring is wrong,
      not the promotion.
      `internal/rubric` owns the `failureEN`/`failureCN` regexes and the `FailureSections`,
      `Softening` and `BlacklistHeadings` vocabularies. Nothing about them is
      skillsaw-specific — they are a mechanization of SkillLens's three tests and its
      "generic process advice" anti-pattern — but `internal/` makes them unreachable, so
      **adh reimplemented all three** (`failure-handling`, `actionable-specificity`,
      `boundary-section`, 60 of its 100 points). Two tools can now disagree about whether a
      skill encodes a failure mode, which is the drift `speclint` and `redlines` were
      promoted to end. skillet's TODO carries the shape; the second consumer already exists,
      so this is not speculative promotion.
      **The weights and the 1-10 mapping stay here.** Same boundary already drawn twice:
      `speclint` owns the description cap, skillsaw owns what a blown cap costs. adh weights
      the same three dimensions at 60 and skillsaw at 35, legitimately — they grade different
      artifacts.
      Once landed, `Config`'s four SkillLens-derived lists come from skillet and the
      remaining `DefaultConfig` fields (`FillerTails`, `Slop`, `CheckpointMarkers`) stay
      local, since dims 1/4/7 have no second consumer.
- [x] **Cite the dimensions' provenance where a reader will hit it.** DONE. `rubric.go`'s
      package doc now names dims 3 (failure), 5 (specificity), and 9 (blacklist) as the
      microsoft/SkillLens rubric (arXiv:2605.23899, 65-66% predictive accuracy), a
      mechanization of its three tests with the weights/1-10 mapping kept local; the
      `Dimensions()` doc cross-references it. This matters for the judge tier:
      `skillsaw-skill` hand-scores dims 3 and 5, which now have their stated source in the
      code, not only the README.
- Deliberately NOT adopted from SkillOpt: **`compute_semantic_density`**
      (`skillopt/evaluation/gate.py`), the one genuinely new dimension on its validation
      gate. It counts leading imperatives (`MUST`/`ALWAYS`/`NEVER`/…) and adds
      `0.05 × density` to the gate score. Under a strict-`>` ratchet that rewards imperative
      *tone* rather than content — an edit can win the gate by sprinkling `MUST` — which is
      the "instruct, don't enforce" shape this repo rejects elsewhere. Note SkillOpt's own
      vendored sleep gate (`skillopt_sleep/gate.py`) omits it too. Its provenance is
      `49a5b61 fix(gate): resolve issue #100`, not SkillLens.

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
- [x] A timeseries **regression gate** for rubric quality across skill versions. Done
  2026-08-23 as `skillsaw regression --skill NAME`, reading `results.tsv` through
  `internal/auditlog` and calling `timeseries.Detect`. This is the check a fixed threshold
  cannot make: "above 80" says nothing about a skill that was at 92 and is now at 84.
  `auditlog.Series` is the pure half, and it **skips rows whose new_score is not a number
  rather than reading them as zero** — a baseline row records `-`, and folding that in
  would manufacture a collapse out of a run that measured nothing.
  `Verdict.Compared == false` prints as `NOT COMPARED` and exits 0. It is an absent opinion,
  not a pass, and the two are printed differently on purpose; failing the first run of a
  metric is the failure `MinHistory` exists to prevent, since the only fix is to stop
  measuring. Verified against dropping the `Compared` branch, which makes the
  short-history cases read as passes. `--tolerance` defaults to 0 but is documented as a
  setting to make deliberately: zero tolerance on a noisy metric is how a gate gets
  switched off. Original entry: — a CI check
      that fails on a quality *drop* vs. the recent baseline, not just a fixed threshold.
      **The math now lives in `skillet/timeseries`** (2026-08-07, available since the v0.11.0 bump): wanting it
      here *and* in exegesis was the 2nd consumer that promoted it.
      `Detect(history, current, Config) Verdict`; what remains here is reading the history out
      of `results.tsv` (`internal/auditlog` already parses it) and choosing a `Tolerance`.
      Two things to get right when wiring it: `Verdict.Compared` is false when history is too
      short and must **not** be read as "passed" — a gate that fails a metric's first run can
      only be fixed by not measuring; and `Tolerance` is absolute, in the rubric's own units,
      so a zero tolerance calls any dip below the mean a regression.
      Correction to the note below: unified-thinking's `DetectRegression` is *not* a
      rolling-window baseline — it uses the single most recent entry, treats a zero baseline
      as an absent one, and divides by the baseline. skillet's version fixes all three.
- [x] **Report whether dim 8 can be scored at all.** DONE (2026-08-15). `checkScorable`
      flags a skill whose behavioral cases specify no checks, and never docks for it.
      This was silent, and it is the largest hole the rubric had: the dim-8 base comes from
      `judge --all` over each case's checks, so a skill nothing can score read exactly like
      one that scored perfectly. Dim 8 is weight 23. Corpus-wide, **183 skills have a
      `test-prompts.json` with no case specifying checks and 50 have no readable file at
      all** — every skill in the tree, unscoreable on its heaviest dimension, without
      saying so. No score moved: 0 of 233.
      It asks `testprompts.ChecksFor`, the same call `judge --all` makes, so the flag
      cannot report a case scorable that `judge` would then skip. Partial coverage gets its
      own message rather than folding into "has checks" — a base over part of a skill's
      cases is a mean over part of its evidence, and nothing downstream marks it partial.
- Note: **gate on the artifact, flag on the inputs.** The rule the dim-3 and dim-8 work
  settled between them, recorded because the two look alike and are not.
  **Dim 3 is a category error**, so it gets a derived predicate that suppresses the
  deduction. `HasCodeBlock` is intrinsic to the artifact: a selection document that
  executes nothing has no runtime failure to encode, and no edit makes it otherwise, so
  docking prices a defect that cannot exist.
  **Dim 8 is missing input**, so it gets a flag and no suppression. `expected: "invoke"`
  is a property of a *different file* describing work not yet done — every one of those
  skills has outputs worth checking, and rewriting one `expected` makes it scorable.
  **Do not give dim 8 a category input.** It would launder unfinished work as
  inapplicable, making 48 skills' missing dim 8 look permanent when the opposite is
  true and is the whole reason fixing them is worth doing. It would also reintroduce
  the self-report already rejected for dim 3 — authored alongside the very prompts it
  excuses — and there is nothing to derive it from: the only available signal is the
  absence itself, so the gate would be circular.
  The deterministic floor must never read as "this skill is fine" when the truth is
  "nobody has measured it".
- [x] **Gate dim 3's penalty on `markdown.Doc.HasCodeBlock`, and carry dim 4's
      applicability.** DONE (2026-08-15, on skillet v0.15.0).
      **dim 3:** the penalty now applies exactly when `branches == 0 && doc.HasCodeBlock`. A
      matching section title no longer buys immunity — it is a container, not an encoded
      mechanism — and the skills it was protecting are the ones the predicate now exempts on
      a sounder signal. **The old `HasOrderedList || workflowMark` proxy was deleted rather
      than kept alongside**: answering "does this dimension apply" from two signals in two
      places is the Information Leakage red flag, and it let the weaker proxy silently decide
      every case they disagreed on. `golangci-lint`'s `unused` caught `workflowMark` the
      moment the second answer went away.
      **dim 4:** nothing to gate — it docks nothing — so the predicate bought a *better
      question for the judge* instead. One sentence reached 230 of 233 skills, asking the
      same thing about a runbook and a decision framework; it now splits **149 / 81** between
      "executes nothing to checkpoint; likely not applicable" and "runs commands, so judge
      whether it needs them". Same `NeedsJudge`, same zero penalty, strictly more
      information.
      **Re-scored:** 44 of 233 totals moved, every one by exactly −3.6 (penalty 3 × weight
      12 ÷ 10); dim-3 penalties went 10 → 54; **no skill lost a penalty and no penalty
      changed in any other dimension**. The penalised set matches the prediction "has a code
      block and no inline branch" exactly — 54 predicted, 54 actual, zero discrepancy either
      way.
      **No `units > N` threshold was added.** That was the original proposal; gating makes it
      unnecessary, and no threshold is calibrated. Recorded so it is not re-proposed.
- Superseded framing (the measurement and the argument still hold):
  **Gate dim 3's penalty on a derived "does this skill execute anything" predicate.**
  Dim 3's section credit is title-based (`"boundary"` is in both
  `skilllens.FailureSectionTitles()` and `BlacklistTitles()`), so a `## B — Boundaries`
  heading satisfies it whatever is written underneath. Measured 2026-08-09 over the
  233-skill corpus: **154 skills (66%) have zero inline failure branches** and pass on
  the heading alone; only 10 are docked anything, and dim 3 is the *only* dimension in
  the whole rubric that ever produces a nonzero penalty.
  A bare `units > N` requirement is the wrong fix — at `units > 3` it more than triples
  the docked set from 10 to 36, and 10 of the additions are a category error: skills that
  execute nothing (`ml-simplest-model-baseline-first`, `strategic-before-tactical-ddd`,
  `microservices-dont-fix-coupling`, `welc-tended-untended-systems`) have no runtime
  failure to encode.
  **CLI wrappers are not a category error and must keep being docked** — `gh-cli` has 87
  shell blocks and 0 failure branches, `rumdl` 13 and 0, `vale` 11 and 0. That is
  precisely the defect dim 3 exists to catch. Gating on **`markdown.Doc.HasCodeBlock`**
  (shipped in skillet 2026-08-14) cuts **36 → 26** and suppresses exactly the right ones.
  **Figures corrected 2026-08-14.** The first pass reported 144/34/20 and named several
  `grpc-*` skills as executing nothing. Both were measurement defects: a `^```` regex
  misses a fence indented inside a list item — common in this corpus — and the probe
  parsed frontmatter as body. Use `HasCodeBlock` rather than a local fence scan when
  re-measuring; that is what it is for.
  **Derive the predicate; do not read it from frontmatter.** A declared `category:` is a
  self-report written by the same generator being measured, letting a skill opt out of
  its own worst dimension. The corpus has no type field today anyway (only `name`,
  `description`, `tags`, `allowed-tools`).
  **Suppress the penalty, always emit the flag.** A mis-derived category that silently
  hides a real gap is worse than the current over-generous check, which at least fails
  visibly. This is what dim 4 already does.
  Dim 4 needs the identical predicate (230 skills flagged "judge if this skill type
  needs them", penalty 0 across all 233), and adh needs it for two dimensions — see the
  promotion note in `../../git/skillet/TODO.md`.
- [x] **Revisit dim 3's `-3`, now that it fires on 54 skills rather than 10.** DECIDED and
      DONE (2026-08-15): **keep the `-3` unchanged, and fix the double-count instead.**
      **Why the penalty stays.** Dim 9's flag-don't-dock precedent exists for a dimension
      whose *applicability* is uncertain; dim 3's no longer is — the gate made its population
      correct by construction, which is exactly what changed. And dim 3's penalty is the
      **only penalty the whole rubric produces** (54 skills, zero in the other eight
      dimensions), so converting it to a flag would leave the deterministic floor unable to
      fail at all. A lint-style floor that always returns the same number is not a floor.
      Retuning the magnitude was rejected as swapping one uncalibrated constant for another:
      nothing calibrates 1 versus 2 versus 3, and `skillsaw calibrate` has no recorded
      judgments to run on.
      **What was actually wrong was double-counting**, and it was not about size. Dim 3 is
      `needs_judge`, so `final = clampScore(base − penalty)`: a judge who reads the flag
      "runs commands but encodes no failure branch" and lowers the base has the skill pay for
      the same defect twice. `fullScore` now uses a judge-supplied base **as-is** and applies
      the penalty only to dimensions the judge did not score, where the deterministic check
      is the only reading of the skill there is.
      Demonstrated on `gh-cli` with a full base set: **72.7 with the fix, 69.1 stacking** —
      exactly 3.6, the penalty times its weight. The deterministic floor is untouched: 0 of
      233 corpus scores moved, and `has_full_score` is still false everywhere because no
      bases exist yet, so the change is latent until a judge runs.
      Incidental: `clamp(v, lo, hi)` became `clampScore(v)` once `unparam` noticed every
      caller passed the same bounds. The 1-10 scale is the rubric's, not a caller's choice.
- Note: skillsaw's TP/FP/TN/FN activation confusion matrix (with Wilson intervals) is
      already more rigorous than unified-thinking's binary exact/contains/tolerance
      evaluators — nothing to adopt there. Calibration is the one real gap; its keyword
      bias/fallacy detectors are the "instruct, don't enforce" shape skillsaw rejects.

## Agent-Red Survey (2026-08-15)

Source: a survey of `~/Documents/agent-red` (26 agent-tooling projects). The one
substantial finding for skillsaw is a single project's treatment of its own rubric, checked
against `AgentLint/standards/` rather than its README.

- [x] **Make the rubric data, not a Go table.** Done 2026-08-23 as
  `internal/rubric/rubric.toml`, **embedded rather than loaded from a path** — the plan's
  one departure from this entry, and the reason is in the entry itself.
  Everything stated here arrives without a load path: a reviewable diff, a `note` per
  weight recording why it has that value, and an edition derived from the bytes. And a load
  path is the single change that reopens the vector this entry flags and does not resolve —
  the tool and the thing it grades live in one repository, so a rubric read from the tree
  under evaluation is a rubric the optimiser can write. Compiled in, the vector is shut **by
  construction** rather than by a guard someone has to keep remembering, which is stronger
  than the "refuse to read one from inside the tree" this entry guessed at. The cost is a
  recompile to change the rubric; given a repo where the gate and the gated thing live
  together, that is the feature.
  **Both recorded traps avoided, and the strict loader still earns its keep** because the
  embedded bytes go through it on every run rather than behind a flag nobody sets. TOML,
  with `MetaData.Undecoded()` rejecting a misspelled key by name. No fallback to compiled
  defaults on any error: `Load` returns an error and no rubric, and `mustLoadEmbedded`
  panics, which is right exactly once — the bytes are compiled in, so a failure is a
  malformed source file rather than anything a caller can recover from, and
  `TestEmbeddedRubricLoads` catches it at build time.
  Validation is what the compiler used to do: nine dimensions, each number once, weights
  summing to 100, every response defined, no empty list — **and no dimension without a
  note**, since a weight with no warrant is a magic number that has merely changed file.
  Moved: dimensions, the three word lists, and the scoring bands (`>= 3` markers, the
  counter-example unit bands, the checkpoint base). **Not moved:** the per-defect penalty
  amounts, scattered through eight check functions. `scoringRevision` still covers those and
  its comment now says exactly that rather than implying the edition is complete.
  `Edition()` moved onto `*Rubric` and hashes the document verbatim — so a comment edit
  moves it too, which is the deliberate trade against hand-picking scoring-relevant fields
  and silently omitting whichever is added next.
  **Faithfulness is the arbiter, and it held:** all four of `TestScoresAreStable`'s golden
  values are unchanged, and the whole suite passed the extraction without edits. Verified
  against a planted weight change (panics naming `weights sum to 101`) and a planted
  threshold change (fails the golden and edition tests). Original entry: *Carries a constraint from "A change that
  loosens a gate may not score as improving it": the rubric being a Go table is the only
  reason a candidate edit cannot relax its own gate today. Loading the rubric from the tree
  being scored opens that vector, so this entry has to answer it — most likely by refusing
  to read a rubric from inside the tree under evaluation.* `rubric.Dimensions()`
  (`internal/rubric/rubric.go:111`) hardcodes the nine weights, and their provenance is a
  code comment — "the reconciled table from spec D1 (dim6 = 5) so the nine weights sum to
  exactly 100". The invariant is asserted in prose and the warrant for each weight is
  unrecorded. `AgentLint` splits the same job across three files joined on a check ID:
  - `standards/weights.json` — dimension weights *and* per-check weights (`"S6": 3` —
    hardcoded secrets, weighted highest because most dangerous, said in a `note`).
  - `standards/reference-thresholds.json` — every threshold with the empirical basis that
    produced it, e.g. `"IMPORTANT": {"reference": 4, "source": "Anthropic 265 versions:
    12→4"}`.
  - `standards/evidence.json` — 58 check entries, each `{dimension, name, scope, fix_type,
    evidence_sources, evidence_text}`, over a `sources` registry that **grades its own
    citations** `primary-data` / `peer-reviewed` / `case-study` / `industry-practice` and
    annotates the weak one honestly: *"n=1 case study, useful reference point not universal
    benchmark"*.
  This is the manifesto's stated requirement — maintain local decisions about how to
  prioritize and trade off, *with provenance* — already implemented. A weight change
  becomes a reviewable diff carrying its own justification instead of an edit to a literal.
  **Determinism is unaffected:** the table is loaded, not computed, and `identity.Hash`
  over the standards files pins which rubric produced a score.
  **Two traps, from a commissioned gap report that reached this same entry independently
  (`~/Documents/agent-green/FPF/skillsaw_topten.md`, Gap 1) and proposed both of them.** It
  is the one accurate finding in seventy across that report — correctly located at
  `rubric.go:111`, correctly quoting the hardcoded slice — and its implementation plan is
  where the value is, as a list of what not to do.
  - **It falls back to the compiled defaults on a read error *and* on an unmarshal error.**
    A mistyped weights file would then score against yesterday's rubric while reporting
    success, which is the silent-downgrade failure this family refuses everywhere else. A
    malformed standards file must be a hard error naming the file and the line. The
    fail-closed direction here is *refuse to score*, not *score with defaults*.
  - **It proposes JSON.** `AgentLint` uses JSON and that is the one part of its design not
    to copy: gnosis measured this and moved its vocabulary to TOML because `toml.Decode`
    reports `MetaData.Undecoded()`, so a mistyped key is caught **by name**, where decoding
    JSON into a struct cannot distinguish a typo from a key the tool was not built to read.
    A threshold somebody believes they changed is the expensive failure, and it is exactly
    the one JSON hides. Use TOML, and reject unknown keys.
  Both traps share a shape worth naming, since this entry is the one that will be
  implemented: **externalising a rubric moves the failure from compile time to load time,
  and the load path is where the strictness has to be paid back.** A lenient loader gives
  up the property that made the hardcoded table trustworthy.
- [x] **Their thresholds file states our charter better than we state it.** Done with the
  item above: *"Reference values from empirical data. NOT enforced thresholds — skillsaw
  measures and compares, users decide"* is the first thing in `rubric.toml`, ahead of any
  number. The entry's point is that the file holding the thresholds is where the temptation
  to harden a reference into a gate actually arises, so it is there and not in a README
  nobody opens while editing a weight. Original entry: Its header
  reads: *"Reference values from empirical data. NOT enforced thresholds — AgentLint
  measures and compares, users decide."* That is canonizer's findings-not-scores rule,
  applied to thresholds, in the file the thresholds live in. Worth copying verbatim as the
  first key of whatever we externalize, because the file is where the temptation to harden
  a reference into a gate will actually arise.
- [x] **Externalizing weights is how you find the bug.** `weights.json` carries a note
  recording that its dimension weights deliberately sum to **1.10**, not 1.0, because
  `D1-D3` and `SS1-SS4` "were previously emitted by deep-/session-analyzer but silently
  dropped because no dimension owned them" — a coverage defect that only became visible
  once the weights were data laid out beside the check IDs. Ours sum to exactly 100 and the
  property is checked by a test, which is stronger; but nothing checks that every
  *deterministic penalty* skillsaw computes is owned by some dimension. That is the same
  class of defect and it is worth a test either way.

  Checked, and **their exact defect cannot occur here**: a check is handed the `*DimScore`
  of whichever dimension dispatched it, so a penalty is structurally owned and cannot be
  orphaned. No orphaned check function exists either — all eight are dispatched. The
  *reachable* defect is different and was unguarded: an arm under the wrong number in
  `applyChecks`' `switch`, which puts one dimension's finding on another's score while every
  existing test still passes, because they each assert on the dimension they happen to look
  at. `TestEachCheckWritesOnlyItsOwnDimension` closes that — one fixture per dimension, each
  asserting its target recorded something and that no judge-only dimension did. Verified
  against dispatching dim 9's check under `case 2`: eight subtests fail and name both the
  wrong owner and the finding that moved.
  Dim 2 has no deterministic check, which is deliberate and already recorded ("judged
  outright" in the `Dimensions` comment). The test derives the judge-only set rather than
  hardcoding it, so adding a check for dim 2 without adding a fixture fails rather than
  passing unnoticed. No `Deterministic bool` was added to `Dimension`: no consumer today.
- [x] **Certainty is computed but not mapped to an action.** DONE 2026-08-15 on skillet
  v0.16.0. `Diagnosis` carries `finding.Action` — the shared vocabulary, not a local string,
  which is why the axis went into skillet rather than into canonizer and here separately.
  A fixed table, no measurement: dims 1/4/6 are `guided` (a tool can propose a shorter
  description, a checkpoint marker, a link target; only a person knows it still says what
  the skill does), and 2/3/5/7/8/9 plus P0 runtime drift are `human`.
  **Nothing is `automatic`, and that is a finding rather than an omission.** Every defect
  skillsaw reports is closed by editing prose whose correctness depends on what the skill
  means. canonizer reached the same conclusion independently over a different artifact,
  which is mild evidence the vocabulary is right: `automatic` earns its place as the option
  neither tool takes.
  **Orthogonal to `Priority`, and neither is derived from the other** — a P0 runtime hit
  needs a person, and a P3 frontmatter cap does not become automatable by being unimportant.
  Unset where there is no target (nothing scored, or every dimension healthy): naming an
  actor to fix nothing would be a false instruction, the same reason `finding` has no
  `ActionUnknown`.
  `edit.AgainstOriginal`'s two defects are `human` too. **The `speclint`/`redlines`
  diagnostics `preflight` composes are deliberately left unclassified** — they are skillet's
  checks, and setting this repo's judgement on another package's output would drift the
  moment skillet classified them itself.
  No score moved: 0 of 233.
  Original entry: `eval` already separates
  `DET.SCORE` (deterministic lower bound) from `FULL`, and names the `NEEDS-JUDGE`
  dimensions — a real certainty distinction, and a better one than a label. What `diagnose`
  does not say is *who acts*: `agentsys` grades every finding HIGH / MEDIUM / LOW meaning
  "safe to auto-fix" / "needs context" / "needs human judgment" (derived from testing over
  1,000+ repos), and `AgentLint` carries `fix_type` (`guided` / `assisted`) per check. Since
  `skillsaw-skill` drives the hill-climbing loop and picks one edit per round, "is this
  finding safe to apply unattended" is a decision the loop is currently making implicitly.
  Cheap: a fixed classification per check, no new measurement.
- [x] **A guard the optimizer cannot rewrite.** Done 2026-08-23, and it was not lower
  priority: `regression`, shipped two days earlier, was **already wrong in this exact way**.
  It averaged a skill's recent scores with no notion of which rubric produced them, so a
  history spanning an edition change compared answers to two different questions — the same
  defect the edition was introduced to fix one layer down.
  Two halves. `auditlog.Row.Rubric` records the edition, defaulted by `log` to the current
  one so a caller cannot forget and cannot supply a stale one. And `regression` refuses to
  compare across editions: `auditlog.Editions` reports the distinct ones in a history, and
  more than one routes to the existing `NOT COMPARED` third state rather than a new
  verdict. An edit to the rubric that raises every score can no longer be laundered through
  the ratchet's own history, because the history declines to average across it — and
  equally, a real regression is not reported on the strength of numbers that are not
  comparable.
  **The column is tenth and rows read with nine or ten.** Promoting `Columns()` to a
  required ten would have failed every log already on disk, and a format change that
  invalidates the history is a poor way to start recording history properly. Empty means
  the row predates the column, which is not the same as matching — the same fail-closed
  reading `scores.Entry.Rubric` uses.
  The entry proposed hash-pinning the standards file in the audit row; that is what this
  is, with the edition standing in for the file hash since the rubric is compiled in.
  Verified against ignoring the recorded edition, which makes the mixed-history case report
  a verdict again. Original entry: `gate` runs keep-or-revert
  for skill self-improvement. `4x` pairs its self-evolution loop with a value gate carrying
  explicit anti-hack checks and a self-modification scope guard. The analogous question
  here: nothing structurally prevents an edit to the rubric that raises every score, since
  the gate and the thing being gated live in one repo. Externalizing the rubric (first item)
  makes this checkable — hash-pin the standards file in the ratchet's audit row so a score
  improvement accompanied by a rubric change is visible rather than inferred.
- Deliberately NOT adopted: `Graft`'s benchmark framing is methodologically right (162 runs
  where only the context differs) but it measures agent task outcomes, which is `adh`'s
  axis, not skillsaw's — skillsaw scores artifacts and already has `calibrate` plus the
  activation confusion matrix for its own accuracy. Nothing in the survey improves on
  those.

## Agent-Blue Survey (2026-08-15)

Source: a survey of `~/Documents/agent-blue` (22 projects), including the two upstreams this
tool is built from. Checked against the code; one earlier claim is retracted.

- **Retraction — SkillOpt's validation gate is fully absorbed, and ours is better.** An
  earlier pass floated adopting `skillopt_sleep/gate.py`. Compared line by line: it has
  `GateResult{action ∈ accept_new_best|accept|reject}` and
  `select_gate_score(hard, soft, metric ∈ hard|soft|mixed, mixed_weight)`. `skillet/ratchet`
  has all of it — same three actions, same `SelectScore` projection with the same clamping,
  same `BestScore`/`BestStep` — **plus** the separate measured axis (`Delta`/`Status`) from
  Adopt-5, an error return on an unknown metric where the Python raises, and a documented
  "ties reject and do not promote". Nothing to take.
- [ ] **SkillLens's two metrics measure the thing dim 8 currently asks a model to guess.**
  From `agent-blue/SkillLens` (`docs/index.html`, the paper's Table 1 framing):
  - **Extraction Efficacy (EE)** — for a fixed *extractor*, average Δ across all targets:
    how reliably the extractor produces useful skills.
  - **Target Evolvability (TE)** — for a fixed *target*, average Δ across extractors that
    distill from its own trajectories: how much that model can improve from skills grounded
    in its own experience.
  Both are a **Δ in downstream task performance**, not a property of the artifact. Our
  rubric is entirely artifact properties — and dim 8, "Real-world test performance", carries
  **weight 23, the largest of the nine, and is judge-scored**. That is a model being asked
  to estimate a Δ that SkillLens measures directly. Two consequences worth separating:
  (a) EE is a property of `book2skill`, not of any one skill — we have no per-producer
  metric at all, so a regression in the producer is currently invisible except as drift in
  aggregate skill scores; (b) TE is a property of the consuming model, which is the axis
  that would tell us whether Claude/Gemini/Qwen benefit differently from the same tree.
  **This does not belong in `eval`** — skillsaw never calls a model, and measuring Δ
  requires running one. It belongs beside `calibrate`, consuming outcome data produced
  elsewhere, exactly as `calibrate` consumes judge scores today.
- [x] **A verdict taxonomy richer than accept/reject, for the axes that actually have
  variance.** Done 2026-08-23, both halves — and **measuring 60 real skills first rewrote
  both of them.**

  **`REPLICATION-MISSING` was a live defect, not a taxonomy nicety.** A skill with no
  `test-prompts.json` made `activation` abort the whole run with a usage dump, and so did a
  directory with prompts but no `SKILL.md`. Both were hit by accident while gathering
  evidence: one missing file discarded a 50-skill batch. Absence of evidence was not merely
  collapsed into failure, it was collapsed into a fatal error that threw away every result
  already computed. Now each such skill is reported `UNMEASURED` with its own cause — "no
  test-prompts.json" versus "no readable SKILL.md" — the run continues, and the exit is
  still 1, *after* reporting rather than instead of it.

  **`CEILING-NEEDS-HARDER-DATA` as filed has no instance.** The entry says a saturated set
  is "reported today as a good score". Over 60 skills with real prompt files: 60 targets of
  which **0 fire**, 38 distractors of which **0 fire**, 42 skills with no type-tagged
  prompts at all, and all 60 already gating `unresolved` — so the noise floor added two
  sessions ago catches precisely the case the entry worries about. Nothing is saturated;
  nothing fires.

  The real and silent state is narrower and sits in the same numbers: **a decoy that never
  fires is not evidence of precision.** `FP == 0` is either perfect precision or trivial
  decoys and the matrix cannot tell them apart, while a skill with *zero* distractors
  printed `FPR 0.00 (0/0)` — a precision figure computed from nothing, in the column beside
  ones computed from something. `noise.EvidenceIn` now reads which halves of the matrix a
  sample supports, and the caveat names the repair, because "no decoys" and "easy decoys"
  want different work. Reported, never gated: `--min` stays the only thing that fails a
  run, and a clean FP over a genuine decoy set is a real perfect score.

  On the corpus it splits exactly along the measured line: 42 "nothing here was measured",
  18 "no decoy ever fired".

  **No ninth word was added.** This is the same idea the codebase already spells eight ways;
  the doc comment lists them and points at `EVIDENCE.md`, which gained the row this makes
  true. Original entry: `agent-blue/cc-thinking-skills/evals/run-replication.js` exposes a **pure,
  I/O-free `verdict()`** over six values: `ELEVATE`, `DIRECTIONAL-NOT-REPLICATED`,
  `NO-LIFT`, `QUARANTINE-REDIRECT`, `CEILING-NEEDS-HARDER-DATA`, `REPLICATION-MISSING`.
  Our *Deliberately NOT absorbed* note above correctly rejected cluster bootstrap / Holm /
  power analysis on the grounds that a deterministic rubric score has no variance to test —
  that reasoning still holds and is not reopened here. But two of these six are not about
  variance:
  - **`CEILING-NEEDS-HARDER-DATA`** — the eval set is saturated, so the result carries no
    information. `activation` is proportion-based and already reports Wilson intervals; a
    test-prompt set on which every skill scores perfectly is a real and currently silent
    state, reported today as a good score.
  - **`REPLICATION-MISSING`** — absence of evidence gets its own verdict rather than
    collapsing into failure. Same discipline as `skillet/timeseries`'s `Verdict.Compared`
    and `ratchet`'s refusal to promote on a tie.
- [x] **Evidence-as-authority, and an explicit list of claims we are allowed to make.**
  Done 2026-08-23 as `EVIDENCE.md` plus `TestNoUnauthorizedClaims`, which is the half that
  survives: a list of claims nobody may make is a list nobody re-reads, so the rule is
  checked against the repository's own shipped documents rather than trusted.
  The document states what each output claims and what it does not, in a table, and says
  outright that nothing here measures lift. It also records the two measured blind spots
  from the mutation table, on the principle the source names — publish them rather than
  hide them.
  **`validated` is deliberately not matched as a bare word.** It flagged three uses on the
  first run, all of them ordinary input validation (`a validated constructor`, `a status
  validated on write`). A check that cries wolf is a check somebody deletes, which §9 lists
  as a red flag by name, so the claim sense is matched by phrase and the engineering sense
  is left alone. `EVIDENCE.md` says so, to stop the next reader "fixing" the list.
  Verified against a planted "proven to improve skills" in `README.md`. Original entry:
  `cc-thinking-skills/analysis/AUDIT.md` opens with a table pinning `analysis/evidence.json`
  by SHA-256 *and* the registry it references by SHA-256, then states: "If this narrative
  disagrees with the JSON, **the JSON wins**", a global disposition of
  `no_automatic_elevation`, and "No public 'proven / validated / improves / eval-informed /
  auto-invoke' claim is authorized. Inferences are labeled." Its headline is that **zero of
  28 skills hold a replicated ELEVATE verdict — published rather than hidden**. Relevant
  because a `skillsaw eval` number is exactly the sort of figure that escapes into a README
  as "validated". Pairs with the AgentLint externalization item in the agent-red survey: once
  the rubric is data, hash-pin it beside the score so a number is inseparable from the rubric
  that produced it.
- [ ] **Diagnose says what is weakest; nothing says why it failed in use.**
  `agent-blue/hermes-agent-self-evolution` runs GEPA over SKILL.md files, and its
  distinguishing property is that it **reads execution traces to understand *why* something
  failed, not merely that it did**, then proposes targeted improvements — with constraint
  gates (tests, size limits, benchmarks) between candidate and merge, terminating in a PR.
  `diagnose` reads the artifact and reports the weakest dimension; it has no failure input.
  The deterministic half of that input is available without a model — see the session-mining
  item filed in adh's TODO, which is where trace harvesting belongs.
- Note on loop design, not a work item: `agent-blue/darwinian_evolver` reports that
  population-based search is **resilient to a noisy evaluator and an unreliable mutator** — a
  mutator that improves only ~20% of the time still drives progress, because selection does
  the work. `skillsaw-skill` climbs with a single candidate per round, which makes every edit
  load-bearing and every bad edit a wasted round. Not an argument to adopt a population (the
  ratchet's determinism is worth more), but it is the reason a cheap-and-frequently-wrong
  edit generator is a viable design and not obviously worse than an expensive careful one.
- Deliberately NOT adopted: `SkillOpt`'s epochs / batch-size / learning-rate vocabulary
  (an apt description of what `skillsaw-skill` does by hand, but renaming the loop buys
  nothing without the scheduler underneath it); `PolyBrain`'s multi-model synthesis and
  `super-hermes`'s prism skills (both are agent-layer, and skillsaw never calls a model).

## Agent-Fuschia Survey (2026-08-18)

Source: a survey of `~/Documents/agent-fuschia` (26 repositories). One substantial item,
and it is the strongest version yet of a question this repo has circled twice.

- [x] **The rubric checks are evals, and nothing proves they would catch a defect they
  lack a fixture for.** `agent-fuschia/evalmut` is mutation testing for eval suites, and its
  opening question is the one to answer here: *"Your eval suite passes. Does it actually
  check anything?"* It takes a case the grader passed, injects a **known** defect, and
  reruns; a grader that still passes has a **hole**. Two properties make it more than a
  fault-injection harness:

  Done 2026-08-23 as `TestKnownDefectsAreNoticed`, which injects a known defect into the
  **artifact being graded** and asks whether any dimension notices. Not source mutation:
  that tests "is this test wired up", which the per-change planted controls already do one
  at a time, and it is not the question the entry asks. Injections come from what this
  corpus was measured to contain, not from reading the checks — writing them from the
  checks would guarantee they all pass and prove nothing.

  **The first measure was wrong and the run said so.** "Noticed" started as "some
  dimension's score dropped", which filed two cases as holes that are nothing of the kind:
  dim 8 on a case with no checks, and dim 3 on a failure heading over a body that executes
  nothing. Both *report* and decline to dock — dim 8 is NeedsJudge and dim 3 will not charge
  for a runtime failure in something with no runtime. The measure is now three-valued
  (`docked` / `reported` / `blind`), which keeps the distinction between a rubric that is
  blind and one that names a defect and defers the price to a reader.

  **Two genuine blind spots, recorded rather than closed:**
  - **Assertions that assert nothing.** A case whose `expected` holds activation prose and
    whose check repeats a word from it is fully scorable and says nothing about behaviour.
    Dim 8 counts scorability, not meaning.
  - **Boundary items that are filler.** Dim 9 counts units; three rows of filler score as
    three real counter-examples. This is the defect `Response = additive` already records.

  Neither is filled, deliberately: a deterministic proxy for either would be a new
  feedable dimension, which is what the first one already is. Both are declared in the
  table with a reason, and the test fails if one *closes* without the declaration being
  updated — a hole that gets fixed must be recorded as fixed.
  - **The defects are mined from documented real-world eval failures, not invented.** Its
    examples are the shape our deterministic checks are exposed to — a `contains("42")`
    check passing *"I did NOT reach 42"*; a presence check on a `count` field saying nothing
    about whether the value is a number or the string `"three"`; a refusal check fooled by a
    reply that refuses in its first line and delivers the harm below.
  - **It is two-sided.** A MISSED mutation is a defect class the eval is blind to; a FLAGGED
    one is a correct-output class it wrongly rejects. Holes are classified `blind` vs
    `coverage-gap`. Our existing mutation testing (`skilllens` detectors, `HasCodeBlock`'s
    five mutants) checks only the first direction.
  Concretely here: `redlines`, the dim 4 checkpoint-marker count, dim 6 link reachability,
  dim 9 blacklist-section presence, and every deterministic penalty are graders over a
  string, and each is exactly the kind of check `contains`-style mutation defeats. A
  standing operator battery mined from *our own* corrected findings — the 233-skill corpus
  has a history of them — would say which dimensions are load-bearing and which are
  decorative. That is also the honest input to the rubric-externalization decision already
  open above: a weight defended by an evidence citation is better than a bare literal, and a
  weight whose check demonstrably catches nothing is better deleted than re-weighted.
- Corroboration, no work implied. `agent-fuschia/gradecore` reaches this repo's charter
  independently — "every grade is a pure predicate over a string, so it reproduces exactly…
  **No second model grades the first**" — and `agent-fuschia/eval-history` states the case
  for regression-relative gating better than our own note does: "An eval score tells you how
  the system did *today*. It can't tell you that yesterday's change made things worse — and
  'worse' is the only thing you actually need to be told." `skillet/timeseries` is that,
  already ours.
- Deliberately NOT adopted: `agent-certlab`'s seeded-defect certification (it grades an
  *agent* against tasks; skillsaw grades an artifact, and the two need different evidence);
  `vac-protocol`'s bundle format (its `results`-recomputed-from-artifacts rule is a good
  discipline but the closest thing skillsaw emits is a score, which this repo deliberately
  does not turn into a shippable claim).

## Deep Reads — `oh-my-agent`, `ruflo`, `superpowers` (2026-08-22)

Three repositories from `~/Documents/agent-green` that the first survey pass had filed as
read-shallowly, opened and read. Written up in gnosis's `manifesto.md`; these are the
items that are skillsaw's. They were recorded in gnosis's `TODO.md` first, which was the
wrong home — a backlog for this tool belongs here.

They divide cleanly. `oh-my-agent` and `ruflo` are about **the ratchet**: what a
hill-climbing loop does when its accept rule meets a case it was not written for.
`superpowers` is about **what the rubric measures**, and it is the harder one, because
it says the score and the thing the score is for are different claims.

### The Ratchet

- [x] **A `PASS → FAIL` transition is its own status, not another failure.** Done
  2026-08-23 as `rubric.TransitionOf` and `DiagnoseAgainst`, reached via
  `diagnose --against PREV.json`.
  **The input had to be designed before the field could exist.** `Diagnose` is pure over one
  evaluation and nothing in the process holds a previous one. Two candidate sources existed
  and only one works: `results.tsv` records which dimension a row *targeted* and the run's
  total, never per-dimension scores, so the audit log cannot answer this. An earlier
  `eval --json` can, since `Evaluation` already serialises every `DimScore`. Hence
  `--against`, reusing `preflight`'s idiom rather than inventing a second spelling of
  "the state before".
  **Two guards this needed and did not have.** `Evaluation` recorded no rubric edition, so
  the comparison would have shipped the exact defect the previous two sessions closed one
  layer up — dim 4 scoring 9 then 7 means nothing if the checkpoint band moved between them.
  `Evaluation.Rubric` was added first and a mismatch routes to `TransitionNotCompared`,
  the same third state `regression` uses rather than a fourth spelling of it. And
  **"clean" is not `Final == 10`**: dims 4, 6 and 9 derive bases below 10 by design, so a
  transition defined as "left 10" would fire on nothing for three dimensions. The comparison
  is against the dimension's own prior score, on `Final` rather than `Penalty` — a derived
  base falling 9 to 2 carries no penalty either side and a check on `Penalty` looks straight
  past it. Verified with a plant that does exactly that.
  Noticing the transition changes the recommendation, which is the point: a regressed target
  is told to read the diff since the earlier evaluation rather than rework the dimension.
  **No failure counter was added.** The entry's argument is that a transition must not
  increment one, and skillsaw has none — `auditlog.CurrentStreak` counts experiments, not
  dimensions. Building one in order to not increment it would be building the thing the
  entry warns about. Original entry:
  `oh-my-agent`'s judge protocol emits `REGRESSED` exactly once on the transition and
  explicitly does **not** increment the failure counter for it, because a regression
  routes differently: it ships the diff between the last passing iteration and now, so
  the next attempt is a targeted diagnosis rather than a reimplementation. `gate`
  already separates the measured axis from the disposition axis (Adopt-5 above) and has
  `improved`/`tie`/`regressed` on `Status` — what is missing is that a *dimension* that
  previously scored clean and now does not is a different event from one that has never
  been clean, and `diagnose` cannot currently say which it is looking at.
- [x] **Re-score every dimension each iteration, including the ones that passed.**
  Checked, and it was a no-op: `Evaluate` → `EvaluateWithBases` iterates the whole of
  `Dimensions()` with no skip path and no cache, and both callers (`cmd/eval`,
  `cmd/diagnose`) score freshly loaded skills. The one thing carried between iterations is
  the set of judge-supplied bases, and `cmd/eval` hashes the skill, refuses to apply stale
  bases, and names both versions on stderr. What was missing was not the behaviour but a
  guard on it, since closing an item on an unpinned property is how it reopens:
  `TestEveryDimensionIsScoredEveryTime` asserts every dimension is scored exactly once and
  that an edit moves the dimension it touched. Verified against a planted skip of dim 9,
  which fails it with `scored 8 dimensions, the table has 9`.
- [x] **A failure counter must count *consecutive* failures and reset on success.**
  Otherwise a dimension that is merely noisy accumulates to a terminal verdict on
  elapsed time. `oh-my-agent` states the reasoning worth keeping next to the code:
  recurring flakiness should surface as repeated regressions — a signal about the
  *check* — rather than as a permanent verdict about the thing checked. Three surveyed
  projects converge on the same shape (consecutive, resets on progress, small *N*).
  There was no counter to fix — `ratchet.Evaluate` is stateless per call and the loop lives
  in `skillsaw-skill` — so the deliverable is making consecutiveness *computable and
  reported*, which is what stops a consumer accumulating on elapsed time.
  `auditlog.CurrentStreak` folds the log into its tail run, counting back from the last row
  and stopping at the first `keep` or `baseline`.
  It returns a `Streak{Length, Errors}` rather than an int, because the entry's own
  reasoning forces the split: a `revert` is a measured regression (evidence about the skill)
  and an `error` is an experiment that failed to run (evidence about the harness). Fusing
  them lets a broken check masquerade as a skill that keeps getting worse, and the two want
  opposite responses. `history` reports both and says so outright when every failure in the
  streak was a harness error. An unrecognised status counts toward the streak rather than
  ending it — fail-closed, since an unknown status is not evidence the run went well.
  Verified against two plants: counting from the front (the recovery case then reports a
  streak of 2 instead of 0) and fusing errors back into the total.
- [x] **Estimate the evaluator's noise floor before calling any delta an improvement.**
  Done for `activation`; see the follow-up below for `judge`.
  `ruflo`'s optimisation log accepts `+0.0028` and `+0.0046` on single runs and rejects
  `−0.0002` and `−0.0004` as *"essentially flat"* — accept and reject thresholds an
  order of magnitude apart, with no variance estimate and no re-measurement of the
  champion. skillsaw's score is deterministic, so this does **not** apply to the rubric
  itself, and it applies squarely to anything sampled: `activation`, `judge`, and any
  future measured-lift number. `internal/stats` already has Wilson; the missing piece
  is refusing to accept a delta that does not clear the interval.

  `internal/noise` now does the refusing for `activation`. `UtilityBound` derives a
  conservative interval on net utility from the Wilson intervals the report already
  carries — net utility rises with TPR and falls with FPR, so its extremes over the
  rectangle of the two intervals are that rectangle's corners, and because targets and
  distractors are disjoint samples the rectangle covers at least 90%. It is deliberately
  wider than a true 95% interval would be; calling it 95% would be the borrowed precision
  the entry objects to. `Gate` returns `clears` / `below` / `unresolved`, and `--min` now
  gates on the bound rather than the point estimate, so a skill whose interval spans the
  floor exits non-zero as UNRESOLVED with the fix named (write more test-prompts, do not
  edit the skill). `VerdictUnknown` is the zero value and the shell treats anything that is
  not `clears` as a failure, so an unevaluated report cannot read as a pass.

  **This changed default behaviour**: `--min` defaults to 0, so a skill with a handful of
  test-prompts that used to pass on a positive point estimate now exits non-zero. That is
  the point of the entry — a two-prompt sample cannot tell a working trigger from a lucky
  one — but it will surface as new CI failures on small prompt sets.
- [x] **The same refusal for `judge`.** Done, in a narrower form than `activation`'s, because
  `judge` has no gate: it emits `mean_soft` and `base` for a reader and for `eval`, with no
  threshold and no delta. Applying the `activation` treatment literally would have invented
  a gate. The refusal that belongs here is that **the base must not read as more resolved
  than the sample supports**.
  `noise.MeanInterval` is the t interval with the sample standard deviation floored at
  `1/(n+1)`. The floor is the point: judge cases very often score identically, and an
  unfloored t interval over identical observations has zero width and reports an exactly
  pinned mean from three samples. The floor is the rule of succession read as a dispersion —
  a deliberate assumption, documented as one rather than dressed as a derived bound.
  Measured over identical observations it leaves 3 cases spanning bases 2–10, 10 cases
  spanning 8±1, and 15 cases resolving to a single base.
  Flooring the *half-width* instead was tried first and rejected on measurement: both
  Hoeffding and the rule of three leave forty identical cases spanning three bases, so
  `Resolved()` would never fire, and a caveat that never clears is not a signal.
  `scores.Aggregate` now carries `Interval`, `BaseLow`, `BaseHigh` and a `Resolved()`
  predicate; `judge --all` prints the supported range and says to score more cases before
  treating an unresolved base as measured. **No exit-code change** — see above.
  The generalisation this entry anticipated did *not* happen: `UtilityBound` is specific to
  a confusion matrix and `MeanInterval` to a bounded mean. They share a package, not a
  signature, and an interface over two functions with different inputs would be built for
  symmetry rather than for a caller.
- [x] **A change that loosens a gate may not score as improving it.** Closed today by the
  rubric being a Go table: `Config` is only ever built by `rubric.DefaultConfig()` in
  `cmd/diagnose` and `cmd/eval`, populated from flags, and nothing loads it from disk. A
  candidate edit changes the skill and never the rubric, so it cannot relax its own gate.
  **This is a constraint on the rubric-as-data entry** — making the rubric loadable from
  the tree being scored is exactly what would open this vector, and that entry has to carry
  the answer with it.
  The original observation, kept because it is the concrete instance: `ruflo`'s loop raised
  a promotion rate by relaxing its promotion predicate from `AND` to `OR`, logged *"BIG
  WIN"*, and committed. Recording the loosening is not enough — it *was* recorded, in prose,
  as a win. Anything in the rubric or in `gate` that a candidate edit can itself relax has
  to be excluded from the improvement it produces.
- [x] **Audit `rubric.Dimensions()` for count-shaped dimensions.** Done, and the answer is
  in the type rather than in a comment, because a comment about which dimensions an
  optimiser can feed is exactly what drifts when a threshold is next tuned. `Dimension` now
  carries a three-valued `Response` beside `NeedsJudge`:

  - **additive** — dim 3 (any branch removes the 3-point penalty) and dim 9 (`units >= 3`
    reaches base 9). More of what the dimension counts raises the score.
  - **subtractive** — dim 4. Reasoning said additive; the test refuted it, scoring 10 then
    9. `finalize` assumes base 10 for a needs-judge dim with no derived base, so *deriving*
    a base of 9 lowers the score. An `Additive bool` would have recorded that backwards.
  - **neutral** — the remaining six.

  The audit's finding is worse than "three dimensions are count-shaped": `strategyFor`
  already tells the optimiser to perform exactly those edits — "insert explicit
  checkpoints" (dims 1, 2, **4**), "add 'if X fails then Y' fallbacks" (dims **3**, 5),
  "add counter-examples" (dims 6, 7, **9**) — and `skillsaw-skill`'s hill-climbing loop
  reads that guidance. So `Diagnose` now appends a caveat naming what does *not* count for
  a non-neutral dimension, and `Diagnosis.Response` exposes the property so a loop can
  weigh it without parsing prose. `TestFeedableDiagnosesSayWhatDoesNotCount` requires the
  caveat on non-neutral dimensions and forbids it on neutral ones, so it cannot spread
  until it means nothing; verified against a planted removal.

  Deliberately **not** done: no re-tuning of dims 4 or 9 — their thresholds were set
  against a 233-skill corpus and re-tuning without eval evidence is the change this family
  refuses on sight — and no deterministic "filler" penalty, since detecting filler needs
  judgement, which is dim 8's job, and a proxy for it would just be a new feedable
  dimension. Original entry: `ruflo`'s loop moved a
  harness score 40 → 55 with no capability change, by adding files a presence-counting
  dimension rewarded — and one of those artefacts, a bare symlink created for the
  purpose, is still in its repository root. **Any dimension scored by counting
  artefacts is gameable by creating artefacts.** Check every dimension against exactly
  that predicate; this repo's charter (never a shippable claim) limits the damage but
  does not prevent it, because `skillsaw-skill`'s hill-climbing loop is an optimiser
  pointed at these numbers.

### What the Rubric Measures, and What It Does Not

- [ ] **The rubric scores form; nothing here measures lift, and the two are different
  claims.** `superpowers` builds skill authoring as TDD over process documentation —
  run a pressure scenario **without** the skill, capture the rationalizations verbatim,
  write the minimal skill addressing those specific failures, re-run, close loopholes —
  under an Iron Law that applies to edits as well as new skills: *"If you didn't watch
  an agent fail without the skill, you don't know if the skill teaches the right
  thing."*

  This is the axis this repo's own notes keep naming and not filling: SkillLens's
  *Extraction Efficacy* and *Target Evolvability*, which the manifesto records as
  unharvested, and `oh-my-agent`'s `oma skills eval` measuring treatment-versus-baseline
  utility. A rubric score says a skill is well-formed. A measured lift says it works.
  **Not a proposal to make skillsaw call a model** — it will not — but the artifact it
  would consume is a *baseline observation*, and the observation can be produced
  outside and read in, exactly as `test-prompts.json` already is.

- [x] **The cheapest half of that is a gate that fires before authoring: the no-guidance
  control.** Done 2026-08-23. The entry said *"a field in the evidence, not a check in the
  scorer"*, and `scores` is the evidence — it already hash-binds what a reader asserted to
  the text they asserted it about, and a control observation is the same shape: what
  happened with the skill *absent*, for that version. `scores.Entry.Baseline` records it and
  `File.Measured(hash)` asks. Empty means **unmeasured**, a third state beside pass and
  fail, reported by `eval` and blocking nothing — consistent with `finding.Unexamined`,
  `noise.VerdictUnknown`, and an unresolved base.
  **A reader/writer asymmetry nearly made the field write-only.** `Marshal` reflects over
  `Entry`'s tags while `parseEntries` names its fields one by one, so the new field was
  written and silently dropped on the way back in. Caught by the test, and now pinned by
  `TestBaselineSurvivesARoundTrip` — Marshal's contract says `Parse(Marshal(e))` yields `e`,
  and only a test makes that true of a field added later.
  Not put in `testprompts`: a control observation is evidence about a version, not part of
  the prompt set, and it would have been a skillet change for something skillsaw owns.
  Original entry: *"Always include a no-guidance control. If the control doesn't exhibit the
  failure, there is nothing to fix — stop, don't author the guidance."* A skill written
  against a failure the model does not exhibit is pure context cost, and no rubric
  dimension can detect one, because the document is about a real-sounding problem and
  is well-formed. This is a field in the evidence, not a check in the scorer: a skill
  that carries no baseline observation is *unmeasured*, which is a third state beside
  pass and fail and is the same discipline as `unchecked` everywhere else in the family.

- [ ] **`diagnose` cannot separate three repair classes that want opposite edits.**
  *Half-built 2026-08-24: `harness/meta-why.txt` asks the question, deliberately outside the
  nine so the runner does not treat it as a phrasing — it is asked after a failure, against
  the same skill, in a different situation. The README carries the three shapes and their
  opposite repairs. **No classifier**, and that is the remaining half: assigning a class
  from prose is the judgement this repo refuses for dim 3, dim 8 and the ruleset subject
  slot. What is missing is answers to classify, which needs runs against the two real
  failures already captured (`03-imperative`, `08-buried-in-a-list` on
  `matryer-decode-valid`).* Original entry:
  `superpowers`' meta-test asks the agent that failed *with* the skill loaded how the
  skill should have been written, and classifies the answer: *"the skill WAS clear, I
  chose to ignore it"* is not a documentation problem and needs a stronger foundational
  principle; *"the skill should have said X"* is a content problem and X goes in
  verbatim; *"I didn't see section Y"* is an **organisation** problem — the content was
  right and unreachable. `diagnose` answers which dimension is weakest and is silent on
  whether the content is wrong or merely misplaced, and those are different edits. The
  classification needs an observation skillsaw does not have; recorded so the shape is
  known when it does.

- [x] **Brevity can trade against efficacy, and a dimension that rewards it should say
  so.** Done 2026-08-23, **and the entry was aimed at the wrong place** — worth recording,
  since the misfiling is why it sat here for a week looking like rubric work.
  Verified twice: **no rubric dimension rewards concision.** `Bytes` is recorded and never
  scored, and `DescriptionMaxRunes` is a cap rather than a reward. The only thing in
  skillsaw that rewards brevity is `edit.DefaultMaxGrowth`, and it *is* in an optimize
  loop's path, so the caution belongs on that constant: a ceiling is defensible because
  unbounded growth is its own defect, but it is a budget rather than a goal, and an edit
  that comes in under it has not thereby been shown to be better. Original entry: `superpowers` ran a branch-wide compression campaign where *"each cut was
  micro-tested with subagent probes, and the one cut that measurably degraded behavior
  was reworked rather than shipped."* One deletion of apparently-redundant prose
  changed behaviour. Any dimension here that rewards concision is optimising something
  that can trade against the thing the skill is for — the same class of defect as a
  count-shaped dimension, and subtler because the direction feels virtuous.

- [x] **Validity scope on the SkillLens dimensions.** `superpowers`' *Match the Form to the
  Failure* classifies four baseline failure types and reports that the guidance form
  fixing one **measurably backfires** on another — *"the prohibition arm produced
  clearly more of the unwanted content than the recipe arm (fully separated
  distributions), and trended worse than even the no-guidance control."* Dims 3/5/9
  reward a boundary section and penalise hedging, which are the right signals for a
  discipline skill under pressure and the wrong ones for a skill whose baseline failure
  is wrong-shaped output or an omitted field. The dimensions do not need changing; what
  is missing is a statement of **which class of skill they are valid for**, so an empty
  result on a reference skill is not read as three passes. Matching entry in
  `skillet/TODO.md`, since the detectors live there.

  Stated in `rubric`'s package doc under *What dims 3, 5 and 9 are valid for*: they suit a
  skill whose baseline failure is a model doing the wrong thing under pressure, a low score
  on a reference skill is a statement about form rather than quality, and three empty
  detector results are not three passes. It also records why the dimensions are **not**
  conditioned on skill class — skillsaw cannot infer the class, and guessing it would
  silently change what a score means.

### `activation` Has a Harness Shape Available

- [x] **Adversarial phrasing corpus + an ordering assertion.** The ordering half is done
  2026-08-23 as `internal/transcript` + `skillsaw ordering --skill NAME TRANSCRIPT...`.
  **This item is much smaller than I had been calling it.** For several turns I ranked it as
  a real-agent harness — slow, nondeterministic, needing credentials — and gated four other
  items behind that. The entry's last paragraph says otherwise: *"This does not make
  skillsaw call a model."* The runner already exists in
  `superpowers/tests/explicit-skill-requests/run-test.sh`; skillsaw owed the contract and
  the scoring, which is ordinary pure Go. The ranking was right and the sizing was wrong.
  **The entry's own fallback had to be refused.** It offers "a new operator in `judge`'s
  closed set" as an alternative to a `preceded_by` notion. `judge.Score` takes a **flat
  string** and every operator is a substring or regex test over prose — `OpToolCalled` is
  already documented as `"heuristic: contains arg"`. An ordering operator there would search
  a prose reply for a fact about event sequence, which contradicts the sentence that
  motivates the item: *"Ordering is not something a prose reply can be trusted to report
  about itself."* Ordering is a property of a transcript, a different input type, so it got
  its own reader and verdict.
  `Order` is four-valued with a fail-closed zero: `not-triggered`, `after-action`, `first`.
  `after-action` is the state the item exists to name and is **worse** than `not-triggered`,
  not milder — the agent had the skill, began work without it, then loaded it, so part of
  the artifact was produced outside guidance it now appears to have followed. Loading a
  *different* skill first counts as action, since that is work under the wrong guidance
  rather than preparation for the right one.
  **The confusion matrix was deliberately left alone.** The entry notes a premature trigger
  is recorded today as a true positive. It is — but `ratchet.Score` is skillet's, computes
  from vocabulary overlap, and never sees a transcript; teaching a text proxy about event
  ordering is not possible, and silently downgrading a TP would move every historical
  comparison. The verdict is reported alongside instead.
  **The event shape is inferred, not captured.** No transcript exists on this machine; the
  format comes from the reference harness's flat greps, which say nothing about nesting. The
  reader therefore walks the decoded JSON for `type: "tool_use"` rather than indexing a
  path, and accepts the skill name at the top level or under `input`. Fixtures cover both
  shapes and **want checking against a real capture.** Verified against binding to one path,
  which makes the other shape stop parsing.
  Not done, and left open below: the **adversarial phrasing corpus** itself — nine variants
  including a pressure phrasing and a pre-summarised workflow. That is prompt authoring plus
  the existing shell runner, not skillsaw code.

- [x] **The adversarial phrasing corpus.** Done 2026-08-23 as `harness/` — nine prompt
  templates, `run.sh`, and a README. Scored by `skillsaw ordering`; no Go code was needed.
  **The reference corpus could not be reused, and copying its shape would have inherited
  that.** Every prompt in `superpowers/tests/explicit-skill-requests/prompts/` hardcodes
  `subagent-driven-development` *and* `docs/superpowers/plans/auth-system.md`. The pressure
  shapes are general; the subject is not. These are templates with `{{SKILL}}` and
  `{{TASK}}`, so the same nine phrasings point at any skill.
  **Two of the reference's nine are not phrasings of one request** — they are a second and
  third skill, checking that explicit request works generally. The entry specifies nine
  phrasings of *one* request, so those two slots hold two further pressure shapes instead:
  the request buried in a list of unrelated asks, and a negation sitting next to it.
  **The control gates the rest, which the reference does not enforce.** `01-bare` is the
  skill's name and nothing else, it runs first, and if it does not load the skill the run
  stops. Eight phrasings failing under pressure cannot be told apart from a skill that does
  not answer to its own name, and only the first is what this corpus measures. Same
  discipline as the no-guidance control in `EVIDENCE.md`. Verified: a failing control
  refuses to print the other eight even when all eight would pass.
  **Scoring is delegated, not re-grepped.** The reference approximates "before" with
  `head -n LINE | grep -v …`. `skillsaw ordering` parses the events; `run.sh` shells out to
  it rather than carrying a second, weaker copy of the rule — the drift this family already
  refuses between a detector and its consumers.
  **`--transcripts DIR` scores a captured run without an agent.** A harness that needs
  credentials to exercise cannot be tested, so the plumbing — substitution, layout, control
  gate, table — is exercised offline against fabricated transcripts, and that same path
  re-scores a real capture after a scorer change. Verified end to end: all three verdicts
  render, the control gate fires, the all-clean path exits 0, and `shellcheck` is clean.
  **Not verified: agent behaviour.** Nothing here has been run against a model. The prompts
  are written and the pipeline works; what a real agent does with them is unmeasured, and
  the README says a single run is an observation rather than a property of the skill.
  **Retargeted to Gemini CLI 2026-08-23**, and it was not a flag swap. Three real captures
  showed the event vocabulary differs from Claude Code's in three ways at once — the tool is
  named in `tool_name` not `name`, arguments sit under `parameters` not `input`, and events
  are flat rather than nested in an assistant message. Worse, **Gemini has no skill tool at
  all**: it loads a skill by reading the `SKILL.md`, and when that read was refused for being
  outside the workspace it fell back to a shell `cat` of the same path. A reader that only
  knew Claude Code's shape would have reported every Gemini run as `not-triggered` — a parse
  failure wearing a verdict's clothes. `internal/transcript` now recognises both dialects,
  detecting a skill load by name *or* by a `<skill>/SKILL.md` path among the arguments, and
  carries a verbatim capture as a fixture.
  `update_topic` joins the planning allowlist: captured firing immediately before a skill
  read, carrying only a title and a summary of what the agent was about to do.
  **`HOME` is no longer isolated, which departs from the reference and costs something.**
  Gemini discovers skills under `$HOME/.gemini/skills`, so a fresh `HOME` hides the skill
  being requested and every run reports not-triggered. The reference isolates `HOME` to keep
  the operator's context out; here the operator's context is where the skill lives. Each
  phrasing still gets its own empty working directory, and runs read-only in plan mode. The
  README says plainly that two machines can therefore disagree.
  Original entry: The other half of the entry above. Nine phrasings
  of one request — including *"Don't waste time — just read the plan and start dispatching
  subagents immediately"* and one where the user pre-summarises the workflow, tempting the
  agent to follow the summary instead of the skill. Authoring plus a runner invocation;
  `skillsaw ordering` now scores what it produces.
  `superpowers/tests/explicit-skill-requests/` is a working generator of exactly the
  observations `activation` consumes. Its shape: run the real agent under an **isolated
  `HOME`** (so the operator's own context cannot leak into the result), capture
  machine-readable transcript events, and assert with a pure predicate over them. Nine
  adversarial phrasings of one request, including a pressure variant (*"Don't waste
  time — just read the plan and start dispatching subagents immediately"*) and one
  where the user pre-summarises the workflow, tempting the agent to follow the summary
  instead of the skill.

  Its second assertion is the one worth copying: **were any other tools invoked before
  the skill was loaded**, against an explicit allowlist of actions that do not count as
  action. `activation` today asks whether a skill triggered; it does not ask whether
  anything happened first, and "triggered, but after the agent had already started
  working" is a distinct and worse outcome that the confusion matrix currently records
  as a true positive. Ordering is not something a prose reply can be trusted to report
  about itself.

  This does not make skillsaw call a model. It defines the file the harness writes and
  `activation` reads — the `test-prompts.json` contract already has `type` and `checks`
  and would need a `preceded_by` notion, or a new operator in `judge`'s closed set.

- [x] **Variance across repetitions as a bindingness metric.** Done 2026-08-24 as
  `skillsaw ordering --agreement` and `harness/run.sh --repeat N`.
  **The unit is agreement, not the verdict**, which the entry states and a first reading
  misses: *"five different interpretations across five reps means the wording isn't
  binding"*. So three firsts and two after-actions reports as `SPLIT over 5 runs: 3 first,
  2 after-action` — not as a pass with noise. A majority verdict would hide precisely the
  case the metric exists to find; verified against a plant that treats a majority as
  agreement.
  A single run is reported and **qualified** rather than counted as convergence: one
  observation is not a property, and `in all 1 runs` would invite reading it as one.
  Repeated runs group by the filename stem before the first dot, so the runner names them
  and the scorer needs no flag telling it how its input is laid out.
  `--agreement` changes what is reported and never the exit rule. A split is a finding; a
  new gate that failed on disagreement is not what a caller measuring bindingness asked for.
  Original entry: *"When guidance lands, reps
  converge on the same shape. Five different interpretations across five reps means the
  wording isn't binding."* Dispersion, not mean — a signal about whether the wording
  constrains at all, available from repeated activation runs at no extra cost beyond
  the reps. `skillet/stats` holds the machinery and this is its prospective second
  consumer; recorded in that repo as held for the ask.

### One Line to Draw, and One Thing Not to Take

- [x] **A vendor's prose advice is not the machine-readable contract, and the rubric
  should not treat them alike.** Done 2026-08-23, in `rubric.toml` — which is where it can
  now live, since making the rubric a document gave every dimension a `note`.
  **Verified that dim 1 mixes the two today.** `speclint` owns the contract (frontmatter
  keys, `DescriptionMaxRunes`), and dim 1 penalises the description cap *and* filler tails
  *and* kebab-case naming, the last two being this project's house style. Its note now says
  which of its checks is which, and records that the house-style half is why the dimension
  stays `needs_judge` rather than fully derived: a style preference must not be the last
  word on a score. The document header states the rule, so a dimension added later has to
  answer it. Original entry: `superpowers` vendors Anthropic's skill-authoring
  guidance verbatim as a reference, accepts the contract from it (`name`,
  `description`, the 1024-character cap), and refuses its prose: *"PRs that
  restructure, reword, or reformat skills to 'comply' with Anthropic's skills
  documentation will not be accepted without extensive eval evidence showing the change
  improves outcomes."* That is the correct line and this family has not drawn it —
  `speclint` gates the contract, and any rubric dimension that encodes a vendor's style
  preference is importing prose advice with the authority of a specification. Worth one
  pass over the dimensions asking, of each: is this the spec, measured evidence, or
  taste?

- Deliberately NOT adopted: `superpowers`'s `persuasion-principles.md`. It grounds an
  Authority-first recommendation ("YOU MUST", "No exceptions") in Meincke et al. (2025),
  *"Call Me A Jerk: Persuading AI to Comply with Objectionable Requests"*, citing 33% → 72%
  compliance — a study that by its own title measures defeating refusal training, not
  improving process adherence. That is the uncalibrated-heuristic class the unified-thinking
  survey above already rejected. Their own head-to-head wording tests are better evidence
  and one level closer to the task, and those are what the entries above cite.

## Applicability Is a Rule, Not a Type — and Dim 3 States It Best (2026-08-22)

`skillet` closed its long-open note about naming a general `Applicability` mechanism. The
answer is **no type**, and skillsaw is the reason: counting the real sites showed the same
predicate consumed four ways *in this repo and adh*, each deliberately different, each
already carrying a reason. A shared type would have to be generic over score, factor,
diagnostic, and nothing-at-all, and the hard question — what to do when a check does not
apply — has a different right answer at each site. Full reasoning in `skillet/TODO.md`.

- Nothing to build here. Recorded because two things in this repo are now cited from a
  family-level rule and should not be "simplified" by someone who has not read it.
  **`internal/rubric/rubric.go`'s dim 3 comment is the canonical statement of the
  distinction** the rule turns on, and it is better than anything in `skillet`:
  *a skill that runs commands and never says what to do when they fail is the defect this
  dimension exists to catch, while one that executes nothing has no runtime failure to
  encode and docking it would be a category error.* Paired with the second branch's
  *"reported, never docked: a mis-derived category must not hide a real gap"*, that is the
  whole argument for why derived applicability must not silently swallow a finding.
  **And dim 4/8 is the case that kills the type.** It uses `HasCodeBlock` only to *reword*
  the judge prompt — *"executes nothing to checkpoint; likely not applicable"* — and
  suppresses **nothing**, deferring to a judge either way. That is the right handling for a
  *missing input* as opposed to a *wrong category*, and an `Applicability` type expressive
  enough to cover it would be one that can launder unfinished work as inapplicable. The
  rule states the distinction; a type would blur it.
  The rule, for reference when either comment is next edited: *applicability is derived, not
  declared, and a run states what it skipped. What gets suppressed is the consumer's choice
  — a deduction, a whole check, or nothing — but the reason is never optional.*

## `auditlog` Came Home (2026-08-22)

- [x] **`skillet/auditlog` is now `skillsaw/internal/auditlog`.** It was extracted to the
      kernel speculatively and skillsaw was its only importer for the whole five months it
      lived there. The second consumer never arrived, and the one plausible candidate ruled
      itself out in writing: gnosis considered it for the mutation-row job and recorded in
      SPEC §15 that it is *"the wrong shape — it reads `results.tsv`, nine columns describing
      an optimization experiment."*
      The move was cheap because the package had **zero skillet dependencies** — stdlib only
      — so it was a copy, three import repoints (`cmd/log`, `cmd/history`, `cmd/log_test.go`),
      and a `gofmt` for the import ordering the new path changed. Build, tests and
      `golangci-lint run ./...` clean on both sides; skillet deleted it in the same pass.
      **Two things follow for this repo.** The package doc now says why it left and that
      promoting it back is the right move if a second tool ever wants an experiment log —
      not copying it. And the two references above that pointed at `skillet/auditlog` now
      point at `internal/auditlog`; that is the whole cost of ownership, and it is a good
      trade for not making four other consumers carry the surface.
      Left verbatim on purpose, including its `t.Parallel()` calls, which match this repo's
      convention (15 of its test files use them) though not the global guidance. Changing
      behaviour during a move would have conflated two changes; if that is worth revisiting
      it is its own edit.

## Commissioned Gap Report, Round Two — Nothing Lands (2026-08-22)

Source: `~/Documents/agent-green/FPF/skillsaw_todo.md`, the successor to the
`skillsaw_topten.md` already assessed in the rubric entry above. Checked; nothing lands.
**Full reasoning is in `skillet/TODO.md` under "Round Two, and What Asking for Code-Reality
Verification Actually Bought"** — recorded once for the family rather than seven times, the
same convention round one used.

Its single finding is *"externalize the rubric data with strict fail-closed loading… using
TOML instead of JSON… strictly failing closed on unmarshal errors rather than falling back
to compiled defaults."* That is this file's own entry, returned. The rubric item above
already says all of it, and the two constraints the report presents as its contribution —
TOML over JSON, refuse-to-score over score-with-defaults — **were derived here**, as the two
traps in round one's plan. A report cannot independently arrive at a correction whose only
written statement is the backlog it read; its verification step says it read `TODO.md`.

Nothing to do. The rubric entry stands unchanged, and it did not need a second witness.

- Worth noting for the entry's own sake, since this is now the second report to reach it:
  **the item's value was never the observation that `rubric.go:111` is a Go literal.** That
  is visible in one grep and both rounds found it. The value is the load-path strictness
  the entry works out at length, which neither round proposed until it was written here.

## `preflight` Owns the Prose↔Test-Prompts Coupling Gate (2026-08-22)

Decided after a false start. The question was posed as *exegesis (structural) versus
skillsaw preflight (quality)*, and both halves of that framing were wrong: `preflight`'s own
help says **"structural gate: reject an edit that breaks structure, whatever it scored"**,
so both tools do structure. What separates them is **scope** — exegesis grades an artifact
as it stands, `preflight` grades *an edit*, "between writing an edit and deciding whether to
keep it".

The coupling defect is inherently about a change: a `SKILL.md` rewritten while its
`test-prompts.json` still describes the previous version cannot be seen in one snapshot.
So it is not a structural-versus-quality question at all, it is snapshot-versus-delta, and
only one tool here is delta-shaped.

- [x] **Gate it in `preflight`, once `skillet/manifest` carries the missing hash.** Done
      2026-08-23. `manifest.Skill.TestPromptsHash` landed in skillet v0.19.0 and went
      further than this entry expected: `Diff` now returns `Axes{Skill, TestPrompts}` per
      changed location, so the comparison was already upstream.
      Three things were needed here. **`preflight` had to start honouring severity** —
      `finding` documents that only `SeverityError` blocks, and preflight counted every
      diagnostic, so no advisory finding was expressible at all. Behaviour-preserving
      today, since nothing reachable emits a warning. **`scan` never populated the hash** —
      `cmd/changed` built `manifest.Skill{Slug, Dir, Hash}` and set neither prompts field,
      so with the new `Axes` a base manifest recording prompts against a current side that
      does not would have reported "prompts changed" for every skill in the corpus. That
      reading moved to `internal/inventory`, which `changed` and `preflight` now share, and
      which also owns the one definition of the location-keying rule that must agree with
      `Diff`. **`Axes` alone is not enough**: it cannot separate "has prompts, unchanged"
      from "has none on either side" — both are `TestPrompts: false` — and only the first is
      a stale pair. The test caught that before it shipped; `edit.Uncoupled` now consults
      the current entries. An untested skill is a real defect and a different one.
      `SeverityWarning` + `ActionHuman`, promoted by `--require-coupling`. Verified against
      reporting on `Axes.Skill` alone, which starts failing the coupled-edit case.
      Original entry:
      `changed` already computes `manifest.Delta` against a baseline manifest, and
      `manifest.Skill` records `Hash` (of `SKILL.md`) plus `TestPrompts` as a **path, not a
      hash** — so the delta can see prose change and is structurally blind to whether the
      prompts changed with it. One field in skillet closes that; filed there.
      Then: a skill in `Delta.Changed` whose test-prompts hash did *not* move is the
      finding. **Advisory by default** — an editorial fix to one sentence legitimately needs
      no test change, and a gate that fires on those teaches people to pass `--no-verify`.
      `preflight` already exits 1 on defects, so the flag that promotes this from report to
      block should be explicit and off.
      **Why not `changed`:** it reports and does not gate, and the value is in stopping an
      optimise loop from keeping an edit whose assertions no longer match. Why not exegesis:
      it has no baseline — one `manifest.Build` call and never `Diff` — and a snapshot cannot
      see a drift by construction.
      **Rejected implementations, recorded so they are not re-proposed:** filesystem mtimes
      (git does not preserve them, so the check reports nothing on CI and everything after a
      rebase) and `git diff --name-only` (an environment dependency inside what should be a
      pure comparison). A content hash in a manifest is what this family already uses.

## Three Items Transferred From gnosis's Backlog (2026-08-23)

The `Deep Reads` section above records that gnosis's `TODO.md` was the wrong home for
skillsaw's work and moved it here. Three more of gnosis's entries turned out never to
have been transferred — a re-read of its backlog on 2026-08-23 found fourteen items
filed against sibling repositories, of which nine were already here or in `canonizer`
and `adh` (five of them already **done**) and five were not anywhere.

The accounting matters more than the items. **A backlog that mirrors another
repository's work goes stale in the direction that flatters**: gnosis was still
carrying `count-shaped dimensions`, `loosens a gate`, the consecutive counter, the
re-verification item and the noise floor as open, and all five had been closed here.
The rule the family already applies to knowledge applies to backlogs: one home, and a
pointer from everywhere else.

- [x] **No cross-repository skill-identity check, and there should be one.** Done
  2026-08-23 as `internal/portability` + `skillsaw portable REPO [REPO ...]`. The item was
  right; **its evidence was wrong on every specific**, which is worth recording since the
  entry reads as though the convention had been checked.

  | The entry says                                                | Measured over both trees                                                                |
  | ------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
  | `cognitive-doc-design` is byte-identical across the two repos | It is in **gentle-ai only** — not shared at all                                         |
  | the convention "verifiably holds"                             | Of 54 distinct names, **one** is shared, and it has **diverged**                        |
  | `gentle-ai-branch-pr` diverged from a portable twin           | `branch-pr` and `gentle-ai-branch-pr` are **both in gentle-ai**; the pair is intra-repo |

  So the check finds a real violation on its first run — `skill-creator`, different content
  in both repositories — and that is the argument for it.

  **Two corrections the corpus forced.** Identity is the **declared frontmatter name**, not
  the directory: a runtime routes on what a skill calls itself, and `skill-creator` sits at
  `curated/` in one tree and `internal/assets/skills/` in the other. And **a duplicate with
  identical content is a copy, not a collision** — my first definition flagged any name
  declared twice in one repository, which produced *five* findings on `gentle-ai`, all of
  them its own skills embedded twice byte-for-byte under `internal/assets/`. A check that
  fires on ordinary work is one somebody turns off. A collision is now a duplicate whose
  copies **differ**; after the fix the pair reports exactly one finding.

  **`skill.Discover` is one level deep**, which is right for a skills tree and wrong for a
  repository — these keep skills under `community/`, `curated/`, and
  `internal/assets/skills/`. Passing those subdirectories separately would make one
  repository look like several and defeat both the collision check and the prefix
  exemption, so `portable` walks recursively.

  The prefix exemption is derived from the repository's own directory name, and the entry's
  caveat is real and is in the doc comment: **the check is exactly as good as the naming**.
  A skill that ought to have been prefixed and was not reports as a portability violation,
  and nothing mechanical separates those, so the message offers the rename alongside the
  sync. No default repositories — which checkouts constitute "everywhere" is a fact about
  one machine. Original entry:
  `Gentleman-Skills` and `gentle-ai` declare a portability convention in prose —
  unprefixed names are portable and keep their canonical names, `<tool>-*` names are
  repo-specific — and it verifiably holds: `cognitive-doc-design` is byte-identical
  across the two repositories, and `gentle-ai-branch-pr` has legitimately diverged.
  It holds because one person maintains both, which is exactly the condition that
  stops holding.
  A check that same-named unprefixed skills hash alike across a set of repositories is
  the mechanism this family could supply back, and `skillet/identity.Hash` already
  does the hard part. The design question is what a legitimate divergence looks like:
  `gentle-ai-branch-pr` is prefixed and therefore exempt by the convention, so the
  check is only as good as the naming, and a skill that *should* have been prefixed and
  was not would report as a violation of portability rather than of naming.
- [x] **The cache key should carry the rubric edition.** Done 2026-08-23, and the hole was
  worse than the entry states: there was **no rubric edition anywhere**, and `scores.Entry`
  keyed judge bases on the skill content hash alone.
  `Config.Edition()` has two components, because one cannot cover both halves. The **data**
  — dimension numbers, weights, needs-judge, and the three word lists — is hashed. Not
  `Name`, `Key`, or `Response`: the first two are labels and the third describes how a
  dimension reacts rather than what it scores, and folding them in would invalidate every
  cached grade on a comment edit. The thresholds written in Go cannot be hashed, so
  `scoringRevision` is a constant the author turns by hand.
  **`TestScoresAreStable` is what makes that constant more than a comment**: four fixtures
  with pinned scores, and any scoring change fails there first with a message saying to bump
  it. I set those golden values by guessing and all four were wrong; they are now measured.
  `scores.Entry.Rubric` records the edition and `Bases` routes a mismatch to the existing
  **stale** verdict — right text, wrong rules is as stale as wrong text, and reusing the
  disposition means no second place has to remember to handle it. Empty is unknown, not
  current.
  **Two consequences worth stating.** Every scores file written before today goes stale on
  upgrade and must be re-judged; that is the fail-closed choice and it is deliberate. And
  the round-trip test caught that `cmd/scores` — the writer this repo ships — emitted a
  document its own reader rejected; it now records the edition. `skillsaw hash --rubric`
  prints it, since a cache key has two inputs and `hash` already provided one.
  Original entry: gnosis's relay key
  deliberately omits its `standards/` version, and the reason it can is that the
  rubric never enters the prompt there — a threshold change cannot stale a model's
  reply about a source. Here the rubric **is** what is being applied, so a key without
  it serves yesterday's grade under today's rules, and the failure is silent: the score
  is returned, it is a number, and nothing says which edition produced it.
  Whatever identifies the edition has to change when a dimension's *scoring* changes
  and not when its prose does, or every comment edit invalidates every cached grade.
- [x] **A known-answer soundness test per rule.** Done 2026-08-23 as `Config.SelfTest`.
  **Only the negative half transferred, and that is the whole of it.** gnosis's rules are
  regexes, where "this pattern must match this string" is a real assertion — it caught a
  pattern whose own example was two characters too short. skillsaw's rules are
  `strings.Count(prose, term)` and `strings.HasSuffix(desc, tail)`, so a positive example
  is a string containing the substring and asserts nothing. The entry's own argument
  carries the weight: the negative case is the one that matters and the one nobody writes
  unprompted.
  **The self-test had to be made to share the checks' matcher.** A soundness check that
  matched by its own rules would report on a rule nobody applies. `SlopHits`,
  `MarkerHits`, and `HasFillerTail` are now the single definition, called by scoring and by
  `SelfTest`, and `TestTheChecksUseTheMatchersTheSelfTestChecks` fails if scoring stops
  using them.
  **Four false alarms found, and two of my six were my own measurement error.**
  `FillerTails` is anchored with `HasSuffix` against the *description*, so "run it as needed
  by the operator's schedule" never reaches it, and `⚠️` matching a real warning marker is
  correct. The genuine four are recorded in `quiet.go` with reasons rather than fixed:
  - `首先` / `其次` in `Slop`. The sharpest of them: in a workflow skill these are *step
    ordering*, which dim 2 rewards, and dim 7 charges a point for each occurrence.
    **This is an argument for removing them, not a decision** — a word list change moves
    every score in the corpus and no corpus is at hand to measure by how much.
  - `STOP` / `CHECKPOINT` in `CheckpointMarkers`, firing on "do not STOP the deployment"
    and "the CHECKPOINT table". A matcher question rather than a list question — a bare
    substring cannot tell a marker from a noun — so it belongs with making the rubric data.
  A tolerated exception that stops firing **fails**, same rule as the mutation table's
  blind spots. Not run at load today: the lists are a compiled-in Go literal, so a test
  already is the load-time check; `SelfTest` is a method so a file-backed ruleset can call
  it later. Original entry: `gate.SelfTest`'s planted-defect
  control generalised: every rule ships a case it must flag and a case it must not,
  and the ruleset refuses to load if any rule fails either. gnosis now does this for
  its §9.3 pattern table — `scan.LoadRules` runs the examples at load rather than in a
  test, on the argument that a test catches the same defect one commit later and only
  for whoever ran it — and it paid for itself immediately by catching a pattern whose
  own positive example was two characters too long to match.
  **Soundness before completeness**, because trust is more sensitive to false alarms
  than to misses: a rule that fires on ordinary work gets the whole tool switched off,
  where a rule that misses something gets it fixed. The negative case is the one that
  matters, and it is the one a rubric author will not write unprompted.

## `activation` Scores a Description It Could Not Read (2026-08-23)

Found while measuring 60 real book skills for the verdict-taxonomy item. Confirmed on
skillet **v0.20.0**.

`skill.Load` leaves `Description` empty when the YAML frontmatter fails to parse, and
records why in `Skill.FrontmatterErr` — a field skillet has carried since **v0.9.0**. Three
consumers took the guard that field exists for: `internal/lint` stopped comparing a name it
could not read, `redlines` stopped demanding a trigger from a description it could not read,
and `speclint` reports the parse error as itself. `skillsaw`'s dim 1 consults it too
(`internal/rubric/rubric.go:307`, penalty 6, "frontmatter did not parse").

**`cmd/activation` is the one consumer that never took it.** It reads `s.Description`, gets
`""`, and computes a full confusion matrix against the empty string.

- [x] **Report an unreadable description instead of scoring against it.** Done 2026-08-23.
  An unscoreable description is now a third unmeasured cause in `scoreDir`, beside the
  missing `SKILL.md` and missing `test-prompts.json` added earlier the same day.
  **The guard the entry proposed would have been half a guard.** It named `FrontmatterErr`,
  and measuring found a second cause with no parse error to catch: a skill whose frontmatter
  parses cleanly and declares `description: ""` produces the identical output — a full
  matrix over the empty string, and the same wrong advice. Over the 60-skill corpus the
  split is **22 unparsed and 8 parsed-but-empty**, so guarding only the documented cause
  would have left 8 skills scoring silently. The condition is "there is no description to
  score", and the two causes are reported apart because they want different repairs: a YAML
  fix versus an authoring one.
  After the change no skill in that corpus produces a confusion matrix from an empty
  description: 30 of 60 report `UNMEASURED`, and none of the 30 still scored carries a
  target. Verified against disabling the empty-description branch, which puts `net_utility`
  back on a skill with nothing to score.
  Deliberately no new penalty: `eval` already prices both defects at dim 1 (6 and 3).
  `activation` only declines to pretend it measured something. Measured over 60
  book skills carrying real prompt files: dim 1 says the frontmatter did not parse for
  **22 of 60**; **18** skills carry at least one target; **all 18 are among the 22**;
  and across the corpus **0 of 60 targets** and **0 of 38 distractors** fired.

  So the entire activation result on this corpus is an artifact: every target-bearing
  skill was scored against an empty description. There is no evidence here about the
  overlap proxy's calibration, because it was never given a description — which is worth
  stating, since a 0/60 hit rate otherwise reads as a damning result about the proxy.
  The per-prompt explanations make it worse than silent: they say *"target MISSED
  (description vocabulary misses it)"* when the truth is that there is no description.
  The fix is the guard the rest of the family already applies — when `FrontmatterErr` is
  non-nil, report the skill as `UNMEASURED` with that cause rather than scoring it.
  `activation` already grew an unmeasured state today for a missing `SKILL.md` and a
  missing `test-prompts.json`; this is a third cause routing to the same place.
- [x] **The `EvidenceIn` caveat added today misdirects on exactly these skills.**
  **Subsumed by the item above, and this entry's own prescription was wrong.** I filed it
  saying the new cause "outranks both existing caveats because it explains them" — an
  ordering rule. That assumed the caveat still prints. It does not: an unscoreable skill
  returns before scoring, so the report carries no counts, and `emit` returns before the
  caveat line anyway. Both protections are independent, which I only learned by planting
  each: removing either leaves the misdirection absent. So there is no ordering to write,
  and a reader following the original instruction would have implemented it for a message
  that never fires.
  What was real is a contract gap rather than a bug. `EvidenceIn` takes a `*ratchet.Report`,
  which carries no trace of the description behind it, so nothing in the type stops a future
  caller handing it one scored against `""`. It now states that precondition with the
  measurement attached. `TestNoDecoyCaveatWithoutADescription` pins the user-visible
  outcome and says outright that it does not guard a particular mechanism, because the
  planting showed it cannot. *A defect
  in work from this session, recorded rather than left to be rediscovered.*
  `noise.EvidenceIn` reports "no decoy ever fired: … Write harder decoys." on a report
  with distractors and `FP == 0`. For all 18 target-bearing skills above that advice is
  wrong: the decoys are fine, the description is empty, and nothing could have fired.
  An author following it would write harder decoys and watch the number not move.
  Fix is small and sits beside the item above — an unreadable description means *neither*
  half of the matrix is evidenced, and it outranks both existing caveats because it
  explains them. Ordering matters here the way it does for collision-over-divergence in
  `portability`: report the cause, not the symptom.

**Corpus note, not a code task.** The 22 unparsed blocks share one shape, already named in
dim 1's own test: a quoted scalar followed by unquoted text. In this corpus it is
`source_book: "Working Effectively with Legacy Code" by Michael Feathers (2005) + "…"`.
Repairing those is authoring work in the books tree; exegesis already handles the parse
failure gracefully rather than compounding it (its `Skip the name/folder check when the
frontmatter did not parse` entry, closed 2026-08-06). Recorded here because this is where
it was measured, and because 22 broken blocks means 22 skills whose `name` is also empty.

## First Harness Run — and the Question It Raises (2026-08-24)

The adversarial phrasing corpus ran against `climax-cli-scaffold` on Gemini CLI 0.46.0.
**All nine phrasings scored `first`** — including the time-pressure variant (*"Don't waste
time — just get straight into it and start immediately"*), the pre-summarised workflow, and
the request buried among three unrelated asks, where the skill loaded before even an
`ls -la`. Verified by reading the tool sequences, not just the verdicts: every run went
`update_topic` (planning, allowlisted) → read of `SKILL.md` → work.

- [x] **Is the corpus discriminating, or is it saturated?** **Answered 2026-08-24: it
  discriminates.** A second subject, `matryer-decode-valid`, scored **7 of 9** — the two
  failures being `03-imperative` and `08-buried-in-a-list`, against `climax-cli-scaffold`'s
  clean sweep. So a `first` result is a property of the subject, not a corpus that cannot
  fail anything.

  **The two failures came from different mechanisms**, which is stronger than two of a kind:
  - `03-imperative` — `glob` ran first. The agent went looking at the codebase before
    loading the skill.
  - `08-buried-in-a-list` — `run_shell_command` with
    `find . -type f -name "*config*" …` ran first. The prompt's **first bullet** is *"Check
    whether the config file still has the old timeout in it"*, and the agent serviced that
    neighbouring ask before reaching the skill request in bullet two. Precisely the failure
    the prompt was constructed to induce, visible in the command it chose.

  Note `04-time-pressure` passed on both subjects. Pressure alone did not defeat either one;
  what did was giving the agent something concrete to do first.

  **Limits.** Two subjects, one run per phrasing, a nondeterministic agent, one model, one
  day. This establishes that the corpus can separate subjects — the question that was
  asked — and nothing about how good either skill is.

- [x] **Does the no-trigger redline predict ordering failure?** **ANSWERED 2026-08-25: no.
  The prediction was recorded before the run and the run falsified it.**

  `context-key-collision-prevention` — **not** flagged by the redline, predicted to sweep
  9/9 — split on **6 of 8** repeated phrasings. Over 24 runs: 15 `first` (62%), 6
  `not-triggered` (25%), 3 `after-action` (12%). Two phrasings produced `after-action`, the
  failure mode the redline was supposed to predict, on a skill the redline cleared.

  So a description stating an explicit trigger condition does not protect against the agent
  starting work first, and the earlier correlation with `matryer-decode-valid` was one
  subject wide. The hypothesis is closed, not deferred: it was tested and it is wrong.

  **Verified as behaviour, not as a detector gap**, which is the failure mode that would
  have faked this result. Every `not-triggered` run was inspected: the agent went straight
  to `grep_search` and `list_directory` without reading the skill. Across all transcripts
  there are 29 skill loads, all by `read_file` or a shell `cat`, all detected, and no
  `activate_skill` call anywhere.

- [x] **Single-run corpus results are not reliable, and two earlier ones were single-run.**
  Completed 2026-08-25: Re-ran both `climax-cli-scaffold` and `matryer-decode-valid` with `--repeat 3`
  to gather reliable repeated measurements.

  **Results summary:**
  * `climax-cli-scaffold` achieved **21/24 (87.5%) first runs**:
    - `01-bare (control)`: 3/3 first
    - Unanimously `first` in `02`, `03`, `04`, `05`, `06`, `07`, `09` (7 of 8 phrasings)
    - `08-buried-in-a-list`: unanimously `after-action` (0/3 first)
  * `matryer-decode-valid` achieved **13/24 (54.2%) first runs**:
    - `01-bare (control)`: 3/3 first
    - Unanimously `first` in `03` and `07` (2 of 8 phrasings)
    - Split in `02`, `04`, `06` (2/3 first each)
    - Split in `05` (1/3 first, 2/3 after-action)
    - Split in `08` (0/3 first: 2/3 not-triggered, 1/3 after-action)
    - Unanimously `not-triggered` in `09` (0/3 first)

  **Findings & Impact:**
  The 9/9 vs 7/9 single-run comparison was indeed a statistical fluke. Under repeated runs,
  the true gap is between an 87.5% binding skill (`climax-cli-scaffold`) and a 54.2% binding
  one (`matryer-decode-valid`). Repetitive measurement is essential to filter out LLM
  stochasticity and separate true structural resilience from lucky single runs.

- [x] **The harness runs the control once, and this run shows why that is not enough.**
  Fixed 2026-08-25: under `--repeat N` the control repeats with everything else and must be
  **unanimously** `first`. A split control now prints its tally and refuses the other
  eight — verified: a 2/3 control exits 1 having printed zero rows after it. The single-run
  path is unchanged and now labels itself *"1 run — not repeated"* rather than presenting
  one sample as a baseline. Original entry:
  `01-bare` gates every other reading — if the bare request does not load the skill, the
  other eight are uninterpretable — and `run.sh` captures it exactly once even under
  `--repeat N`. On a subject where 38% of runs are not `first`, a single control can pass or
  fail by luck, and a lucky pass silently licenses eight rows that should have been refused.
  The fix is to repeat the control with everything else and require it to be *unanimously*
  `first`, since a split control is precisely the case where the rest cannot be read.
  skillet v0.22.0 carries the fix and skillsaw is bumped to it.** Re-measured over the same
  286 skills: the trigger redline now flags **1**, down from 5.

  **The prediction, written before the runs rather than after.** The entry that raised this
  is the one that must not be checked against a rationalised result, so it is recorded here
  and the run either matches it or does not:

  | subject                            | flagged now | predicted corpus result     |
  | ---------------------------------- | ----------- | --------------------------- |
  | `matryer-decode-valid`             | yes         | fails at least one phrasing |
  | `context-key-collision-prevention` | no          | sweeps 9/9                  |
  | `option-configuration-patterns`    | no          | sweeps 9/9                  |
  | `pbt`                              | no          | sweeps 9/9                  |
  | `webapp-review`                    | no          | sweeps 9/9                  |

  **`matryer-decode-valid` is not independent evidence and must not be counted as such.** It
  was chosen *because* it was flagged, and it has already failed `03-imperative` and
  `08-buried-in-a-list`. Its failing again confirms nothing; the four newly-cleared skills
  are where the hypothesis can actually die. If any of them fails a phrasing, the redline is
  not predicting ordering behaviour and the earlier correlation was one subject wide.

  **What would make the result meaningless.** All five sweeping, which would say the corpus
  stopped discriminating rather than that the redline predicts nothing — check the corpus
  still separates before reading the redline into it. And a single run per phrasing on a
  nondeterministic agent: `--repeat 3` costs three times as much and is the difference
  between a verdict and a coin flip, given `03-imperative` has failed exactly once so far.

  Original suspension note follows. **Suspended 2026-08-24 — the signal was broken.** `redlines.checkTrigger` clears a description containing any of `when`,
  `whenever`, `invoke`, `reach for`, and `before`/`after` (each with a trailing space) — and **the list omits "trigger"**.
  A description reading `Trigger: user is using context.WithValue …` is flagged as stating
  no trigger. Confirmed directly against `redlines.Check`, and filed in `skillet/TODO.md`.
  Two of the operator's five flagged skills are false positives on exactly this. Testing
  whether the redline predicts ordering failure while it is wrong for a large share of its
  hits would measure the bug, so this waits on the upstream fix. Original entry: A hypothesis this run
  raises and cannot settle. `matryer-decode-valid` was chosen *because*
  `preflight --redlines` flagged its description as stating no trigger — it opens
  *"Eliminate repetitive decode-then-validate boilerplate…"*, an instruction, where
  `climax-cli-scaffold` opens *"Invoke when initializing a new Go CLI application…"*, a
  situation. The weaker description did score worse, as predicted.
  That is n=2 and the prediction was made by the person reading the outcome, which is the
  shape of reasoning this repo distrusts everywhere else. Testing it means picking further
  subjects by the redline **before** running them and recording the prediction first. Of
  285 installed skills only four are flagged, and three of those state a trigger in a form
  `speclint` does not recognise (`Trigger:`, `Triggers on:`) — so the flagged population is
  nearly exhausted, and that near-miss is itself worth a look.

- [ ] *(superseded — kept for the original reasoning)* **Is the corpus discriminating, or is it saturated?** Nine passes on the first subject
  cannot tell those apart, and it is the same state `noise`'s `CEILING-NEEDS-HARDER-DATA`
  reasoning names: a suite on which everything passes carries no information about the
  suite. Two readings fit — the skill and agent genuinely withstand all nine pressure
  shapes, or the phrasings are not adversarial enough to separate anything.
  What settles it is a subject that fails. Run the corpus against a skill with a weak or
  ambiguous description and see whether any phrasing produces `after-action` or
  `not-triggered`. Until one does, a `first` sweep is not evidence the corpus works.
  The corpus is cheap to run and the result is one line per skill, so this is a sweep rather
  than a project — but note the cost: nine agent calls per skill, and one run took ~19
  minutes when the API errored and retried.

- [ ] **`activation`'s first real measurement, and the question under it.** After the corpus
  repair, `books/` scores 246 of 277 skills: **recall 373/396 (94%)**, but **false positives
  156/237 (66%)** — decoys fire two times in three. Gates: 42 clear, 44 unresolved.
  The FPR has two readings the confusion matrix cannot separate: descriptions over-trigger,
  or the decoys are drawn from the same book domain and share vocabulary with the
  description by construction. `ratchet.Score` is salient-term overlap with a five-character
  minimum, so same-domain decoys would fire on it regardless of how precise a description
  is. Settling it needs observations of what a real agent does with those prompts — which
  is what the harness now produces.
  What is safe to say: the precision half of the matrix is **evidenced** for the first time.
  Before the repair every FPR in that corpus was computed from an empty description.

**Detector gap found by the first run, and fixed.** Gemini CLI has a dedicated
`activate_skill` tool that none of three probes reached, because in every captured run the
agent got to the skill by reading `SKILL.md` first. It names the skill in `parameters.name`,
which neither the `skill`-key lookup nor the `<name>/SKILL.md` path fallback would have
caught — a run going straight to it would have scored `not-triggered`. The nine verdicts
above were correct only because a file read preceded it every time. Recognised now by tool
name rather than by any `name` parameter, since `name` is far too common to treat as a skill
reference; the captured event is a test fixture.

## `preflight` Discards a Batch Over One Unloadable Directory (2026-08-24)

Found while using `preflight --redlines` to pick a corpus target: 285 skill directories
produced nothing, 40 worked. **One directory among them has no `SKILL.md`** —
`~/.agents/skills/unconventional-commits-workspace/`, a support folder holding
`check_commits.py` and an `inputs/` tree — and it takes the whole run with it.

- [x] **Report an unloadable directory instead of aborting the run.** Done 2026-08-24, the
  same shape `activation` took the day before: the directory becomes a defect about itself
  and the run continues. Confirmed on the real tree — one invocation over 286 skills now
  reports 286 where it previously reported none.
  **The abort was hiding a result, not just suppressing output.** Chunking around it had
  found 4 skills failing the no-trigger redline; the unchunked run finds **5**. Verified
  against restoring the early return. Sibling audit done as the entry asked: `activation`,
  `portable`, `eval` and `diagnose` all report-and-continue, so `preflight` was the last.
  Original entry:
  `cmd/preflight/preflight.go:147` returns on the first `skill.Load` failure:

  ```go
  s, err := skill.Load(dir)
  if err != nil {
      return fmt.Errorf("preflight: %w", err)
  }
  ```

  Two consequences, both observed. Every result already computed is discarded — 284 skills
  that loaded fine are never reported. And because the error reaches `ff`'s handler it
  **prints the full usage text**, so a missing file reads as though the caller mistyped a
  flag.

  **This is the same defect, in the same shape, that `activation` was fixed for on
  2026-08-23** — see *`activation` Scores a Description It Could Not Read* above, where a
  missing `SKILL.md` or `test-prompts.json` used to abort a 50-skill batch. That fix
  introduced a per-skill `Unmeasured` reason, kept the run going, and still exited non-zero
  after reporting rather than instead of it. `preflight` should route the same way: a
  directory that cannot be loaded is a defect *about that directory*, not a reason to stop.

  It is not merely cosmetic. `preflight` is the structural gate an optimize loop runs
  between writing an edit and deciding whether to keep it; a corpus-wide invocation that
  silently returns nothing because one adjacent folder lacks a `SKILL.md` is a gate that
  reports clean by failing to run.

  Worth checking the other multi-argument commands for the same shape while fixing this.
  `activation` and `portable` handle it; `eval` skips-with-a-note; `preflight` and
  `diagnose` are the ones to look at. `diagnose` already reports and continues
  (`skip %s: %v` to stderr), so `preflight` may be the last one.

## The Phrasings Are Not Equally Hard, and One Does Most of the Work (2026-08-26)

> **Superseded in part, 2026-08-27.** The table below was computed by an `OrderOf` that
> reads a parallel tool batch as a sequence. The `08` column — the entry's whole argument —
> is the column most exposed to that defect, because `08` is the phrasing that provokes a
> wide concurrent batch. Do not cite the numbers until they are recomputed. See *`OrderOf`
> Reads a Parallel Tool Batch as a Sequence*, below.

Three subjects have now been run with `--repeat 3`, which is enough to compare *phrasings*
rather than skills. That comparison lives in no entry — it is visible only by reading three
per-subject tables against each other — and it is the most transferable thing the corpus
has produced, because it is a claim about the instrument rather than about any skill.

First-verdict counts, nine runs per phrasing (three subjects x three repeats):

| phrasing                | climax | matryer | ctx-key | total   |
| ----------------------- | ------ | ------- | ------- | ------- |
| `04-time-pressure`      | 3/3    | 2/3     | 3/3     | **8/9** |
| `07-accepting-an-offer` | 3/3    | 3/3     | 2/3     | **8/9** |
| `02-with-context`       | 3/3    | 2/3     | 2/3     | 7/9     |
| `03-imperative`         | 3/3    | 3/3     | 1/3     | 7/9     |
| `05-pre-summarised`     | 3/3    | 1/3     | 3/3     | 7/9     |
| `06-user-pre-explains`  | 3/3    | 2/3     | 1/3     | 6/9     |
| `09-negated-nearby`     | 3/3    | 0/3     | 1/3     | 4/9     |
| `08-buried-in-a-list`   | 0/3    | 0/3     | 2/3     | **2/9** |

**`08-buried-in-a-list` is the instrument.** At 2/9 it is twice as discriminating as the
next-worst phrasing and four times the field average, and it is the only one that fails a
subject *unanimously* — 0/3 on both `climax-cli-scaffold` and `matryer-decode-valid`,
skills whose overall rates are 87.5% and 54.2%. A pressure that defeats the strongest
subject as completely as the weakest is measuring something the others are not.

**And the pressure everyone expects to matter does not.** `04-time-pressure` — *"Don't
waste time, just get straight into it and start immediately"* — is joint **best** at 8/9.
Explicitly telling the agent to skip preliminaries is close to harmless. What defeats it is
being given something else to do first: `08` buries the request as the second of three
unrelated asks, and the agent services the first one.

- [ ] **Decide what an unequal corpus means for how it is read and reported.** Two readings,
  and the data does not choose between them:
  - **The corpus is working and `08` is why.** Then a subject's headline number is
    dominated by one phrasing, and reporting `21/24` hides that the failure is entirely in
    one row. A per-phrasing table should lead, not the total.
  - **`08` is a different test wearing the same clothes.** The other eight vary *how the
    request is phrased*; `08` varies *what else is in the message*. That is arguably a
    distinct axis — competing demands rather than adversarial phrasing — and averaging it
    with the rest produces a number about neither.
  Owed before either is adopted: a fourth subject, to check `08`'s dominance is not an
  artefact of three. If it holds, splitting the report is cheap and the alternative is a
  headline figure that mostly reports one row.

- [ ] **`09-negated-nearby` may be measuring the wrong thing.** Second-worst at 4/9, and
  its two failures are `not-triggered` rather than `after-action` — the skill never loaded
  at all. That is what `activation` diagnoses, not what ordering does. The prompt places a
  negation next to the request (*"Don't bother with anything heavyweight or
  process-driven"*), and a skill that then fails to load may be responding to the negation
  rather than being beaten to the punch. Worth separating: if `09` mostly produces
  `not-triggered`, it belongs with trigger accuracy and not in a corpus about ordering.

## The After-Action Verdict on `08` Is Partly Built into the Prompt (2026-08-26)

> **Largely superseded, 2026-08-27.** The mechanism proposed here — the agent services the
> config-file item, then loads the skill — is not what the transcripts show. It issues both
> at once. The `OrderOf` defect below is the real cause. What survives is the meta-test
> methodology note, which is restated in the new entry's last item.

The L1346 meta-test was run twice against `climax-cli-scaffold` on `08-buried-in-a-list`,
and the pair is more informative than either half.

The first run substituted the bare `{{TASK}}` string and lost the surrounding list, so it
asked about a message that never failed. It answered that the task did not match the
skill's trigger list (*"initializing a new Go CLI application... adding new subcommands...
drifted from the canonical ff/v4 pattern"*) and so the skill would not be loaded — an
account of `not-triggered`. The measured verdict was `after-action` 3/3: the skill *did*
load. The account explained an outcome that did not occur, and the measurement refuted it.

The second run substituted the real three-item prompt. It answered that it decomposes a
bulleted list and executes the actionable items in encounter order, and that it treated
loading the skill as *"one independent task among three, rather than a blocking
pre-condition for the entire turn"* — naming the config-file search specifically. That is
an account of `after-action`, and it matches.

**The code says the same thing, and says it is close to forced.** `OrderOf` sets
`acted = true` on any tool outside `planningTools()`, and the first list item — *"Check
whether the config file still has the old timeout in it"* — cannot be answered without
reading a file. So an agent that services the items in the order they are written earns
`after-action` before it ever reaches the request. The 2/9 rate is at least partly a
property of the prompt.

**This makes `08`'s `after-action` a different event from `01-bare`'s.** In `01` nothing
else was asked, so acting before the skill loads means acting on the skill's own task —
the defect the verdict is named for. In `08` it can equally mean answering a neighbouring
question and then loading the skill promptly. The two are pooled under one word, and a
subject's headline number adds them together. This sharpens the second reading in the
entry above from "arguably a distinct axis" to a mechanism.

- [ ] **Settle per-run whether `08` failures serviced a neighbour or started the subject's
  own work.** `Before()` already returns exactly this — the non-planning tools that ran
  ahead of the skill, in order — and a config-file `read_file` reads very differently from
  an edit to the CLI being scaffolded. Nothing needs writing to answer it. What blocks it
  is that `run.sh` defaults `--out` to `mktemp -d` and the completed runs were not kept, so
  the transcripts that would decide it are gone. Re-run the three subjects on `08` with an
  explicit `--out`, then report `Before()` alongside the verdict.

- [x] **Report the tools-before list wherever `after-action` is reported.** Done 2026-08-27. If the two
  meanings above both turn out to occur, the verdict alone is not actionable and the fix
  differs between them. `Before()` exists and is unused by the harness summary.

- [ ] **Constrain the meta-test to the exact failing prompt, and check its answer against
  the verdict.** Both runs picked the same shape — *"the skill was clear, I chose to start
  working anyway"* — while giving mechanisms that imply different verdicts, so the shape
  taxonomy carries little information on its own and the mechanism is the payload. One
  cheap guard follows: an account that implies `not-triggered` for a run measured
  `after-action` is refuted, and can be discarded without judgement. Reconstruction from a
  cold session is a hypothesis; resuming the failing session would be a report, and needs a
  stable per-phrasing working directory plus `--session-id` in `run.sh`.

## `OrderOf` Reads a Parallel Tool Batch as a Sequence (2026-08-27)

`climax-cli-scaffold` was re-run on all nine phrasings with `--repeat 3` and an explicit
`--out`, so the transcripts survived and `Before()` could finally be asked. It reported
`08-buried-in-a-list` as SPLIT — 2 first, 1 after-action — where the previous run had it
0/3. That instability is the finding: the two runs are the same behaviour.

Both issue a single parallel tool batch, four calls, no result returned in between:

| run 1 — `after-action`   | run 2 — `first`          |
| ------------------------ | ------------------------ |
| `update_topic`           | `update_topic`           |
| `glob **/*config*`       | `read_file` .../SKILL.md |
| `read_file` .../SKILL.md | `glob **/*config*`       |
| `read_file` MEMORY.md    | `read_file` MEMORY.md    |

Emitted across 742ms and 576ms respectively; every `tool_result` in both runs arrives after
the last of the four. **The verdict turns on the emission order of two concurrent calls
whose results had not come back.** Nothing was learned from the `glob` before the skill was
read — its output was `No files found`, and it had not yet been delivered.

`OrderOf` walks a flat slice and sets `acted` on the first non-planning tool, so it cannot
see this: `Read` discards `tool_result` records, and batch structure is gone before
`OrderOf` runs. The rule it encodes — *work began before the skill loaded* — requires that
a result was available to work from. Concurrent issuance is not work.

Recomputed over all 27 captured runs, treating a non-planning tool as action only when it
sits in a **strictly earlier batch** than the skill load: exactly one verdict changes,
`08-buried-in-a-list.1` from `after-action` to `first`. It was the run's only failure, so
`climax-cli-scaffold` is 9/9 with no ordering defect at all. The change is strictly more
lenient, so no `first` can become a failure and the other 26 are unaffected by
construction.

- [x] **Give `ToolUse` a batch index and make `OrderOf` compare batches.** Done
  2026-08-27. `Read` numbers batches lazily — the counter advances when a use arrives after
  a result, not at the result — so the run of consecutive results that ends a four-call
  batch advances it once. `OrderOf` returns `OrderAfterAction` only when the earliest
  non-planning use is in a lower-numbered batch than the load; `Before()` filters the same
  way. All pure.

  Two zero-value rules make it fail closed, and both matter more than the batching itself:

  - **A zero batch means unknown, and unknown falls back to document order** — the stricter
    reading. Every hand-built slice carries zeros, so the alternative would have quietly
    voided the existing table.
  - **A transcript that logs no results at all has its batch numbers withdrawn.** Without
    results there is nothing to divide batches by, so every use lands in batch 1 and reads
    as one enormous concurrent burst — clean, always. Found by the `cmd` tests, whose
    fixtures log uses without results. The tempting fix was to add results to the fixtures;
    that would have left any real transcript in a result-free format silently passing.
- [x] **Plant the negative control before trusting it.** Done 2026-08-27, twice. The
  `Read` table was checked against a plausible wrong implementation — increment at the
  result rather than lazily — and three rows caught it across both runtime dialects. The
  `OrderOf` table was run against the unmodified function first: the run-1 row failed
  `after-action`/`first` and two `Before` rows failed, which is the whole reason the pair
  is in the file. A row with a genuine result-then-act sequence guards the opposite cheat,
  since "always return `first`" passes the concurrent rows.

  One caveat on the third test,
  `TestNumberingEveryUseSeparatelyReproducesDocumentOrder`: it passed before the change as
  well as after. It is a guard against future loosening of `<` to `<=`, not a control for
  this one, and should not be read as evidence the change worked.
- [x] **Re-score the captured corpus.** Done 2026-08-27. `ordering --agreement` over all 27
  transcripts reports `first in all 3 runs` for every phrasing, exit 0 — one flip from the
  flat reading, matching the recomputation exactly. `climax-cli-scaffold` has no ordering
  defect on any of the nine.
- [ ] **Recompute the phrasing table, and re-ask whether `08` discriminates.** The
  superseded entry above claimed `08` is the instrument at 2/9. That claim is now
  unsupported and plausibly backwards: `08` asks for three things, so it provokes the
  widest concurrent batch, so it has the most chances for a non-skill call to land earlier
  in the batch. `08` may have been measuring batch width. The `matryer` and `ctx-key`
  transcripts were written to `mktemp -d` and are gone, so this needs re-running with
  `--out` before anything is concluded — including whether the corpus discriminates at all.
- [x] **Have `run.sh` default `--out` to a durable path.** Done 2026-08-27. Three findings in two days have
  been blocked on transcripts that no longer exist. A run whose evidence is discarded by
  default cannot be re-examined when the reading of it is questioned, which is exactly when
  it is needed.
- [ ] **Restated from the superseded entry: constrain the meta-test to the exact failing
  prompt, and check its answer against the verdict.** Both meta-runs picked the same shape
  while giving mechanisms implying different verdicts, so the shape taxonomy carries little
  on its own. Note that the second meta-answer — decompose the list, execute in encounter
  order — was *also* wrong: the transcripts show one concurrent batch, not sequential
  servicing. It was plausible, matched the verdict, and still misdescribed the behaviour.
  That is a stronger caution than the first failure, because consistency with the verdict
  was the check proposed for catching this.

## An Unrecognised `activate_skill` Inverts the Verdict (2026-08-27)

A fresh 27-run capture of `climax-cli-scaffold` reported `06-user-pre-explains` as SPLIT,
2 first and 1 after-action. `Before()` named the tool that supposedly came first:
`activate_skill`. The run was this, and it is the best-behaved run in the corpus:

```text
batch1  activate_skill  {"skill_name": "climax-cli-scaffold"}
batch2  update_topic, read_file .../climax-cli-scaffold/SKILL.md
```

It activated the skill as its very first tool call, before anything else, and scored
`after-action` **for having done so**. `skillIn` read the declared skill from `name`, and
Gemini wrote it under `skill_name`, so the load went unrecognised — and an unrecognised
`activate_skill` is not ignored, it is an ordinary tool, which counts as work. The verdict
was not merely wrong, it was the exact inverse of the behaviour.

Both spellings appear in the same 27 runs from the same tool: `skill_name` twice
(`05-pre-summarised.2`, `06-user-pre-explains.3`) and `name` once
(`05-pre-summarised.1`). Neither is *the* spelling, which is why the fix is a list rather
than a corrected constant.

Fixed 2026-08-27, with both spellings checked and a negative control seen to fail: the
planted single-spelling version fails the unit case and the end-to-end one, the latter
reporting `Before() blames [activate_skill]`. A row for an ordinary tool carrying a `name`
parameter guards the inverse mistake — reporting a load that never happened, which is the
more flattering error and so the one worth pinning.

With this and the batching fix in, all nine phrasings read `first in all 3 runs`, exit 0.

- [ ] **Re-examine every `after-action` ever recorded for this cause.** Two independent
  defects have now each produced a false `after-action`, and the corpus's entire claim to
  discriminate rests on that verdict. Neither was visible without opening the transcript.
  Nothing recorded before 2026-08-27 should be cited until re-scored.
- [x] **The harness runs whatever `skillsaw` is on PATH, and said nothing about it being
  three days stale.** Closed by the provenance header below, which also found that the
  version string alone would not have caught it.
- [x] **Find the remaining spellings before they cost another verdict.** Answered
  2026-08-27, and it refuted the fix above. Gemini CLI 0.46.0 documents **one** argument —
  `name` — in `bundle/docs/tools/activate-skill.md`, and `skill_name` appears nowhere in
  the bundle. Both captured `skill_name` calls came back *"Tool activate_skill not found.
  Did you mean one of: write_file, update_topic, read_file"*: under `--approval-mode plan`
  the tool is absent, and the model invented the call along with the parameter. Honouring
  it credited a failed call to a non-existent tool as a loaded skill. Reverted the same
  day; `skillIn` now carries a note so the next reader who meets `skill_name` in a
  transcript does not re-derive the wrong fix from the same two calls.
- [x] **`run.sh` reports the instruments it used.** Done 2026-08-27, and the obvious
  version string was not enough. `skillsaw version` prints `BuildDate` from the *module
  pseudo-version*, not the build: a `go install` at 09:08 today still reported
  `BuildDate: 2026-08-25T03:21:10`, so two builds days apart from the same dirty tree are
  identical in every field it prints. The binary's mtime is the field that differs, and is
  what the header now carries, alongside the resolved path, `GitVersion`, a dirty-tree
  warning, and the gemini version when the agent actually ran. Written to
  `$OUT/provenance.txt` as well as stdout, since the transcripts outlive the scrollback.

## Twenty of Twenty-Seven Runs Never Loaded the Skill (2026-08-27)

Teaching `OrderOf` to skip calls the runtime rejected — the fix for the errored
`activate_skill` being reported as work done first — turned the control red, and the
control was right. `01-bare` had been passing 3/3 on two calls that both failed:

```text
read_file          error: Path not in workspace: "/Users/steve/.agents/skills/
                          climax-cli-scaffold/SKILL.md" resolves outside the workspace
run_shell_command  error: Tool execution denied by policy. You are in Plan Mode with
                          access to read-only tools.
```

Three ways in, all shut. `activate_skill` is not in the plan-mode tool set; `read_file`
refuses a path outside the workspace, and the skills live in `~/.agents/skills`; the `cat`
fallback is denied as script execution. **The agent never read the skill.**

Across the corpus, a load attempt succeeded in **7 of 27 runs**. Re-scored per run:
19 `not-triggered`, 6 `after-action`, 2 `first`.

Every ordering number this harness has produced under `--approval-mode plan` was awarded
for a *refused* attempt to read the skill. The instrument was not measuring when the skill
loaded; it was measuring the order of attempts, and 20 of 27 of those failed. The phrasing
table, `08`'s apparent discriminating power, the 9/9 sweep from earlier today — all of it
was scored on transcripts in which the skill was never in context.

The control did exactly what it exists for, three days later than it should have, because
until today a failed read counted as a load.

- [x] **Decide how the harness gives the agent access to the skill.** Decided 2026-08-27:
  **drop `--approval-mode plan`.** The re-run itself is the three items below.

  Four options were weighed. Two of them — running from a working directory that contains
  the skills, and copying the subject skill into each run's workspace — open `read_file`
  and leave `activate_skill` absent, so they measure the *fallback* path. That is the
  decisive argument against both: the corpus asks when the skill loads relative to work,
  and under Gemini the intended mechanism is `activate_skill`, which succeeded in exactly
  one of 27 captured runs. Either option would produce green numbers while leaving the
  primary path as untested as it is today, and this week has been a sequence of green
  numbers that meant nothing. Switching to Claude Code was the fourth, and does not answer
  the question the corpus asks.

  **What it costs.** The agent can write, edit, and run commands. `capture()` already gives
  each run a fresh `mktemp -d` working directory and a `timeout 300`, so isolation and
  runaway are already handled — but an agent with shell access is not confined to its cwd,
  and runs on the operator's machine with the operator's privileges. That is a deliberate
  acceptance, not an oversight, and it is the reason this was a decision rather than a fix.
  Runs also get slower and messier, because prompts like `08` invite the agent to actually
  do the task.

  **Why the cost is right.** An ordering harness whose agent cannot act cannot observe
  acting-before-loading, which is the one thing it measures. Plan mode was not only blocking
  the skill; it was blocking the failure mode. Expect the numbers to get *worse* — every
  `first` in the corpus so far was awarded for a refused read — and expect that to be the
  first evidence the instrument works.
- [x] **Drop plan mode in `capture()`, and say in the comment what that admits.** Done
  2026-08-27, and the title understated it: `--approval-mode` has four values, and two of
  the three alternatives to `plan` would have reproduced the bug. `default` prompts for
  approval, and `capture()` runs with stdin closed, so a tool needing approval is denied or
  stalls to the 300s timeout — the same failure by another route, and one that would have
  read as a fresh finding. `auto_edit` leaves `run_shell_command` needing approval, so the
  `cat` fallback stays shut. Only `yolo` is genuinely non-interactive. Exposed as
  `--approval-mode`, defaulting to `yolo`, so `plan` can still reproduce the pre-2026-08-27
  corpus, and recorded in the provenance block because the mode decides what a run is *able
  to observe* rather than merely how it behaves.

  **Two doors open, one may not.** `activate_skill` and the shell fallback are approval
  decisions and open under `yolo`. `read_file` refusing `~/.agents/skills/...` is a
  *workspace boundary*, and there is no reason to expect an approval mode to move it.
  `--include-directories` would, and was deliberately not added: it widens what the agent
  may read, which is adjacent to the option the decision rejected, and adding it now would
  confound the one variable being changed. The control run is the probe.

  The control's advice now branches three ways rather than asserting `plan`: under `plan`
  the cause is known and the fix is the flag; under anything else approval is ruled out and
  the workspace is what remains; and when scoring captured transcripts it says that this
  run's mode describes nothing about what produced them — which would have been confidently
  wrong for every transcript captured before today. The
  `--approval-mode plan` argument comes out; the block comment above `capture()` currently
  ends *"and plan mode keeps it read-only"*, which becomes false. Replace it with what is
  now true: the agent can act, that is required for the measurement, and the containment is
  a fresh cwd plus a timeout rather than a read-only mode.
- [ ] **Re-run the control alone before paying for the rest.** `01-bare` at `--repeat 3`.
  If it does not come back unanimously `first`, access is still not solved and there is no
  reason to run the other eight. This is cheap and it is the step that was skipped every
  previous time.
- [ ] **Then re-run the full corpus, and re-derive every claim from it.** Three subjects at
  `--repeat 3`. Everything blocked below depends on this and nothing else.
- [x] **A refused load must not read as `not-triggered`.** Done 2026-08-27. The verdict now says the skill's
  description failed to fire when the truth is the runtime denied access, and that is
  currently the reported outcome for 19 of 27 runs. It was filed as a hypothetical when
  `OrderOf` learned to skip failed calls; it is not hypothetical. Needs either a fifth
  `Order` — the honest reading is "unmeasurable", not a verdict about the skill — or a
  refusal to score such a run at all, which is what `activation`'s `UNMEASURED` already
  does for a skill it could not read.
- [x] **Make the control's failure name the cause.** Done 2026-08-27. It correctly refused to interpret the
  other eight, and said *"fix the trigger first"* — advice for a skill defect, pointed at
  a sandbox policy. An author following it would rewrite a description that was never read.
  The control now branches on whether `unmeasurable` appears among its verdicts, and prints
  the agreement line for the control before the advice, so the claim *"the run above names
  which"* is true rather than aspirational.

**One thing the work turned up, worth keeping.** `ordering.emit` ended in a `default` case
that rendered `"first — loaded before any work"`. Adding `OrderUnmeasurable` without
touching it would have printed **a pass** for every refused run — exit code still non-zero,
so CI would have caught it, and every human reading the report would not. `OrderFirst` is
now an explicit case and the default reports an unrecognised verdict. This is the third
inverted verdict this file has produced in a week, and the first one caught before it
shipped rather than after; `TestANewVerdictDoesNotRenderAsAPass` guards the shape rather
than any one value, so the next `Order` added cannot repeat it.

## The Orphan Gate, Sited Here (2026-08-27)

Source: exegesis deferred this pending a siting question and the answer came back "not
there". exegesis is snapshot-shaped — one `manifest.Build` call, **zero** `manifest.Diff` —
and a regression-relative gate would cost it that property permanently for one check.

- [ ] **Report what a change orphaned: a skill that lost its last inbound edge.**
  `coherence`'s `OrphanEndpoints` meter (`internal/drift/drift.go:198`) is the shape:
  `NewlyOrphanedEndpoints` **and** `NewlyCoveredEndpoints` — both directions, so the gate
  is regression-relative rather than absolute — plus `BaseAvailable`, keeping *"no
  baseline"* distinct from *"zero"* the way `timeseries.Verdict.Compared` does.

  **It belongs here because `Uncoupled` already solved its sub-problems**, not merely
  because `changed` has a baseline. Two rules transfer unchanged from
  `internal/edit/coupling.go`, and re-deriving them would be the whole risk:
  - *"A location absent from the baseline is new and has nothing to be uncoupled from"* — a
    skill absent from the baseline cannot be **newly** orphaned. Without this the first
    corpus-wide run reports every pre-existing orphan as new.
  - *"The result is advisory… a gate that fires on those teaches people to bypass it"* — an
    intentional removal legitimately orphans something, so blocking is the caller's
    decision taken by promoting the severity.

  **Carry `Convention` across, and it is the most transferable part.** True only when the
  current graph contains any edge of the kind being checked — proof the corpus actually
  uses the pattern — and it skips the check when false. That is a **fifth** way to answer
  "does this check apply here", and unlike the four the family already uses (a derived gate
  in `redlines.checkTrigger`, a declared field in `skill.Lineage`, a manual `--check`
  opt-in, an advisory severity) it is **derived from the corpus** rather than declared,
  judged, or opted into. A repo that has never written a `verifies` edge is not failing the
  convention; it has not adopted it.

  Blocked on one prerequisite: **`related` must be promoted from exegesis to skillet**
  (filed there). It is the only reader of the `## Related skills` graph and is currently
  under exegesis's `internal/`. Do not fork it — a second implementation of "what is an
  edge" would disagree at the margins over fences and wrapped bullets.

  - [ ] **And blocked on a second decision the source entry did not surface: where the
    baseline edge graph comes from.** Found 2026-08-27 while planning, by reading
    `manifest.Skill` rather than trusting the siting argument.

    **`manifest.Skill` is `{slug, dir, sha256, test_prompts, test_prompts_hash}` — it
    records no edges.** So `manifest.Diff(base, cur)` reports which skills changed and
    never what the baseline's graph was, and "newly orphaned" splits into a half that is
    computable and a half that is not:

    | half | needs | available |
    | ---- | ----- | --------- |
    | orphaned **now** | the current tree | yes, with `--tree` and promoted `related` |
    | **not** orphaned before | the baseline's edge graph | **no** |

    An edge disappears when a skill is removed, or when a surviving skill's body drops a
    bullet. The first is invisible because the skill is gone; the second because its old
    body is. A hash says the body moved, not what it said.

    Three options, none free:
    - **The manifest records each skill's edges** (or a hash of them). Exact and cheap at
      diff time; widens a kernel type that is deliberately identity-only — the same
      widening exegesis declined for origin-and-verdict.
    - **The caller supplies a baseline *tree*, not just a manifest.** No kernel change, and
      the graph is read identically on both sides; costs `changed` a second tree argument,
      and a tree is heavier to keep around than a manifest.
    - **Ship the absolute half, labelled, with `BaseAvailable=false`.** Honest and useful
      today — and it is exactly the check exegesis refused to site. Moving a weaker check
      to a different tool does not answer the objection that it collapses the distinction.

    **Do not start the gate before this is answered.** Building against an unmade choice is
    the failure recorded twice in adh's harvest line, and the promotion above is
    independently justified, so there is useful work that does not wait on it.
