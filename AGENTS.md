# AGENTS.md

Operating rules for AI agents working in this repository.

---

## Non-negotiables

1. Everything runs in Docker. Never install anything on the host.
2. Never claim code works without executing it.
3. Verify APIs against installed source, not memory.
4. No emojis anywhere - code, comments, logs, commit messages, output, docs. ASCII compatibility is important.
7. Ask when the decision is the user's and has not been made. Do not ask for permission to follow an instruction you already have.
8. Tests must be actually testing. The results must bring value and understanding.
9. Logging must be exhaustive. We must be able to troubleshoot with great efficiency out of the logs. Some of the logs may correctly belong to DEBUG.

---

## Don't make things up

- **Source each substantive claim.** For every claim, name the source line. Zero
  sources means cut it or label it explicitly as your own reasoning. Unsourced
  assertions sitting next to sourced ones inherit their credibility.
- **Check the document against its own rules.** Do not introduce the thing the
  document elsewhere recommends against.
- **Delete credibility words.** *Honest, genuinely, truly, carefully, thorough.*
  They ask for credit for accuracy instead of being accurate, and labelling one
  statement implies the unlabelled ones are a different category. If the
  sentence loses nothing, it was decoration. If a claim is weak, say what is
  weak about it.
- **Answers must be based on facts, not assumptions.** There is only harm in
  quick unverified answers that could have been verified.

---

## The scope of an instruction

Do what was asked. Stop. Report.

- **An instruction covers what it says**, not the next step, not the obvious
  follow-on, not the thing you proposed last turn.
- **Proposing a next step does not authorise it.** Ending a turn with "X next"
  leaves X still needing to be asked for.
- **Silence is not agreement.** A reply that does not mention your proposal has
  not approved it. A reply about something else has not approved it either.
- **One instruction authorises one unit of work.** Where a plan splits work into
  chunks, sections or phases, an instruction to apply one applies one.
- **Approval does not carry forward.** Agreement to this change is not agreement
  to the next change of the same kind.
- **A correction is not an extension.** Being told to fix something inside the
  work in hand does not widen what the work in hand covers.

When the asked-for work is done and you believe more should follow, name it and
stop. Do not begin it.

This governs **scope**, not confidence. Stopping short because you are unsure is a different failure.

---

### Don't buy attention with a promise

Anything that only resolves after reading the thing it is attached to is
decoration. A promise of accuracy is not accuracy; a promise of a payoff is
not a payoff. State the fact in the position where the reader needs it.

- **Headings label, they don't tease.** A heading is an index entry. Two
  tests, both applied before the body exists: can a reader who has not read
  the section resolve it, and does it survive an edit to the section? Cut any
  clause that fails either - *...and why the other two lost*, *...and what it
  means*, *The real reason for X*, *What we learned from X*. If that clause
  carried information, it is the first sentence of the body. Forward
  references (*the other two*, *the surprising part*) point at text the
  scanner has not reached. Counts and standings (*the other two*, *the third
  option*) go false the moment an item is added or reinstated.

  Headings may carry conclusions - that is not the failure. *Meta.constraints:
  not viable* is better than *Meta.constraints*, because it costs the scanner
  nothing and saves them a section. The failure is withholding one.

  Write the words a reader would grep for. Nobody searches a repo for `lost`.

      bad:    Candidates, and why the other two lost
      fixed:  Policy declaration: candidates

      bad:    Is Meta.constraints viable?
      fixed:  Meta.constraints: not viable

      bad:    What we learned from the RLS rollout
      fixed:  RLS rollout: three defects

      bad:    A surprising result
      fixed:  Drift test catches what autodetection would miss

      bad:    Migrations: the deeper problem
      fixed:  Migrations: policy changes are invisible to the autodetector

- **No withheld payoff mid-prose either.** The same move reappears as a bolded
  lead-in (*The catch:*, *The key insight:*), a rhetorical question standing in
  for a claim (*So why not just subclass the autodetector?*), and a closing
  line that gestures at significance instead of stating it (*This has
  implications for how we handle migrations.*). In each case the sentence
  announces that something is coming rather than being it. Delete the
  announcement and promote what followed it.

      bad:    **The catch:** create_model discards falsy constraint_sql.
      fixed:  create_model discards any constraint whose constraint_sql is
              falsy, so a policy declared in Meta.constraints would not exist
              on a newly created table.

      bad:    This has implications for our migration strategy.
      fixed:  A changed policy is therefore not autodetected; the drift test
              is what catches it.

  Same rule for commit subjects and PR titles: name the change, not the
  reader's reaction to it.

---

## Error handling

- Fail loudly and early. Do not add defensive `try/except` around code that should not fail - it hides bugs.
- Never use a bare `except:` or `except Exception:` without re-raising or logging with full context.
- Catch specific exceptions. If you are catching broadly at a boundary, say why in a comment.
- Do not convert an integrity error into a silent no-op. A constraint firing means the caller did something wrong and needs to know.
- Validation errors are 400 with a machine-readable code. Domain rule violations are 409. Do not return 500 for an expected rejection.

---

## Output and style

- **No emojis.** Not in code, comments, docstrings, log messages, commit
  messages, documentation, test names, or your responses.
- No decorative separators, banner comments, or ASCII art.
- No comments restating the code. Comment *why*, never *what*.
- Follow the formatter and linter configured in the repo. Do not reformat files
  you did not otherwise change.
- Type hints on service functions and public interfaces.
- Docstrings on service functions: what it does, what it guarantees, what it
  raises. Not on obvious methods.
- When reporting what you did, be concrete: what changed, what you ran, what the
  result was. Do not pad with summaries of the obvious.

---

## Git

- Small, focused commits. One logical change each.
- Subject line format is in `README.md` under "Github naming convention", which
  owns it: `gh-<NNN> [<ACTION>] <COMMENT>`. No emojis.
- Never commit secrets, `.env` files, or database dumps.
- Never force-push a shared branch.
- Never commit generated migrations you have not read.
- Do not commit unless asked to.
- Never commit all unstaged files, always commit exactly what needs to be committed.
