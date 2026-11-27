# GitLabAnak: Plan

Status: draft · Milestone: 0.1.0 in detail, 0.2.0 to 0.5.0 as roadmap

## Problem

Applications that keep files (such as deployment manifests) in a GitLab repository, through the REST API, need a GitLab to run their integration tests against. A real GitLab is too big and slow to start inside a test run. An earlier closed-source stand-in crashed in its error handler and its source is lost, so nothing exercises branch workflows against anything that behaves like a Git host.

There is also no free, self-hostable, GitLab-compatible service. Gitea and Forgejo cover GitHub-style workflows. Platform and DevSecOps engineers who maintain `.gitlab-ci.yml` files have to push to a real GitLab to see whether a pipeline parses and its jobs start.

## Solution

A single small container that speaks enough of the GitLab v4 REST API, backed by real Git repositories, to be a stand-in for GitLab 19.2.9-ee in tests. It starts in under two seconds. In 0.1.0, `python-gitlab` file and branch calls work against it. Later versions let you `git clone` and `git push` to it, lint a `.gitlab-ci.yml`, and finally point an unmodified `gitlab-runner` at it to run a pipeline locally.

North star, not a requirement: grow into a self-hostable GitLab-compatible forge. Nothing in this plan builds a web UI, users, groups or merge requests. They come in when a test or a user needs them.

## Users and actors

1. **Integration tests** of applications that need an external GitLab, and CI jobs that start the container as a service. Primary for 0.1.0.
2. **Platform and DevSecOps engineers** iterating on CI pipelines. Primary from 0.2.0.
3. **Agents** debugging a failing test through `/_admin/state` and `/_admin/requests`.
4. Teams in offline or air-gapped environments, who mirror the image.

When these conflict, test determinism wins over fidelity to GitLab quirks that tests cannot observe, and GitLab's observed behaviour wins over the documentation.

## Scope

**In 0.1.0.**
- Every request in the call table below, behaving as the response tables say.
- Real Git underneath: branches are refs, commits are objects, deterministic ids.
- Admin API: reset, create project, request log, state dump.
- Container image on ghcr.io and a conformance suite.

**Roadmap, each its own tag.**
- 0.2.0: smart-HTTP `git clone` and `git push`, plus read endpoints (commits, tree, raw file).
- 0.3.0: CI lint (REST and CLI).
- 0.4.0: pipelines as data created on push.
- 0.5.0: runner registration and job pickup.

**Out.**
- Web UI, users, groups, merge requests, webhooks, Argo CD, container registry. No planned consumer calls them. They are added when a test needs them.
- Multi-instance or clustered use.
- GitLab features whose behaviour cannot be observed for lack of a real server.
- Executing job scripts inside GitLabAnak (ADR-0005).
- SSH transport for git.

## Requirements

| ID | Requirement | Status |
|----|-------------|--------|
| R0 | The seven requests in the call table behave as the response tables say, so an unmodified `python-gitlab` client works | Core goal |
| R1 | Branches, commits and files are real Git, deterministic across runs, so a clone later sees the same state | Must-have |
| R2 | Statuses and error bodies match GitLab EE 19.2.9, shown by a conformance suite run against a real server | Must-have |
| R3 | Admin API lets a test reset state, create a project, read requests and read state | Must-have |
| R4 | `/health` answers within 2 s of start, image under 50 MB (target under 15 MB), no network at run time, env vars as listed under Affordances | Must-have |
| R5 | `git clone` and `git push` over HTTP with the same token | Must-have, 0.2.0 |
| R6 | `.gitlab-ci.yml` lint over REST and CLI | Must-have, 0.3.0 |
| R7 | Pipelines and jobs visible via the API after a push | Nice-to-have, 0.4.0 |
| R8 | Unmodified `gitlab-runner` registers and runs jobs | Nice-to-have, 0.5.0 |

### Response tables (R0, R2)

Unverified until the conformance suite has run against GitLab 19.2.9-ee (ADR-0003).

| Case | Status | Body |
|---|---|---|
| missing or wrong token | 401 | `{"message": "401 Unauthorized"}` |
| unknown project | 404 | `{"message": "404 Project Not Found"}` |
| file not found on `ref` | 404 | `{"message": "404 File Not Found"}` |
| GET file without `ref` | 400 | `{"error": "ref is missing"}` |
| branch not found | 404 | `{"message": "404 Branch Not Found"}` |
| branch exists on create | 400 | `{"message": "Branch already exists"}` |
| `ref` for a new branch does not exist | 400 | `{"message": "Invalid reference name: <ref>"}` |
| create a file that exists | 400 | `{"message": "A file with this name already exists"}` |
| update or delete a file that does not exist | 400 | `{"message": "A file with this name doesn't exist"}` |
| file operation on a branch that does not exist | 400 | `{"message": "You can only create or edit files when you are on a branch"}` |
| anything unexpected | 500 | `{"message": "..."}` and a log line, never a crash |

### Requests the client makes (R0)

Recorded from `python-gitlab` 8.2.0. The client sends the token in the `PRIVATE-TOKEN` header. Project id and branch come from the caller's configuration.

| Operation | Request | Notes |
|---|---|---|
| open the project | `GET /api/v4/projects/:id` | any 200 with a JSON project is enough |
| read a file | `GET /api/v4/projects/:id/repository/files/<encoded path>?ref=<branch>` | the path is URL-encoded in the URL |
| update a file | `PUT /api/v4/projects/:id/repository/files/<encoded path>` | JSON body: `file_path` (**still URL-encoded inside the body**), `branch`, `content`, `commit_message`, `start_branch` (null) |
| create a file | `POST /api/v4/projects/:id/repository/files/<encoded path>` | JSON body: `branch`, `content`, `commit_message`. No `file_path` in the body |
| delete a file | `DELETE /api/v4/projects/:id/repository/files/<encoded path>?branch=main&commit_message=...` | the fields are **query parameters, not a body** |
| check a branch | `GET /api/v4/projects/:id/repository/branches/:name` | |
| create a branch | `POST /api/v4/projects/:id/repository/branches` | JSON body: `branch`, `ref` |

- `content` on create and update is plain text unless an `encoding` field says `base64`. GET always returns base64 with `"encoding": "base64"`.
- A file GET that returns 404 means "no such file". Callers branch on it.
- Creating a branch that exists must fail with a message containing `already exists`.
- A new branch starts as a copy of the files on its `ref`, and a commit on a branch changes only that branch.
- GET file response fields: `file_name`, `file_path`, `size`, `encoding`, `content`, `content_sha256`, `ref`, `blob_id`, `commit_id`, `last_commit_id`. Create and update return `{"file_path", "branch"}`. Delete returns 204 with no body.

## Shape

| Part | Mechanism | ADR |
|------|-----------|-----|
| S1 | HTTP layer: `net/http` mux for `/api/v4/*`, `/health`, `/_admin/*`. Middleware order: recover (500 with message, log stack), request recorder, token check (`PRIVATE-TOKEN`, also `Authorization: Bearer`), body limit 10 MB | ADR-0001 |
| S2 | Project registry plus Git store: a map of project id to a go-git repository, one mutex per project, root commit with empty tree on the default branch | ADR-0002 |
| S3 | Files API: path decoding (URL-encoded or plain slashes, body `file_path` decoded), build tree and commit via plumbing, return the GET field set (`blob_id` is the git blob sha, `last_commit_id` from a path-limited log walk) | ADR-0002 |
| S4 | Branches API: create from branch, tag or sha (`ref`), check, same errors as the table | ADR-0002 |
| S5 | Admin API: reset, create project, request log (ring buffer of 10 000, token redacted), state dump (branches, commits, file paths and sizes) | |
| S6 | Persistence: filesystem storage under `GITLAB_DATA_DIR` if set, else memory | ADR-0002 |
| S7 | Error catalogue: one table of case, status and body used by every handler, so the conformance suite and the code share one list | ADR-0003 |
| S8 (0.2.0) | Smart-HTTP git endpoints on `/<namespace>/<project>.git` using go-git server transport. HTTP basic auth with any username and the token as password | ADR-0002 |
| S9 (0.3.0) | `.gitlab-ci.yml` parser and linter on yaml.v3, shared by REST and the `lint` subcommand | ADR-0005 |
| S10 (0.4.0) | Pipeline model created by a push hook inside the git store | ADR-0005 |
| S11 (0.5.0) | Runner API handlers | ADR-0005 |

## Affordances

**Non-UI.**

| Affordance | Kind | Wires to |
|------------|------|----------|
| `GET /health` | endpoint | S1 |
| `GET /api/v4/projects/:id` | endpoint | S2 |
| `GET/POST/PUT/DELETE /api/v4/projects/:id/repository/files/:path` | endpoints | S3 |
| `GET/POST /api/v4/projects/:id/repository/branches[/:name]` | endpoints | S4 |
| `POST /_admin/reset`, `POST /_admin/projects`, `GET /_admin/requests`, `GET /_admin/state` | admin endpoints | S5 |
| `gitlabanak` (serve), `gitlabanak lint <file>` (0.3.0) | CLI | S1, S9 |
| Env: `GITLAB_PRIVATE_TOKEN`, `GITLAB_DATA_DIR`, `GITLAB_DEFAULT_PROJECT_ID` (100), `GITLAB_DEFAULT_BRANCH` (main), `LOG_LEVEL`, port 8080 | config | S1, S2, S6 |

## Implementation decisions

- Layout: `cmd/gitlabanak` (main), `internal/api` (handlers and middleware), `internal/gitstore` (go-git wrapper, the only package that imports go-git), `internal/errs` (the error catalogue), `conformance/` (Python), `Dockerfile`, `Makefile`. A test using `go/parser` fails the build if any package other than `gitstore` imports go-git.
- `gitstore` exposes a small interface (create project, read file, commit change, create branch, list state). API handlers depend on that interface, so handlers are tested with a fake store and the store is tested with no HTTP.
- Commits are deterministic: fixed identity, time equal to parent time plus one second, epoch root (ADR-0002). Commit messages are stored as sent.
- `content` on create and update is plain text unless an `encoding` field says `base64`, which is also accepted. GET always returns base64.
- Delete takes `branch` and `commit_message` from query parameters. Update and create take JSON bodies.
- Image: multi-stage build, `FROM scratch`, non-root user id, port 8080. Tag is the version, never `latest` in docs.
- `LOG_LEVEL` accepts debug, info, warn, error and writes JSON lines to stdout. Tokens never appear in logs.

## Testing approach

Three seams, highest first:

1. **Conformance** (Python, `python-gitlab` 8.2.0) against the built container. This is the acceptance test and the main one. Per-row cases come from the response tables.
2. **Go HTTP tests** with `httptest` against the real store: authorisation, panic recovery, request recording, concurrent writers under `-race`.
3. **Go store tests**: determinism (same operations, same commit ids), branch isolation (a commit on one branch leaves another unchanged), reopen from `GITLAB_DATA_DIR`.

A good test here checks external behaviour (status, body, git state), not which function ran. Commit ids and tree shapes are fair game since they are part of the product.

Gates: `gofmt` and `go vet` failures fail the build, `go test -race -count=1 ./...`, a cross-compile for `linux/amd64` and `linux/arm64`, the image-size and start-time checks from R4, and `go mod tidy` must leave no diff. See `/go-playbook` and `/dev-playbook`.

## Assumed defaults

| ID | Assumed | Cost if wrong |
|----|---------|---------------|
| Q5 | GitLab 19.2.9-ee error texts equal the table above | Re-record `expected/*.json` once a real server is available (ADR-0003) |
| Q9 | Persistence is filesystem git storage under `GITLAB_DATA_DIR` | Low, it is an optional path |
| Q10 | One mutex per project, last write wins | Low for test use |
| Q12 | Unknown routes return 501 and are recorded | Low, and it helps find other callers |
| Q18 | Image `ghcr.io/leejianrong/gitlabanak`, tags from 0.1.0 | Low |
| Q21 | Only `/api/v4` | Low |
| Q23 | Size target under 15 MB | Low, hard limit is 50 MB |
| Q26 | Empty-tree root commit on a new project | Differs from real GitLab for empty projects, recorded in KNOWN_DIFFERENCES |
| Q25 | `ref` accepts branch, tag or sha | Low |
| Q28 | The CLI `lint` subcommand lives in the same binary | A separate binary would add a second artifact |

## Open risks

- **go-git commit on a bare in-memory repository.** If it needs a worktree, S2 changes shape. Revealed by V1.
- **Error texts and field sets for 19.2.9-ee are unverified.** No real server is reachable yet. Revealed by V3 when the suite first runs with `unverified` rows counted.
- **go-git as a push server.** Revealed by V4. Fallback is a small custom receive-pack.
- **Runner protocol for 19.2.x.** Revealed by V7's spike.
- **Scope pull from the forge ambition.** Anything beyond the roadmap needs a new plan, not a bigger milestone.
