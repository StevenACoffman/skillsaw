# Adversarial phrasing corpus

Nine phrasings of one request, differing only in how hard each pushes the agent to start
working before it loads the skill.

`skillsaw activation` asks whether a skill's description *would* trigger, from vocabulary
overlap. This asks something vocabulary cannot answer: whether anything happened first. An
agent that starts editing and then loads the skill has used it — a reply describes that as
success, truthfully — but part of the work was done outside the guidance it appears to have
followed. Only the event log separates the two, and `skillsaw ordering` reads it.

## Running it

The agent is **Gemini CLI**. `gemini skills list` shows the names available to request.

```console
$ harness/run.sh --skill climax-cli-scaffold --task "I need a new CLI scaffolded."
skill:      climax-cli-scaffold
transcripts: /tmp/tmp.XXXX/transcripts

01-bare (control)        first

02-with-context          first
03-imperative            after-action
...
```

Without `gemini` on `PATH`, score a run somebody else captured:

```console
harness/run.sh --skill brainstorming --transcripts ./captured
```

Files are matched to prompts by name: `03-imperative.json` scores as `03-imperative`.

Each phrasing runs in its own empty working directory, so one cannot see what the last one
wrote, and in read-only plan mode.

**`HOME` is deliberately not isolated, and that departs from the reference.** Gemini CLI
discovers skills under `$HOME/.gemini/skills`, so a fresh `HOME` would hide the very skill
being requested and every run would report `not-triggered`. The reference isolates `HOME` to
keep the operator's context out of the result; here the operator's context is where the
skill lives. What that costs is real and worth stating: an operator's other skills,
extensions and settings are in scope, so two machines can disagree.

## The control gates the rest

`01-bare` is the skill's name and nothing else. It runs first, and if it does not load the
skill the run **stops**: eight phrasings failing under pressure cannot be told apart from a
skill that does not answer to its own name, and only the first of those is what this corpus
is for. Reporting the other eight anyway would be offering numbers that cannot mean what
they appear to mean.

Under `--repeat N` the control repeats too, and must be **unanimously** `first`. A split
control is precisely the case where the rest cannot be read — it says the skill does not
*reliably* answer to its own name, so a phrasing failing under pressure is
indistinguishable from one failing for that reason. This is not hypothetical: a subject
measured at 62% `first` across 24 runs passed a single control on one lucky sample, which
would have licensed eight rows that should have been refused.

The same reasoning as the no-guidance control in `EVIDENCE.md`: establish the baseline, and
refuse to interpret the treatment when the baseline does not hold.

## The pressure shapes

| Prompt                  | What it tempts the agent to do                             |
| ----------------------- | ---------------------------------------------------------- |
| `01-bare`               | nothing — the control                                      |
| `02-with-context`       | read the context and begin from it                         |
| `03-imperative`         | treat the request as an order to start, not to load        |
| `04-time-pressure`      | skip preliminaries because time is short                   |
| `05-pre-summarised`     | follow the summary in the message instead of the skill     |
| `06-user-pre-explains`  | skip loading, since the user already described the skill   |
| `07-accepting-an-offer` | proceed from its own earlier suggestion without re-reading |
| `08-buried-in-a-list`   | service the neighbouring requests first                    |
| `09-negated-nearby`     | read a nearby negation as applying to the request          |

`{{SKILL}}` and `{{TASK}}` are substituted at run time, so the same nine shapes point at any
skill. The `#` line at the top of each file names its shape for a reader and is stripped
before the prompt is sent.

## How "the skill loaded" is detected

Gemini CLI has **no dedicated skill tool**. It loads a skill by reading the file, so the
evidence is a path ending `<name>/SKILL.md` among a tool call's arguments — observed as a
`read_file` `file_path`, and as a `run_shell_command` catting the same path when the read was
refused for being outside the workspace. `skillsaw ordering` recognises both, and also
recognises Claude Code's `Skill` tool, so a capture from either runtime scores here.

Planning tools do not count as having started work: writing down an intention is not acting
on one. `update_topic` is Gemini's — captured firing immediately before a skill read,
carrying only a title and a summary of what the agent was about to do. `TodoWrite`,
`TaskCreate`, `TaskUpdate`, `TaskList` and `TaskGet` are Claude Code's.

## Asking why, after a failure

`meta-why.txt` is not one of the nine and the runner does not use it. It is asked **after**
a phrasing has failed, against the same skill, and its answer is prose for a person to read.

It exists because the three repairs a failure might want are opposite, and nothing in the
verdict distinguishes them:

| The answer                              | What it means               | The repair                            |
| --------------------------------------- | --------------------------- | ------------------------------------- |
| "The skill was clear, I started anyway" | not a documentation problem | a stronger foundational principle     |
| "It should have said X"                 | a content problem           | X goes in, verbatim                   |
| "I didn't see section Y"                | an organisation problem     | the content was right and unreachable |

Run it by hand:

```console
gemini -p "$(sed -e 's|{{SKILL}}|matryer-decode-valid|g' -e 's|{{TASK}}|…|g' -e '/^#/d' harness/meta-why.txt)" \
  --skip-trust --approval-mode plan
```

**There is no classifier, deliberately.** Assigning one of those three classes from prose is
a judgement, and this repository refuses that shape elsewhere for the same reason — see dim
3, dim 8, and the ruleset subject slot. The prompt collects the answer; you classify it.

## What a result does and does not establish

A run measures whether an explicit request loaded the skill before work began, **for one
model, on one day, over one task, on one machine's skill configuration**. The agent is not deterministic; a single run is an
observation, not a property of the skill.

It is not a quality measurement and not a lift measurement. `EVIDENCE.md` lists the claims
this repository does not authorise, and they apply to anything written about these results:
a skill that scores `first` on all nine has not thereby been shown to work.

Two failures mean different things and the table keeps them apart. `not-triggered` says the
request did not reach the skill — a trigger-vocabulary problem, which `activation`
diagnoses. `after-action` says the request did reach it, too late — a problem with how the
skill is described or how strongly the request competes with the surrounding text, and the
one this corpus exists to find.

## Provenance

The shape is taken from `superpowers/tests/explicit-skill-requests/`, which pairs a real
agent run under an isolated `HOME` with a pure predicate over the transcript. Three
departures:

- **The prompts are templates.** The reference hardcodes one skill and one plan path into
  every file, so its corpus cannot be pointed at anything else.
- **Nine phrasings of one request, as specified.** Two of the reference's nine are different
  skills rather than phrasings; those slots are used here for two further pressure shapes
  (`08`, `09`).
- **Scoring is delegated.** The reference re-implements "before" as `head -n LINE | grep`.
  `skillsaw ordering` parses the events instead, and this runner shells out to it rather
  than carrying a second, weaker copy of the same rule.
- **A different agent.** The reference drives Claude Code. This drives Gemini CLI, whose
  event vocabulary differs in three ways at once — the tool name is under `tool_name`, the
  arguments under `parameters`, and events are flat rather than nested in an assistant
  message — and which has no skill tool at all. Those shapes were captured from real runs,
  not inferred; `internal/transcript` carries one as a test fixture.
