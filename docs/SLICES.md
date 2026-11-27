# GitLabAnak: Slices

Vertical increments. Each ends in something you can demonstrate. V1 confronts the riskiest unknown: can go-git produce real, deterministic commits behind the files API with no worktree. V1 to V3 make up 0.1.0.

See the "Releases" table at the end for which slice ships which tag.

## V0: Engineering foundation

**Delivers:** the gates every later slice relies on (dev-playbook, go-playbook, lean-code).

**Build plan**

1. Go module and Makefile: bare `make` lists targets, `fmtcheck` runs over `go list` dirs and fails on an empty list, `vet`, `lint` (golangci-lint v2), `tidycheck`, `test` with `-race -count=1`, `xbuild` for linux/amd64 and linux/arm64, `check`, `ci`. Anchored `.gitignore`.
2. Pre-push hook (`make hooks`) running `make check && make test`. CLAUDE.md with honest build status, commands and conventions, including lean-code rules and ADR-0004.
3. GitHub Actions with parallel jobs (static, test, xbuild, govulncheck, gitleaks), cancel-in-progress, and a PR template asking for the net line change.
4. After V1 creates `internal/gitstore`: an import test that fails if anything else imports go-git.

**Demo:** push a PR with a mis-formatted file and see `fmtcheck` fail in CI and in the hook, then fix it and see green.

### Test plan

#### End-to-end

- `make` prints targets and runs nothing. `make ci` passes on a clean tree.
- A mis-formatted file, an untidy go.mod and a data race each fail `make ci`.
- A PR shows all CI jobs running in parallel.

#### Integration

- A file importing go-git outside `internal/gitstore` fails the import test with a message naming ADR-0002.

#### Unit

- None. This slice has no product code.

## V1: Files API over real git

**Delivers:** R0 (files part), R1, R4 (partial)

**Build plan**

1. `go mod init`, Makefile with fmt, vet, test, build. `internal/errs` with the full error catalogue from PLAN.
2. `internal/gitstore`: create an in-memory bare repository, write the empty-tree root commit on `main`, deterministic identity and time.
3. `gitstore` operations: read a file at a ref, create, update, delete a file (build tree through plumbing, commit, move the ref).
4. `internal/api`: `/health`, token middleware, recover middleware, project 100 lookup, and the four file endpoints with path decoding in URL and body.
5. `cmd/gitlabanak`: env vars, port 8080.

**Demo:** Start the binary, run a ten-line `python-gitlab` script that creates, reads, updates and deletes `pipelines/ns1/manifest.yaml` on `main`. Read `GET` back as base64. Restart and replay: the commit ids printed are identical.

**Rests on assumptions:** Q26 (empty-tree root commit). If wrong, only the root commit shape changes.

### Test plan

#### End-to-end

- `python-gitlab` create, get, update, delete on project 100 all succeed, and a get after delete returns 404 `404 File Not Found`.
- Two runs from a fresh start print identical commit ids for the same four operations.
- A forced panic inside a handler returns 500 with a JSON `message` and the next request still succeeds.

#### Integration

- Create then create again on the same path returns 400 "A file with this name already exists".
- Update or delete of a missing file returns 400 "A file with this name doesn't exist".
- A URL path with encoded slashes and one with plain slashes reach the same file.
- A wrong or missing `PRIVATE-TOKEN` returns 401 on every `/api/v4` route and 200 on `/health`.
- `GET` without `ref` returns 400 `{"error": "ref is missing"}`.

#### Unit

- Deterministic time: root equals the fixed epoch, child equals parent plus one second.
- Same sequence of file changes gives the same tree and commit hashes.
- Body `file_path` is URL-decoded, and a missing body `file_path` falls back to the URL path.
- `content_sha256` and `blob_id` are correct for a known content string.

## V2: Branches, admin API, full error table

**Delivers:** R0 (branches), R1, R3, R2 (code side)

**Build plan**

1. `gitstore`: create a branch from a branch, tag or sha, check a branch, list state.
2. Branch endpoints with the "already exists" and "Invalid reference name" errors.
3. File operations on a named branch, with the "you can only create or edit files when you are on a branch" error when the branch is missing.
4. Admin API: reset, create project, request ring buffer with token redaction, state dump.
5. Unknown routes return 501 and are recorded. Unknown project returns 404.
6. Per-project mutex, run the concurrency test under `-race`.

**Demo:** Run a gitflow scenario against it: register a manifest on `branch-ns1`, check the branch exists, create `main` file, read both. `GET /_admin/state` shows `main` and `branch-ns1` with different file lists, and `GET /_admin/requests` shows every request the script made with no token.

**Rests on assumptions:** Q12 (501 for unknown routes), Q15 (ring buffer of 10 000), Q25 (`ref` may be tag or sha).

### Test plan

#### End-to-end

- Branch created from `main` has a copy of `main`'s files. A commit on it leaves `main` unchanged, and the reverse.
- Creating `branch-ns1` twice returns 400 with a message containing `already exists`.
- Creating a branch from a missing `ref` returns 400 `Invalid reference name: <ref>`.
- `POST /_admin/reset` returns the server to the start state and `GET /_admin/state` shows only `main` and no files.
- `POST /_admin/projects` with id 101 then `GET /api/v4/projects/101` returns 200, and project 102 still returns 404.
- A branch-based workflow can be driven by direct calls: a manifest only on `main`, only on a branch, on both, and on neither.

#### Integration

- 50 goroutines writing different files on the same branch all succeed and produce 50 commits in a linear history (`-race` clean).
- The request log never contains the token string, including in headers and query.
- `/_admin/*` without a token returns 401.
- A request on an unknown route returns 501 and appears in the request log.

#### Unit

- Branch creation from a sha and from a tag.
- Ring buffer drops the oldest entry past capacity.

## V3: Conformance suite, container image, 0.1.0 release

**Delivers:** R2, R4

**Build plan**

1. `conformance/`: pytest with `python-gitlab` 8.2.0. One parametrised test per row of the PLAN tables and per row of the call table in PLAN. `expected/*.json` with `unverified` flags and a count printed at the end. `KNOWN_DIFFERENCES.md`.
2. Dockerfile (multi-stage, `FROM scratch`, non-root). Makefile targets `image`, `conformance`.
3. GitHub Actions: Go gates, build image, run the conformance suite against the container, check size and start time, push to ghcr on a version tag.
4. README: what it is, quick start with `docker run`, endpoint table, the GitLab version last verified against, licence, the `unaffiliated` line.
5. README section showing how a consuming repository pins the image tag and runs its integration tests against it, with a `python-gitlab` example.
6. Tag `0.1.0`.

**Demo:** `docker run -p 8080:8080 ghcr.io/.../gitlabanak:0.1.0` then `pytest conformance/` passes.

**Rests on assumptions:** Q5 (error texts). If wrong, the suite's `unverified` count shows exactly which rows to re-record.

### Test plan

#### End-to-end

- A `python-gitlab` consumer test (register, update, delete a manifest on a branch and on `main`) passes against `0.1.0`.
- `pytest conformance/` passes against the container with 100% of the PLAN table rows covered.
- Container start to first `GET /health` 200 takes under 2 s, measured in CI.
- Image size under 50 MB, asserted in CI (target under 15 MB).

#### Integration

- The CI workflow fails if the unverified-row count rises without an explicit change to a baseline file.
- `conformance` run with `GITLAB_URL` pointed at GitLabAnak and at a real GitLab uses the same code path (selected only by env).
- Cross-compile for `linux/amd64` and `linux/arm64` succeeds.

#### Unit

- Dockerfile lint (`hadolint`) passes.
- A test confirms nothing but `internal/gitstore` imports go-git.

## V4: git clone and push, plus read endpoints (0.2.0)

**Delivers:** R5

**Build plan**

1. Spike inside the slice (half a day): `git clone`, commit, `git push` against a go-git server transport handler on an in-memory repository, using the real `git` binary as the client. If push fails, scope a minimal receive-pack before continuing.
2. Smart-HTTP routes at `/<namespace>/<project>.git/*`, with basic auth (any username, token as password) and a project path to id mapping (`POST /_admin/projects` takes an optional `path`).
3. Read endpoints: `GET .../repository/commits`, `.../tree`, `.../files/:path/raw`, `.../commits/:sha`.
4. Conformance rows for the new endpoints. Update README.

**Demo:** `git clone http://x:<token>@localhost:8080/group/proj.git`, add a file, push to a new branch. `GET .../branches/<name>` returns 200 and `GET .../files/<path>?ref=<name>` returns the pushed content. Commit via the API, `git pull`, and see it locally.

**Rests on assumptions:** none new.

### Test plan

#### End-to-end

- Clone, commit, push, then read the file through the REST API gives identical bytes.
- Commit through the REST API, then `git fetch` shows the same commit id.
- Push with a wrong token fails with an authentication error from the `git` client.

#### Integration

- Pushing a branch that already exists with a non-fast-forward is rejected, as git does.
- Concurrent push and REST write on the same project leave a consistent history (`-race`).
- Clone of an empty-tree project works.

#### Unit

- Path to project id mapping, including nested group paths.

## V5: Runner protocol spike and CI lint (0.3.0)

**Delivers:** R6, and the go/no-go for R8

**Build plan**

1. Time-box one day: run `gitlab-runner register` against GitLabAnak with stub `/api/v4/runners`, and have it poll `POST /api/v4/jobs/request` and get 204. Record the exact requests it sends in `docs/spike-runner.md`. Decide go or re-plan V7.
2. `.gitlab-ci.yml` parser and linter (S9), shared by `POST /api/v4/ci/lint`, `POST /api/v4/projects/:id/ci/lint` and `gitlabanak lint <file>`.
3. Rules: unknown keys, missing `script`, undefined stage in `stages`, `needs` that points at a missing job, bad `extends`, `include:local` that is not in the repo. Error text modelled on what a real 19.2.9-ee returns, marked `unverified` until recorded.
4. Conformance rows against the lint endpoint.

**Demo:** `gitlabanak lint broken.gitlab-ci.yml` prints a list of errors and exits 1. Fixing the file prints OK and exits 0. The same file POSTed to the REST endpoint returns the same findings as JSON. `gitlab-runner register` succeeds against the running container.

**Rests on assumptions:** lint error texts are best guesses until compared with a real server. If the runner spike says no-go, V7 is re-planned and V5's lint is unaffected.

### Test plan

#### End-to-end

- A fixture set of ten good and ten broken `.gitlab-ci.yml` files gives the expected exit code and finding list through the CLI.
- The REST lint endpoint returns the same findings as the CLI for the same file.
- `gitlab-runner register` completes against the container and the runner shows as online in the poll log.

#### Integration

- `include:local` resolves against the project repo at the given ref.
- Lint of a file from the repo by `ref` matches lint of the same content posted directly.

#### Unit

- Each lint rule, one fixture and one expected message each.
- `stages` order and default stage handling (`test` when none given).

## V6: Pipelines as data (0.4.0)

**Delivers:** R7

**Build plan**

1. Pipeline model: pipeline, stages, jobs with status `created` or `pending`, ids from a counter.
2. A push or API commit to a branch with a `.gitlab-ci.yml` creates a pipeline. Invalid YAML creates a failed pipeline with a `yaml_errors` field, as GitLab does.
3. Endpoints: `GET .../pipelines`, `.../pipelines/:id`, `.../pipelines/:id/jobs`, `POST .../pipeline` (create on a ref).
4. State dump shows pipelines. Conformance rows.

**Demo:** Push a branch with a three-stage `.gitlab-ci.yml`. `GET .../pipelines` shows one pipeline. `GET .../pipelines/1/jobs` shows the jobs in stage order with correct `needs`.

**Rests on assumptions:** none new beyond lint's.

### Test plan

#### End-to-end

- A push creates exactly one pipeline per pushed ref, and a second push creates a second one.
- Job order across stages matches the YAML and `needs` is surfaced.
- A broken file yields a failed pipeline with the lint errors.

#### Integration

- A commit on a branch with no CI file creates no pipeline.
- `rules: if` on `$CI_COMMIT_BRANCH` includes or excludes jobs correctly.

#### Unit

- Job expansion from `extends` and `default`.
- Predefined variable set (`CI_COMMIT_SHA`, `CI_COMMIT_BRANCH`, `CI_PROJECT_ID`, `CI_PIPELINE_ID`) is correct for a known commit.

## V7: Runner executes jobs (0.5.0)

**Delivers:** R8. Subject to the V5 spike result.

**Build plan**

1. Runner API: registration, `jobs/request` handing out the next `pending` job, job trace upload, job status update, artifact upload as stored bytes in memory.
2. Job state machine: `pending`, `running`, `success`, `failed`, with stage ordering and `needs` honoured. Pipeline status derived from jobs.
3. Compose file in `examples/` starting GitLabAnak and a `gitlab-runner` with the Docker or shell executor.
4. Docs page "Try a pipeline locally".

**Demo:** `docker compose up`, push a repo with a two-stage pipeline (build, test) that runs `echo` in an `alpine` image. `GET .../pipelines/1` ends as `success` and `GET .../jobs/:id/trace` shows the echoed lines.

**Rests on assumptions:** the runner protocol of 19.2.x matches what the spike observed. If the runner needs endpoints we cannot observe, this slice is cut.

### Test plan

#### End-to-end

- The two-stage pipeline above finishes `success` with the expected trace lines.
- A failing script marks its job and pipeline `failed` and skips later stages.
- A job with `needs` runs before its stage would normally start.

#### Integration

- Two runners polling concurrently never get the same job (`-race`).
- Artifacts uploaded by one job are downloadable by the next.

#### Unit

- Job state transitions reject illegal moves.
- Pipeline status derivation from a set of job statuses.

## Releases

| Tag | Slices | Holds |
|-----|--------|-------|
| 0.1.0 | V1, V2, V3 | Drop-in for python-gitlab file and branch calls, real git, conformance suite |
| 0.2.0 | V4 | clone and push, read endpoints |
| 0.3.0 | V5 | CI lint |
| 0.4.0 | V6 | Pipelines as data |
| 0.5.0 | V7 | Runner executes jobs |
