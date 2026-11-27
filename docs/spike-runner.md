# Spike: can an unmodified gitlab-runner talk to GitLabAnak?

**Decision: go.** Registration and an empty job poll work against a handful of stub routes, so V7 (the runner executes jobs) stays as planned in ADR-0005. The hard part is still ahead: handing out a real job (`POST /jobs/request` with a body) and receiving traces and status updates. This spike did not touch those.

## What was run

`gitlab/gitlab-runner:latest`, which reported version 19.6.1, against `bin/gitlabanak` listening on `0.0.0.0:18100`, using `--network host`:

```sh
gitlab-runner register --non-interactive --url http://127.0.0.1:18100 --token glrt-spike --executor shell --name spike
gitlab-runner run
```

## What the runner sent

| Request | When | Body | We answered |
|---|---|---|---|
| `POST /api/v4/runners/verify` | registration | `{"info": {name, version, revision, platform, architecture, executor, shell, features{...}, config}, "token": "<runner token>", "system_id": "r_..."}` | `200 {"id": 1, "token": "<same token>", "token_expires_at": null}` |
| `POST /api/v4/jobs/request` | every poll | an `info` block like the one above plus the runner `token` (other fields not recorded) | `204 No Content` |

Four polls in nine seconds, all answered 204, and the runner logged no errors. `POST /api/v4/runners` (the older registration-token flow) is also stubbed but was not exercised by this runner version, which went straight to `verify` when given a `glrt-` token.

## What this tells us about V7

- Runner routes authenticate with the runner token in the **body**, not `PRIVATE-TOKEN`. They must bypass the API token check (done).
- The `features` block lists what this runner can do (`variables`, `artifacts`, `cache`, `trace_checksum`, `job_inputs`, ...). A real job response has to stay within it.
- The stub accepts any non-empty token. V7 needs real runner records: created through the API, with the token checked on every poll.
- Not yet verified: the response headers a real server sends on a poll (`X-GitLab-Last-Update`), and the shape of a job payload. Both are V7 work.

## Reproduce

Start the server with any `GITLAB_PRIVATE_TOKEN` of 8 or more characters, run the two commands above, and read `GET /_admin/requests`. A one-letter token also works but the request log's token redaction then mangles every letter of that kind in the logged body.
