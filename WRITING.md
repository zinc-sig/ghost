# Writing Conventions

Rules for prose written into this repository: comments in Go files, Markdown
documents, commit messages, and pull request descriptions. The reader is a
future maintainer who knows Go and the domain but was not present when the
text was written. They need the contract and the reason behind it; the
history of how the code reached its shape is in git and the RFDs.

String literals, error messages, help text, and other product output have
their own owners; this document does not apply to them.

Enforcement: `just comment-lint` (`scripts/comment_lint.sh`, also in CI) scans
comments and Markdown and fails on the **[hard]** checks below, which have no
legitimate use in prose about code. Checks marked **[advisory]** are listed by
`scripts/comment_lint.sh --list` and never fail the build: they point at a
sentence worth rereading, and sometimes the flagged word is the right word. Do
not swap a flagged word for a synonym to satisfy the tool; either the sentence
reads well with the word, or it needs rewriting around the point it was trying
to make. Everything else is review discipline against this document.

Sources: the Google developer documentation style guide (timeless wording,
tone, formatting), the Go doc-comment spec (go.dev/doc/comment), and the
Google Go style guide (contract over implementation). Rules 3 and 4 are added
because those guides assume a human author who would not write process notes
into source.

## Rules for all writing

### 0. Keep the reason

A sentence that carries a reason may be rewritten, shortened, or moved, but
never deleted without a successor that carries the same reason. When editing,
ask "would a maintainer be able to reconstruct why this is here?" If the
answer changes from yes to no, the edit is wrong.

**Why:** rationale that lives only in a chat transcript or a review thread is
lost. It has already had to be re-derived more than once here.

### 1. Timeless: describe the present state

Write what the code does and what constraint holds. Do not write what it used
to do, when it changed, or what broke. Change history belongs in commits, pull
requests, and RFDs, all of which are searchable from the line in question.
Text is assumed to describe the current state, so "currently" and "now" add
nothing, and "new", "soon", "latest", and "existing" go stale. **[hard]** for
dates, PR and issue numbers in comments and documents; **[advisory]** for the
history words, because "no longer exists" can describe the present.

Avoid: previously, used to, no longer, was X, now, currently, new, soon,
latest, existing, old, landed, shipped, merged, reverted, dates, PR numbers,
issue numbers without a tracker link, incident names.

Commit messages, pull requests, and RFD history sections are records of a
change, so there the change is the subject and a date or version is the
reference point. Even there, the subject is what changed and why.

```go
// Bad
// GOMAXPROCS pinning was added after the Aug-2026 heartbeat starvation (#18):
// the agent now reads the cgroup quota at startup.

// Good
// GOMAXPROCS is pinned to the cgroup CPU quota so upload goroutines cannot
// starve the heartbeat sender when the container is CPU-throttled.
```

### 2. Plain declarative voice

No capital-letter emphasis, no em-dash chains, no arrows, no rhetoric
("deliberately NOT", "load-bearing", "footgun", "by construction", "the
dangerous one"). State the rule and its consequence in ordinary sentences. If
a fact needs emphasis, the emphasis is the consequence, stated plainly.
**[hard]** for em-dash and arrow characters, and for the emphasis words NOT,
ONLY, NEVER, ALWAYS, SAME, EVERY, EXACTLY in Go and Markdown. `must` is fine
when something is required; write it in lower case. **[advisory]** for the
rhetoric words.

Write in a reference register: direct, literal, and even in tone. Concretely:

- No metaphor, personification, or idiom. "The reaper babysits orphans" is
  "the reaper collects the wait status of a child whose parent exited".
  Readers include people whose first language is not English.
- No intensifiers or superlatives. "The dangerous one", "the entire reason",
  "the worst case by far", "critically" carry no information a reader can
  check; state the observation.
- No false ease. "Simply", "just", "easily", and "obviously" tell a reader who
  is struggling that the fault is theirs.
- No antithesis as a rhythm. "A guard, not a convenience" becomes "a guard:
  without it, X happens". Use "X, not Y" only when Y is a real alternative
  being rejected, and then say why Y is rejected.
- No rhetorical questions, no exclamation, no "please" in instructions, no
  "let's", no "note that" or "please note". Say what the rule is and what
  breaks without it.
- Active voice and present tense: say who does what. In instructions address
  the reader as "you", and put the condition before the instruction ("If a
  timeout is set, run supervise", not "Run supervise if a timeout is set").
- Prefer the plain verb: "use", "return", "reject", "record". A word chosen
  for effect carries nothing a maintainer can check.
- One idea per sentence, about 25 words. A parenthetical that carries a fact
  becomes its own sentence.

```go
// Bad
// NetworkMode is deliberately NOT set — the agent NEEDS egress and this is
// load-bearing for the fetch step.

// Good
// NetworkMode is left unset because the agent fetches the submission over the
// network; an isolated network would make every run fail at the fetch step.
```

### 3. No process labels or session artifacts

No review-finding numbers, plan or phase labels (P2, P4-DM, D-12, F3, T2.7,
Phase 8, GAP 1), reviewer verdicts (SHOULD-FIX), or citations of plan and
decision files. Keep the rule the label justified; drop the label. Plan and
spec files written for one piece of work are session artifacts: they are not
cited from code or documents, and once the work has merged the file is deleted
and its durable content, if any, moves to a package README or an RFD.
**[hard]** for labels, review references, and unresolved paths.

In commit messages and pull requests, refer to a review or an issue by its
tracker link, and state the resulting rule in the text, so the message still
reads when the thread is gone.

```go
// Bad
// DECISION (review finding 11a): pin GOMAXPROCS here — see the RFD 0015 Phase 8
// notes and docs/plans/2026-08-agent-starvation.md.

// Good
// GOMAXPROCS is pinned to the CPU quota so the Go runtime does not spawn one P
// per host core inside a throttled container, which starves the heartbeat.
```

### 4. Cite by name, and every citation resolves

RFD numbers and links to documents that exist in the tree (`README.md`,
`USAGE.md`, `CONFIG.md`, package READMEs) are durable and stay. A cited path
must resolve to a tracked file, or to the sibling `affairs/` repository with
that prefix. Cite a rule or principle by its descriptive name, not its number:
"the result trailer", not "F3". Numbers move when a document is edited; names
do not. Link text names the target, because a reader scanning for a link sees
only the link text.

### 5. Length is a signal

A doc comment longer than about fifteen lines is a design note in the wrong
place. Keep a two-to-four sentence contract plus a link and move the rest to a
package README or the `USAGE.md` reference. Body comments longer than about
eight lines usually mean the same thing. **[advisory]** for comment blocks of
fifteen lines or more.

In documents, use a numbered list for a sequence, a bulleted list for other
parallel items, and a table for pairs of related data. Headings are in
sentence case. Split a section that runs past a screen of prose with a
sub-heading or a table. In a table cell that has no value, write `none`, not a
dash.

### 6. Mechanics

- Code-related text (identifiers, paths, flags, values) is in code font. The
  lint ignores inline code spans and fenced blocks, so a literal that would
  otherwise trip a rule belongs in one.
- Bold marks a defined term or the subject of a rule, and is the only emphasis
  device; capital-letter emphasis fails the lint.
- Use the serial comma. Spell out "for example" and "that is" rather than the
  Latin abbreviations. Keep one spelling convention within a file.
- In a Go doc comment, never write two apostrophes in a row or two backticks:
  gofmt rewrites them as curly quotes. Say "the empty string" in prose.
- A `TODO` carries an owner or a tracker: `// TODO(#123): ...` or
  `// TODO(name): ...`. "Follow-up", "later", and "tomorrow" without a tracker
  are not actionable and are removed at the next cleanup.

## Rules by surface

### 7. Doc comments name the symbol and state the contract

The first sentence starts with the identifier and says what the symbol does or
represents. Then, only if non-obvious: return values, error values, special
cases, concurrency, cleanup obligations. The algorithm, the history, and the
design discussion belong in the body, the commit log, and the RFD. Doc
comments render in godoc and IDE hovers; write them for someone who has not
opened the file.

```go
// Bad
// resolveExecSpec: this used to build the args inline but that broke Temporal
// replay (see the flaky grading test), so we now sort the env — DO NOT change
// this back.

// Good
// resolveExecSpec returns the command and environment for one exec activity,
// with the environment sorted by key. The order is deterministic because the
// agent replays the activity schedule under Temporal.
```

### 8. Body comments explain only the non-obvious

Comment a surprising conditional, an ordering constraint, a workaround for a
library or platform, or an invariant the next line relies on. Do not describe
what the next line does. A short signal-boost is fine: `if err == nil { // no
error`.

### 9. Test comments say what is asserted and what a failure means

Name the behaviour under test and the wrong behaviour the test would catch. Do
not narrate the bug it was written for or the review that requested it.

```go
// Bad
// TestSupervise_OutputCap locks the fix for the truncation bug found in review
// round 2.

// Good
// TestSupervise_OutputCap asserts that the child is killed once combined
// stdout and stderr reach --max-output-bytes and that the trailer reports
// truncated=true; a cap applied per stream would let the total exceed the
// limit, so the test writes to both.
```

### 10. Documents describe the code as it is

A package README says what the package is for, how to use it, and which
invariants and pitfalls a maintainer has to know. A convention document states
the rules and the reason for each. Neither records its own history: no "Phase
3" or "since the refactor" narration, no review history, no record of what was
considered and rejected. If the reason for a shape is worth keeping, state it
as the present rationale. Design records that are genuinely historical belong
in an RFD.

### 11. Commit messages and pull requests

The subject line is imperative and present tense, under about 70 characters,
prefixed by the area (`fix(agent):`, `docs(usage):`). The body says what
changed and why, in the same register as the rest of this document, and how it
was verified. It names the constraint the change satisfies; a list of attempts
tells a reader what did not work without saying what the code guarantees. A
pull request description gives a reviewer the problem, the change, what was
verified, and what was deliberately left out, as four short sections in that
order; a transcript of the session leaves the reviewer to reconstruct those
four points.

### 12. RFDs

The proposal and decision sections follow rules 0 to 6. The background,
alternatives, and history sections are records: they keep their dates, version
numbers, and the alternatives that were rejected, because that is their
purpose. When implementation diverges from a recorded decision, the RFD says
so in the text as a present fact for ratification. Rewriting the decision would
erase the record of what was agreed.

## Quick check before committing

- Would this still be true and useful in a year, read by someone who never saw
  the pull request?
- Does it say what and why, in plain sentences, in under ten lines?
- Does every path or name it cites resolve to something in the tree?
