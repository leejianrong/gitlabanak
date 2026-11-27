# GitLabAnak

A small GitLab API stand-in backed by real git repositories. Point an application's integration tests at it instead of a full GitLab, with no network and a start time measured in milliseconds.

**Status: early, pre-0.1.0.** Only the file endpoints work. The roadmap is in [`docs/PLAN.md`](docs/PLAN.md). This project is not affiliated with GitLab Inc.

## What works today

- `GET /health`
- `GET /api/v4/projects/:id`
- Repository files on the default branch: read, create, update and delete, through `python-gitlab` or plain HTTP.
- Every change is a real git commit. Commit ids are deterministic: the same requests in the same order give the same ids on a fresh server.

Not built yet: branches, the admin API, clone and push, CI lint and pipelines. See [`docs/SLICES.md`](docs/SLICES.md) for the order.

## Run it

```sh
make build
GITLAB_PRIVATE_TOKEN=secret bin/gitlabanak        # listens on :8080
```

| Variable | Default | |
|---|---|---|
| `GITLAB_PRIVATE_TOKEN` | required | Sent by clients as `PRIVATE-TOKEN` (or `Authorization: Bearer`) |
| `GITLAB_DEFAULT_PROJECT_ID` | `100` | The one project that exists at start |
| `GITLAB_DEFAULT_BRANCH` | `main` | |
| `LISTEN_ADDR` | `:8080` | |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

```python
import gitlab
gl = gitlab.Gitlab("http://localhost:8080", private_token="secret")
project = gl.projects.get(100)
project.files.create({"file_path": "a/b.txt", "branch": "main", "content": "hi\n", "commit_message": "add"})
print(project.files.get(file_path="a/b.txt", ref="main").decode())
```

`make demo` runs a longer version of this against the built binary.

## Develop

`make` lists the targets. `make ci` runs what CI runs: format, vet, lint, tidy check, tests with the race detector, and cross-compiles. Requires Go 1.26.11 or newer. See [`CLAUDE.md`](CLAUDE.md) for conventions and [`docs/adr/`](docs/adr/) for the decisions behind the design.

Error texts follow the GitLab API documentation and are **not yet verified** against a real GitLab 19.2.9-ee server. A conformance suite to do that is planned (ADR-0003).

## Licence

Apache-2.0. See [`LICENSE`](LICENSE). The implementation is written from public documentation and observed behaviour, with no GitLab source code (ADR-0004).
