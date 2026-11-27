# ADR-0005: Run CI by letting the real gitlab-runner connect, after lint and pipeline modelling

- Status: Accepted (direction only. The runner protocol work is spiked in V5 before it is committed to.)
- Date: 2026-11-27
- Deciders: project owner, Claude

## Context

Platform teams and DevSecOps engineers want to iterate on `.gitlab-ci.yml` without pushing to a real GitLab: check syntax, check that jobs start, see stage order. The same image already serves application integration tests, so it has to stay small and must not require a Docker socket by default. GitLab Runner is an MIT-licensed binary that already knows how to run shell and Docker executors, services, caches and artifacts.

## Decision

Staged, each stage useful alone:

1. **Lint** (0.3.0). Parse `.gitlab-ci.yml` and report errors: `POST /api/v4/ci/lint`, `POST /api/v4/projects/:id/ci/lint`, and `gitlabanak lint <file>` in the same binary for use with no server. Supported: `stages`, `default`, `variables`, `include:local`, `extends`, `needs`, `rules` (only simple `if` on predefined variables), `image`, `services`, `script`, `artifacts` as parsed data.
2. **Pipelines as data** (0.4.0). A push creates a pipeline with stages and jobs in the pipelines API, with statuses that move only when something reports. No execution.
3. **Runner** (0.5.0). Implement the runner-facing API (`/api/v4/runners`, `/api/v4/jobs/request`, job trace and status updates) so an unmodified `gitlab-runner` registers against GitLabAnak and executes the jobs. GitLabAnak never executes job scripts itself.

The runner stays outside the image. It is started by the user (or a compose file), pointing at GitLabAnak.

## Alternatives considered

| Option | Why not |
|--------|---------|
| Own executor inside GitLabAnak (Docker socket) | Bigger image, needs privileged access, and reimplements what gitlab-runner does well. |
| Wrap a tool like gitlab-ci-local | Does not run against a server, so it gives no GitLab API behaviour (pipelines, jobs, MR-less triggers) for other tools to talk to. |
| Skip CI entirely | Drops a stated goal of the project. |

## Consequences

- Buys: high fidelity job execution for little code, and the exact runner users run in production.
- Costs: the runner protocol is large and its details in 19.2.x are unverified. V5 is a time-boxed spike for exactly that reason: if registration plus an empty `jobs/request` poll cannot be made to work in a day, the stage is re-planned.
- Forecloses: running job scripts without a runner binary.
- Out of scope even here: merge-request pipelines, child pipelines, `trigger`, `environments`, `cache` server semantics.
