# GitLabAnak

A small GitLab v4 API stand-in backed by real git repositories, in Go. Target: GitLab EE 19.2.9. Start with `docs/PLAN.md`; slices are in `docs/SLICES.md`, decisions in `docs/adr/`, the decision register in `docs/QUESTIONS.md`. The board is Pandan board `GIT` (use the `pandan` skill).

## Build status

0.3.0 is released: lint, files, branches, commits, tree, raw, admin API, persistence, and git clone/fetch/push over HTTP. Pipelines and running jobs (0.4.0 onward) are not built; the runner stub only registers and polls (docs/spike-runner.md). Trust the code over the docs if they disagree, then fix the docs. Error texts in the plan are **unverified** against a real server (ADR-0003); `make conformance` prints how many.

## Commands

Run `make` for the list. The ones you need: `make check` (fmt, vet, lint, tidy), `make test` (`-race -count=1`), `make ci` (everything CI runs), `make hooks` (install the pre-push hook; `git push --no-verify` is the escape hatch).

## Conventions

- One branch per slice off fresh `main`, PR-only. Run `make ci` before pushing.
- Dependencies: go-git and yaml.v3 only (ADR-0001). A third needs a new ADR. Only `internal/gitstore` may import go-git (ADR-0002).
- Never read or paste GitLab source code (CE or EE) into this repo (ADR-0004). Behaviour comes from public docs, `python-gitlab`, and observed responses.
- Lean code: read before adding, delete what the change supersedes, comment only a non-obvious why, and put the net line change (`git diff --shortstat origin/main...HEAD`) in the PR description.
- Inject the clock and randomness. Commit time is derived from the parent (ADR-0002), never `time.Now()`.
- Every bug becomes a test first. Prove a new guard by watching it fail.
- Never log or commit the token.
