# What a skillsaw Number Is, and What It Is Not

If this document disagrees with the code, the code wins. It exists so that a number
produced here does not acquire, on its way into a README, a meaning it never had.

## What each output actually claims

| Output                     | Claims                                                                   | Does not claim                                                                     |
| -------------------------- | ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------- |
| `eval` deterministic score | A lower bound on quality loss from objectively detectable defects        | That the skill works                                                               |
| `eval` full score          | The above, plus a reader's judgement of the dimensions no tool can score | That the reader was right, or that another reader would agree                      |
| `judge` base               | The mean of per-case check results, over the cases that carry checks     | That the cases assert anything about behaviour — see the measured blind spot below |
| `activation` net utility   | Vocabulary overlap between a description and its prompts                 | That the runtime would route this way                                              |
| `preflight` verdict        | The structure is intact                                                  | That the edit was an improvement                                                   |

`activation`'s FPR is the row worth reading twice. It claims nothing when no decoy ever
fired: a false-positive rate of zero is either perfect precision or decoys too easy to
separate anything, and a confusion matrix cannot tell those apart. Measured over sixty real
skills, **no decoy fired once** and forty-two carried none at all — so every FPR in that run
was a figure computed from nothing. The command now says so per skill.

A skill it could not read at all is reported as **unmeasured** rather than scored or
skipped, which is the same distinction in a different place: not measured is not a result.

**Nothing here measures lift.** The rubric scores form: what the artifact looks like, not
what changes when an agent loads it. Those are different claims and the first is not weak
evidence for the second — a skill written against a failure the model does not exhibit
scores perfectly and is pure context cost. `scores.Entry.Baseline` is where an observation
that a failure is real gets recorded, and an entry without one is **unmeasured**, which is
a third state beside pass and fail.

## Claims that are not authorised

No document in this repository may describe a skill, the rubric, or this tool as
**proven**, **eval-informed**, **auto-invoke**-worthy, or an **empirically validated**
skill on the strength of a number produced here. None of those follow from what is
measured, and `TestNoUnauthorizedClaims` enforces it rather than trusting this paragraph.

Saying what *was* measured is always allowed: "scores 84 on the deterministic rubric" is a
fact; "proven" is a claim about the world.

**"validated" is not matched as a bare word, on purpose.** It has an ordinary and correct
meaning here — a validated constructor, a status validated on write — and matching it
flagged three such uses on the check's first run. A check that cries wolf is a check
somebody deletes, so the claim sense is matched by phrase. Do not "fix" the list by adding
the bare word back.

## Measured blind spots

The rubric's own checks are an eval suite, and `TestKnownDefectsAreNoticed` injects known
defects to find what they miss. Two blind spots are recorded there rather than hidden:

- **Assertions that assert nothing.** A case whose `expected` holds activation prose and
  whose check merely repeats a word from it is fully scorable and says nothing about
  behaviour. Dim 8 counts scorability, not meaning.
- **Boundary items that are filler.** Dim 9 counts units; three rows of filler score as
  three real counter-examples.

Both need a reader. A deterministic proxy for either would be a new dimension an optimiser
could feed, which is the defect the first one already is.
