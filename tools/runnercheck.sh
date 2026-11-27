#!/bin/sh
# Registers the real gitlab-runner (shell executor) against the image, runs a two-stage pipeline
# with an artifact to success, and runs a failing pipeline to failed with the later stage skipped.
# Needs docker. Usage: tools/runnercheck.sh <image>   (docs/spike-runner.md has the background)
set -eu
image=${1:?usage: runnercheck.sh <image>}
runner=${RUNNER_IMAGE:-gitlab/gitlab-runner:latest}
port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
cfg=$(mktemp -d)
sid=$(docker run -d --rm --network host -e GITLAB_PRIVATE_TOKEN=runnercheck-token -e LISTEN_ADDR="127.0.0.1:$port" "$image")
cleanup() {
	docker rm -f "$sid" >/dev/null 2>&1 || true
	docker rm -f runnercheck >/dev/null 2>&1 || true
	docker run --rm -v "$cfg:/c" alpine rm -rf /c/. >/dev/null 2>&1 || true
	rmdir "$cfg" 2>/dev/null || true
}
trap cleanup EXIT
until curl -sf "127.0.0.1:$port/health" >/dev/null; do sleep 0.1; done
docker run --rm --network host -v "$cfg:/etc/gitlab-runner" "$runner" register --non-interactive \
	--url "http://127.0.0.1:$port" --token glrt-runnercheck --executor shell --name runnercheck >/dev/null 2>&1 ||
	{ echo "runner registration failed"; exit 1; }
docker run -d --rm --name runnercheck --network host -v "$cfg:/etc/gitlab-runner" "$runner" run >/dev/null
polls=0
for _ in $(seq 1 60); do
	polls=$(curl -s -H 'PRIVATE-TOKEN: runnercheck-token' "127.0.0.1:$port/_admin/requests" | python3 -c 'import json,sys; print(sum(r["path"]=="/api/v4/jobs/request" for r in json.load(sys.stdin)["requests"]))')
	[ "$polls" -ge 2 ] && break
	sleep 0.5
done
[ "$polls" -ge 2 ] || { echo "runner did not poll for jobs ($polls polls)"; exit 1; }
echo "runner registered and polled $polls times"

api() { curl -s -H 'PRIVATE-TOKEN: runnercheck-token' "127.0.0.1:$port/api/v4/projects/100/$1"; }
# wait_pipeline <ref> <status>: waits for the newest pipeline on ref to reach status, prints its id
wait_pipeline() {
	for _ in $(seq 1 90); do
		out=$(api "pipelines?ref=$1" | python3 -c 'import json,sys; p=json.load(sys.stdin); print(p[0]["id"], p[0]["status"]) if p else print("- none")')
		[ "${out#* }" = "$2" ] && { echo "${out%% *}"; return 0; }
		sleep 1
	done
	echo "pipeline on $1 did not reach $2 (last: $out)" >&2
	return 1
}
work=$(mktemp -d)
git clone -q "http://x:runnercheck-token@127.0.0.1:$port/group/project.git" "$work/c"
push() { # push <branch> <ci file content on stdin>
	cat >"$work/c/.gitlab-ci.yml"
	git -C "$work/c" add .gitlab-ci.yml
	git -c user.name=t -c user.email=t@example.com -C "$work/c" commit -qm "ci for $1"
	git -C "$work/c" push -q origin "HEAD:refs/heads/$1"
}

push good <<'YML'
stages: [build, test]
compile:
  stage: build
  script:
    - echo "building on $CI_COMMIT_BRANCH"
    - mkdir out && echo artifact-data > out/a.txt
  artifacts:
    paths: [out/]
unit:
  stage: test
  script:
    - echo "testing sha $CI_COMMIT_SHORT_SHA"
    - cat out/a.txt
YML
pid=$(wait_pipeline good success)
trace=$(api "jobs/$(api "pipelines/$pid/jobs" | python3 -c 'import json,sys; print([j["id"] for j in json.load(sys.stdin) if j["name"]=="unit"][0])')/trace")
echo "$trace" | grep -q "artifact-data" || { echo "second job did not see the first job's artifact"; echo "$trace"; exit 1; }
echo "$trace" | grep -q "testing sha " || { echo "trace lacks the echoed line"; exit 1; }
echo "two-stage pipeline succeeded, artifact passed between jobs"

push bad <<'YML'
stages: [build, test]
broken:
  stage: build
  script: [exit 3]
never:
  stage: test
  script: [echo unreachable]
YML
pid=$(wait_pipeline bad failed)
states=$(api "pipelines/$pid/jobs" | python3 -c 'import json,sys; print(",".join(j["name"]+"="+j["status"] for j in json.load(sys.stdin)))')
[ "$states" = "broken=failed,never=skipped" ] || { echo "unexpected job states: $states"; exit 1; }
echo "failing pipeline failed and skipped the later stage"
rm -rf "$work"
