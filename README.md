# GitLabAnak

A small GitLab API stand-in backed by real git repositories. Point an application's integration tests at it instead of a full GitLab, with no network and a start time measured in milliseconds.

**Status: 0.3.0.** File, branch and read endpoints work, and `git clone`, `git fetch` and `git push` work over HTTP. The roadmap is in [`docs/PLAN.md`](docs/PLAN.md). This project is not affiliated with GitLab Inc.

## What works today

- `GET /health`
- `GET /api/v4/projects/:id`
- Repository files on any branch: read, create, update and delete, through `python-gitlab` or plain HTTP.
- Branches: create from a branch, tag or commit sha, and look one up. A new branch starts as a copy of its `ref`, and a commit on one branch leaves the others alone.
- `git clone http://x:<token>@localhost:8080/group/project.git`, then commit and push, including new branches and branch deletes. Any username works, the token is the password. Each project has a clone path: `group/project` for the default one, `group/project-<id>` or the `path` given to `POST /_admin/projects` for others. Commits made through the REST API show up on the next `git fetch` with the same ids.
- Read endpoints: `GET .../repository/commits` (`ref_name`, `page`, `per_page`), `.../commits/:sha`, `.../repository/tree` (`path`, `ref`, `recursive`) and `.../files/:path/raw`.
- Every change is a real git commit. Commit ids are deterministic: the same requests in the same order give the same ids on a fresh server.
- Test helpers under `/_admin` (same token): `POST /_admin/reset`, `POST /_admin/projects` (`{"id": 101, "default_branch": "main"}`), `GET /_admin/requests` (what clients sent, token redacted), `GET /_admin/state` (branches, commits and files).
- Any other route answers `501 not implemented` and appears in the request log, so you can see what a client needs that is missing.

- CI lint: `POST /api/v4/projects/:id/ci/lint` (body `content`, optional `ref` for `include:local`), `GET /api/v4/projects/:id/ci/lint` (the repository's own `.gitlab-ci.yml`, `content_ref`) and `POST /api/v4/ci/lint`. Checks unknown job keys, missing `script`, undefined `stage`, `needs` on a missing job, bad `extends`, and `include:local` files that do not exist. `include:remote`, `project` and `template` are skipped with a warning.

Not built yet: pipelines and running jobs. See [`docs/SLICES.md`](docs/SLICES.md) for the order.

## Lint a pipeline file

No server needed:

```sh
gitlabanak lint .gitlab-ci.yml            # prints findings, exits 1 if any, prints OK and exits 0 otherwise
gitlabanak lint --json .gitlab-ci.yml     # the same findings as the REST response
docker run --rm -v "$PWD:/w" ghcr.io/leejianrong/gitlabanak:0.3.0 lint /w/.gitlab-ci.yml
```

`include:local` paths resolve against the file's directory, or `--root`. The messages follow GitLab's wording but are not yet verified against a real server, so treat a clean result as "no obvious mistakes", not as a guarantee GitLab accepts the file.

## Run it

```sh
make build
GITLAB_PRIVATE_TOKEN=secret bin/gitlabanak        # listens on :8080
```

| Variable | Default | |
|---|---|---|
| `GITLAB_PRIVATE_TOKEN` | required | Sent by clients as `PRIVATE-TOKEN` (or `Authorization: Bearer`) |
| `GITLAB_DEFAULT_PROJECT_ID` | `100` | The one project that exists at start |
| `GITLAB_DEFAULT_PROJECT_PATH` | `group/project` | Clone path of the default project |
| `GITLAB_DEFAULT_BRANCH` | `main` | |
| `GITLAB_DATA_DIR` | unset | If set, projects are kept as bare git repositories under `<dir>/projects/` and reopened at start. Unset means memory only, nothing written to disk |
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

```sh
docker run --rm -p 8080:8080 -e GITLAB_PRIVATE_TOKEN=secret ghcr.io/leejianrong/gitlabanak:0.3.0
```

Then clone, change and push:

```sh
git clone http://x:secret@localhost:8080/group/project.git && cd project
git checkout -b feature && echo hi > f.txt && git add f.txt && git commit -m add
git push origin feature     # now GET /api/v4/projects/100/repository/files/f.txt?ref=feature works
```

### Using it from your own tests

Pin the image tag, not `latest`, so a new release never changes your results unannounced. Start the container before the tests and point `python-gitlab` at it (or in CI, run it as a service container). `examples/consumer/test_consumer.py` is a runnable example; `make consumer IMAGE=ghcr.io/leejianrong/gitlabanak:0.3.0` runs it against the published image. Call `POST /_admin/reset` between tests to start clean.

In the container (non-root, so a new volume at `/data` is writable), keep data across restarts with `-e GITLAB_DATA_DIR=/data -v gitlabanak-data:/data`.

## Develop

`make` lists the targets. `make ci` runs what CI runs: format, vet, lint, tidy check, tests with the race detector, and cross-compiles. Requires Go 1.26.11 or newer. See [`CLAUDE.md`](CLAUDE.md) for conventions and [`docs/adr/`](docs/adr/) for the decisions behind the design.

Error texts follow the GitLab API documentation and are **not yet verified** against a real GitLab 19.2.9-ee server. The conformance suite in `conformance/` (`make conformance`) runs `python-gitlab` against the server and prints how many expected responses are still unverified (ADR-0003).

## Licence

Apache-2.0. See [`LICENSE`](LICENSE). The implementation is written from public documentation and observed behaviour, with no GitLab source code (ADR-0004).
