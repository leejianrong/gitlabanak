# ADR-0003: A Python conformance suite is the source of truth, run against GitLabAnak and against GitLab EE 19.2.9

- Status: Accepted
- Date: 2026-11-27
- Deciders: project owner, Claude

## Context

The target is GitLab Enterprise Edition v19.2.9-ee, which is the version the maintainers target. The error messages in the first requirements draft were written from documentation and memory, and knowledge of GitLab 19.x behaviour is incomplete. Any hand-written expectation can be wrong, and a mock that is wrong teaches tests the wrong thing.

## Decision

- A pytest suite in `conformance/` uses `python-gitlab` (pinned to 8.2.0, the client most consumers use) for every row of the response tables in PLAN.md.
- `GITLAB_URL`, `GITLAB_TOKEN` and `GITLAB_PROJECT_ID` choose the target. The same tests run against GitLabAnak (every change, in CI) and against a throwaway project on a real GitLab 19.2.9-ee (by hand or on a schedule).
- Expected values live in `conformance/expected/*.json`, recorded from the real server. When the real server is unavailable, the file stays at the spec's value and is marked `unverified: true`. CI prints the count of unverified rows, so a guess is never invisible.
- A difference found against the real server is a bug in GitLabAnak, except the differences listed in `conformance/KNOWN_DIFFERENCES.md` (for example the initial empty-tree commit, ADR-0002).
- The README states the GitLab version the suite was last verified against.

## Alternatives considered

| Option | Why not |
|--------|---------|
| Go tests only | They would encode our own assumptions, and the reference client library, `python-gitlab`, is Python. |
| Record real traffic once and replay | Misses the cases we have not thought to record. The parametrised suite can also be re-run against the real server after a GitLab upgrade. |

## Consequences

- Buys: a way to detect drift when the target GitLab upgrades, and confidence that `python-gitlab` calls behave identically.
- Costs: someone needs a throwaway GitLab 19.2.9-ee project and token to turn `unverified` rows into verified ones. Until then the error texts are best guesses.
- Python is needed in CI only.
