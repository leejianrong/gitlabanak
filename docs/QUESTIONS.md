# Questions

Statuses: `DECIDED` (user answered) · `ASSUMED` (default taken, correct it if wrong) · `FORK` (waiting on the user) · `DEFERRED` (not needed this milestone).

Source idea: the first requirements draft, now folded into `docs/PLAN.md`.

## Open forks

None. Round 1 is closed. The user's answers opened no fork where the recommendation was low-confidence and expensive, so they became decisions below.

## Register

| ID | Question | Status | Answer or default | Landed |
|----|----------|--------|-------------------|--------|
| Q1 | Primary user | DECIDED | Integration tests and CI jobs first. Platform and DevSecOps engineers iterating on CI from 0.2.0. Agents debug with the admin API. | PLAN §Users |
| Q2 | Git model | DECIDED | Real Git (go-git). The first draft's in-memory maps were rejected. Commits are deterministic. | ADR-0002 |
| Q3 | Language | DECIDED | Go | ADR-0001 |
| Q4 | Rewrite vs recover 0.2.0 | DECIDED | Source not obtainable. Brand-new project, versions from 0.1.0. | ADR-0001, PLAN §Open risks |
| Q5 | Target GitLab version | DECIDED | GitLab EE v19.2.9-ee. Error texts remain unverified until a real server is checked. | ADR-0003 |
| Q6 | Other consumers | DECIDED | Yes. Other applications' integration tests that need an external GitLab, plus CI-pipeline iteration by platform teams. | PLAN §Users, ADR-0005 |
| Q7 | Branch semantics | ASSUMED | Branch is a git ref. A commit touches only its branch. | PLAN §Shape S4 |
| Q8 | Identity | ASSUMED | Project by integer id (and path from 0.2.0), branch by name, file by decoded path, commit by real git sha. | PLAN §Shape S2 |
| Q9 | Persistence | ASSUMED | Memory by default. Filesystem git storage under `GITLAB_DATA_DIR` if set. | ADR-0002 |
| Q10 | Concurrency | ASSUMED | One mutex per project. Last write wins. Create and update existence checks are the only conflicts. | PLAN §Shape S2 |
| Q11 | Path decoding | ASSUMED | Encoded or plain slashes in URL. Body `file_path` is decoded. | PLAN §Shape S3 |
| Q12 | Unknown routes | ASSUMED | 501 and recorded. | PLAN §Shape S1 |
| Q13 | Panic safety | ASSUMED | Recover middleware, tested with a forced panic. | PLAN §Shape S1 |
| Q14 | Auth | ASSUMED | `PRIVATE-TOKEN` or Bearer equals `GITLAB_PRIVATE_TOKEN`. `/health` open. `/_admin/*` same token. Basic auth with the token as password for git. | PLAN §Shape S1, S8 |
| Q15 | Request log | ASSUMED | Ring buffer of 10 000, token redacted. | PLAN §Shape S5 |
| Q16 | Dependencies | DECIDED | go-git, and yaml.v3 from 0.3.0. Nothing else without a new ADR. | ADR-0001 |
| Q17 | Conformance suite | ASSUMED | Python and `python-gitlab` 8.2.0, runs against GitLabAnak and a real server. | ADR-0003 |
| Q18 | Image path and tag | ASSUMED | Same ghcr path. Tags 0.1.0 onwards. | PLAN §Implementation |
| Q19 | CI for this repo | ASSUMED | GitHub Actions with Go gates, conformance against the container, push on tag. | SLICES V3 |
| Q20 | Merge requests, users, groups, UI | DEFERRED | Out until a test or user needs them. | PLAN §Scope |
| Q21 | API versioning | ASSUMED | Only `/api/v4`. | PLAN §Scope |
| Q22 | Secrets | ASSUMED | Token from env, never logged, redacted in the request log. | PLAN §Implementation |
| Q23 | Success measures | ASSUMED | Start under 2 s, image under 50 MB (target 15 MB), every table row covered by a conformance test. | PLAN §Requirements R4 |
| Q24 | Body size | ASSUMED | 10 MB per request, larger gets 413. | PLAN §Shape S1 |
| Q25 | `ref` forms | ASSUMED | Branch, tag or commit sha. | PLAN §Shape S4 |
| Q26 | Initial state of a new project | ASSUMED | Root commit with the empty tree, so `main` exists. Differs from real GitLab. | ADR-0002 |
| Q27 | Clone and push | DECIDED | Yes, 0.2.0, from the same repositories. | ADR-0002, SLICES V4 |
| Q28 | Lint surface | ASSUMED | REST plus `gitlabanak lint` in the same binary. | ADR-0005 |
| Q29 | CI execution | DECIDED | Platform teams must be able to try pipelines without a real GitLab. Delivered by lint, then pipelines, then a real runner. | ADR-0005 |
| Q30 | Licence and provenance | ASSUMED | Apache-2.0, clean-room from public docs and observed behaviour. | ADR-0004 |
| Q31 | Forge ambition | DECIDED | Long-term goal (a free self-hostable GitLab-compatible alternative). A north star only, with no requirements in this plan. | PLAN §Solution |
| Q32 | Real GitLab access for the suite | ASSUMED | Not available yet. Rows stay `unverified` and are counted until someone supplies a throwaway project on 19.2.9-ee. | ADR-0003 |

## Coverage

| Category | Covered by |
|----------|-----------|
| Primary user and actors | Q1, Q6 |
| Scope boundary | Q4, Q20, Q21, Q31 |
| Data model and identity | Q2, Q7, Q8, Q26 |
| State and storage | Q9 |
| Concurrency and conflict | Q10 |
| Interfaces and contracts | Q11, Q12, Q14, Q15, Q25, Q27, Q28 |
| Failure behaviour | Q12, Q13 |
| External dependencies | Q3, Q16, Q17, Q29 |
| Runtime and deployment | Q3, Q18, Q19 |
| Measurable success | Q23 |
| Security and secrets | Q14, Q22, Q24, Q30 |
| Versioning and migration | Q5, Q18, Q21, Q32 |
