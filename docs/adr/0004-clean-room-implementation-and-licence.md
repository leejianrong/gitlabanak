# ADR-0004: Clean-room implementation from the public API, licensed Apache-2.0

- Status: Accepted
- Date: 2026-11-27
- Deciders: project owner, Claude

## Context

GitLabAnak is meant to be shareable and, over time, to be the free self-hosted GitLab-compatible option that Gitea and Forgejo are for GitHub-style workflows. The target version is GitLab Enterprise Edition, whose `ee/` code is proprietary. Copying it, or code derived from it, would make the project unusable as an open product.

## Decision

- Behaviour is derived only from the public GitLab REST documentation, the public OpenAPI-style references, observed responses from a running server, and the JSON schemas that `python-gitlab` itself expects. No GitLab source code (CE or EE) is read or copied into this repository.
- The repository is licensed Apache-2.0, matching go-git, so there is no licence conflict.
- The image is `ghcr.io/leejianrong/gitlabanak`. The README says plainly that the project is unaffiliated with GitLab Inc.
- Observed-response fixtures in `conformance/expected/` are our own recorded data, containing only status codes and message strings, with no tokens or hostnames.

## Alternatives considered

| Option | Why not |
|--------|---------|
| AGPL, as with some forges | Blocks use inside companies, which is the main audience. |
| MIT | Fine, but gives no patent grant, and Apache-2.0 matches the main dependency. |
| Read GitLab CE (MIT) for reference | Increases the risk of accidentally pulling EE-only behaviour, and the observed-server approach is enough. |

## Consequences

- Buys: freedom to publish, mirror and let others contribute.
- Costs: slower fidelity work. Details the docs omit have to be observed from a real server.
- Contributors must be told not to paste GitLab source into issues or pull requests (a line in CONTRIBUTING).
