# ADR-0001: Build in Go as one static binary, with go-git and yaml.v3 as the only third-party code

- Status: Accepted
- Date: 2026-11-27
- Deciders: project owner, Claude

## Context

The service must answer `/health` in under 2 s, ship in an image under 50 MB, run with no network, and be safe to mirror into an offline or air-gapped registry. This is a clean rewrite of an earlier closed-source stand-in whose source is lost, with no compatibility burden beyond a few env vars and port 8080 (listed in PLAN). The long-term goal is a self-hostable GitLab-compatible forge, so the language has to handle a git server, YAML parsing and process orchestration later.

## Decision

- Go, one static binary (`CGO_ENABLED=0`), image `FROM scratch` plus CA certificates only if a later feature needs outbound TLS.
- HTTP uses `net/http` and its pattern mux, JSON uses `encoding/json`.
- Third-party code is limited to `github.com/go-git/go-git/v5` (Apache-2.0, pure Go, ADR-0002) and `gopkg.in/yaml.v3` (MIT/Apache-2.0, needed from 0.3.0 for `.gitlab-ci.yml`). Adding a third dependency needs a new ADR.
- The conformance suite is Python (ADR-0003). That is test code and is not in the image.

## Alternatives considered

| Option | Why not |
|--------|---------|
| Rust | Equal image size, but no mature pure-Rust git server library, and a slower build loop for a project where most work is protocol fidelity. |
| Python (FastAPI/Flask) | The image would be 50 MB or more, startup is slower, and there is no static artifact for offline mirroring. |
| Shell out to the `git` binary | It would need git in the image, which breaks `FROM scratch` and adds hundreds of MB if a full distro is used. |

## Consequences

- Buys: tiny image, fast start, a trivial cross-compile for offline environments, and a race detector in tests.
- Costs: go-git lags upstream git in some areas (see ADR-0002 risks). Every dependency we add in the future has to pass the licence and offline check.
- Forecloses: easy reuse of Python tooling inside the server.
