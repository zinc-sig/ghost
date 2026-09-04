# Agent and contributor guide

Ghost is a command orchestration CLI built in Go with Cobra. It runs an
external command and reports structured execution metadata. `README.md` is the
quick start, `USAGE.md` is the command reference, and `CONFIG.md` documents
flags and environment variables.

## Build and test

The `justfile` holds the common tasks; run `just` to list them. Build with
`just build`, run the unit tests with `just test`, and run the full checks with
`just ci`. Run `just fmt` before every commit.

## Writing conventions

`WRITING.md` at the repo root is the authoritative convention document for all
prose in the tree: source comments, Markdown documents, commit messages, and
pull request descriptions. Doc comments state the contract; wording is timeless
and plain, with no capital-letter emphasis, em-dashes, arrows, metaphor, or
intensifiers; and there are no review-finding numbers, plan labels, PR numbers,
dates, or citations of files outside the tree. Keep the reason a comment
carries, and drop the process narrative around it. Run `just comment-lint`
after editing comments or documents; it is also a CI step.
