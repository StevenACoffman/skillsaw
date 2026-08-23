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
- [ ] ~~**A report of which cases still lack `checks`, scoped to a directory.**~~ Follows from
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
- [ ] **MEASURED 2026-08-08: do not build this as written — it would write empty arrays.**
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
- [ ] A timeseries **regression gate** for rubric quality across skill versions — a CI check
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

- [ ] **Make the rubric data, not a Go table.** `rubric.Dimensions()`
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
- [ ] **Their thresholds file states our charter better than we state it.** Its header
  reads: *"Reference values from empirical data. NOT enforced thresholds — AgentLint
  measures and compares, users decide."* That is canonizer's findings-not-scores rule,
  applied to thresholds, in the file the thresholds live in. Worth copying verbatim as the
  first key of whatever we externalize, because the file is where the temptation to harden
  a reference into a gate will actually arise.
- [ ] **Externalizing weights is how you find the bug.** `weights.json` carries a note
  recording that its dimension weights deliberately sum to **1.10**, not 1.0, because
  `D1-D3` and `SS1-SS4` "were previously emitted by deep-/session-analyzer but silently
  dropped because no dimension owned them" — a coverage defect that only became visible
  once the weights were data laid out beside the check IDs. Ours sum to exactly 100 and the
  property is checked by a test, which is stronger; but nothing checks that every
  *deterministic penalty* skillsaw computes is owned by some dimension. That is the same
  class of defect and it is worth a test either way.
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
- [ ] **Lower priority — a guard the optimizer cannot rewrite.** `gate` runs keep-or-revert
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
- [ ] **A verdict taxonomy richer than accept/reject, for the axes that actually have
  variance.** `agent-blue/cc-thinking-skills/evals/run-replication.js` exposes a **pure,
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
- [ ] **Evidence-as-authority, and an explicit list of claims we are allowed to make.**
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

- [ ] **The rubric checks are evals, and nothing proves they would catch a defect they
  lack a fixture for.** `agent-fuschia/evalmut` is mutation testing for eval suites, and its
  opening question is the one to answer here: *"Your eval suite passes. Does it actually
  check anything?"* It takes a case the grader passed, injects a **known** defect, and
  reruns; a grader that still passes has a **hole**. Two properties make it more than a
  fault-injection harness:
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

- [ ] **A `PASS → FAIL` transition is its own status, not another failure.**
  `oh-my-agent`'s judge protocol emits `REGRESSED` exactly once on the transition and
  explicitly does **not** increment the failure counter for it, because a regression
  routes differently: it ships the diff between the last passing iteration and now, so
  the next attempt is a targeted diagnosis rather than a reimplementation. `gate`
  already separates the measured axis from the disposition axis (Adopt-5 above) and has
  `improved`/`tie`/`regressed` on `Status` — what is missing is that a *dimension* that
  previously scored clean and now does not is a different event from one that has never
  been clean, and `diagnose` cannot currently say which it is looking at.
- [ ] **Re-score every dimension each iteration, including the ones that passed.**
  *"Because fixing C2 is how C1 silently regresses."* Needs checking first: if the
  ratchet already re-scores the whole rubric this is a no-op and the entry should say
  so. If it re-scores only what it edited, it cannot see a regression its own edit
  caused, which is the failure mode the hill-climbing loop is most likely to produce.
- [ ] **A failure counter must count *consecutive* failures and reset on success.**
  Otherwise a dimension that is merely noisy accumulates to a terminal verdict on
  elapsed time. `oh-my-agent` states the reasoning worth keeping next to the code:
  recurring flakiness should surface as repeated regressions — a signal about the
  *check* — rather than as a permanent verdict about the thing checked. Three surveyed
  projects converge on the same shape (consecutive, resets on progress, small *N*).
- [ ] **Estimate the evaluator's noise floor before calling any delta an improvement.**
  `ruflo`'s optimisation log accepts `+0.0028` and `+0.0046` on single runs and rejects
  `−0.0002` and `−0.0004` as *"essentially flat"* — accept and reject thresholds an
  order of magnitude apart, with no variance estimate and no re-measurement of the
  champion. skillsaw's score is deterministic, so this does **not** apply to the rubric
  itself, and it applies squarely to anything sampled: `activation`, `judge`, and any
  future measured-lift number. `internal/stats` already has Wilson; the missing piece
  is refusing to accept a delta that does not clear the interval.
- [ ] **A change that loosens a gate may not score as improving it.** `ruflo`'s loop
  raised a promotion rate by relaxing the promotion predicate from `AND` to `OR`,
  logged *"BIG WIN"*, and committed. Recording the loosening is not enough — it *was*
  recorded, in prose, as a win. Anything in the rubric or in `gate` that a candidate
  edit can itself relax has to be excluded from the improvement it produces.
- [ ] **Audit `rubric.Dimensions()` for count-shaped dimensions.** `ruflo`'s loop moved a
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

- [ ] **The cheapest half of that is a gate that fires before authoring: the no-guidance
  control.** *"Always include a no-guidance control. If the control doesn't exhibit the
  failure, there is nothing to fix — stop, don't author the guidance."* A skill written
  against a failure the model does not exhibit is pure context cost, and no rubric
  dimension can detect one, because the document is about a real-sounding problem and
  is well-formed. This is a field in the evidence, not a check in the scorer: a skill
  that carries no baseline observation is *unmeasured*, which is a third state beside
  pass and fail and is the same discipline as `unchecked` everywhere else in the family.

- [ ] **`diagnose` cannot separate three repair classes that want opposite edits.**
  `superpowers`' meta-test asks the agent that failed *with* the skill loaded how the
  skill should have been written, and classifies the answer: *"the skill WAS clear, I
  chose to ignore it"* is not a documentation problem and needs a stronger foundational
  principle; *"the skill should have said X"* is a content problem and X goes in
  verbatim; *"I didn't see section Y"* is an **organisation** problem — the content was
  right and unreachable. `diagnose` answers which dimension is weakest and is silent on
  whether the content is wrong or merely misplaced, and those are different edits. The
  classification needs an observation skillsaw does not have; recorded so the shape is
  known when it does.

- [ ] **Brevity can trade against efficacy, and a dimension that rewards it should say
  so.** `superpowers` ran a branch-wide compression campaign where *"each cut was
  micro-tested with subagent probes, and the one cut that measurably degraded behavior
  was reworked rather than shipped."* One deletion of apparently-redundant prose
  changed behaviour. Any dimension here that rewards concision is optimising something
  that can trade against the thing the skill is for — the same class of defect as a
  count-shaped dimension, and subtler because the direction feels virtuous.

- [ ] **Validity scope on the SkillLens dimensions.** `superpowers`' *Match the Form to the
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

### `activation` Has a Harness Shape Available

- [ ] **Adversarial phrasing corpus + an ordering assertion.**
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

- [ ] **Variance across repetitions as a bindingness metric.** *"When guidance lands, reps
  converge on the same shape. Five different interpretations across five reps means the
  wording isn't binding."* Dispersion, not mean — a signal about whether the wording
  constrains at all, available from repeated activation runs at no extra cost beyond
  the reps. `skillet/stats` holds the machinery and this is its prospective second
  consumer; recorded in that repo as held for the ask.

### One Line to Draw, and One Thing Not to Take

- [ ] **A vendor's prose advice is not the machine-readable contract, and the rubric
  should not treat them alike.** `superpowers` vendors Anthropic's skill-authoring
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

- [ ] **Gate it in `preflight`, once `skillet/manifest` carries the missing hash.**
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
