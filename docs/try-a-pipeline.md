# Try a pipeline locally

Push a `.gitlab-ci.yml` to GitLabAnak and watch a real `gitlab-runner` run it, with no GitLab account and nothing leaving your machine. You need Docker and `git`.

## Start GitLabAnak and a runner

```sh
cd examples/compose
docker compose up -d
```

This starts GitLabAnak on port 8080 (change it with `GITLABANAK_PORT`), registers a runner with the Docker executor, and starts it. The token is `secret` unless you set `GITLAB_PRIVATE_TOKEN`. The runner reaches GitLabAnak at `http://gitlabanak:8080` and starts job containers through your Docker socket.

## Push a pipeline

```sh
git clone http://x:secret@localhost:8080/group/project.git
cd project
cat > .gitlab-ci.yml <<'YML'
stages: [build, test]

build:
  stage: build
  image: alpine:3
  script:
    - echo "hello from the build job"

test:
  stage: test
  image: alpine:3
  script:
    - echo "hello from the test job"
YML
git add .gitlab-ci.yml
git commit -m "add pipeline"
git push origin HEAD:main
```

## Watch it

```sh
curl -s -H 'PRIVATE-TOKEN: secret' localhost:8080/api/v4/projects/100/pipelines/1     # status: pending, running, then success
curl -s -H 'PRIVATE-TOKEN: secret' localhost:8080/api/v4/projects/100/pipelines/1/jobs
curl -s -H 'PRIVATE-TOKEN: secret' localhost:8080/api/v4/projects/100/jobs/2/trace      # what the test job printed
```

The first run pulls `alpine:3` and the runner helper image, so give it a minute. After that a two-job pipeline takes a few seconds.

## What runs and what does not

- `script`, `before_script`, `after_script`, `image`, `variables`, `artifacts` (passed to later stages), `needs`, `extends`, `default`, `allow_failure`, and simple `rules:if` / `only` / `except` are honoured.
- A failing job fails the pipeline and skips the later stages. A job with `needs` starts as soon as the jobs it needs are done.
- Not supported: `services`, `cache`, `trigger`, merge request pipelines, `when: on_failure` / `always` ordering, retries, environments. Jobs marked `when: manual` stay `manual`.
- Pipelines, traces and artifacts are held in memory. Restarting GitLabAnak forgets them (the repositories survive when `GITLAB_DATA_DIR` is set).
- GitLabAnak never runs scripts. The runner does, so what a job can reach is what your runner configuration allows.

To check only syntax, skip all of this and run `gitlabanak lint .gitlab-ci.yml` (see the README).

## Clean up

```sh
docker compose down -v
```
