#!/bin/sh
# Registers the real gitlab-runner against the image and checks it polls for jobs and gets 204.
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
