#!/bin/sh
# Runs the conformance suite against a freshly started GitLabAnak: the binary in bin/, or the
# container when IMAGE is set. Needs `uv`. To test a real GitLab instead, skip this script and run
#   GITLAB_URL=... GITLAB_TOKEN=... GITLAB_PROJECT_ID=... uv run --with python-gitlab==8.2.0 --with pytest pytest conformance
set -eu
port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
if [ -n "${IMAGE:-}" ]; then
	cid=$(docker run -d --rm -e GITLAB_PRIVATE_TOKEN=t0k -p "127.0.0.1:$port:8080" "$IMAGE")
	stop() { docker rm -f "$cid" >/dev/null 2>&1 || true; }
else
	GITLAB_PRIVATE_TOKEN=t0k LISTEN_ADDR="127.0.0.1:$port" bin/gitlabanak >/dev/null 2>&1 &
	pid=$!
	stop() { kill $pid 2>/dev/null || true; }
fi
trap stop EXIT
i=0
until curl -sf "127.0.0.1:$port/health" >/dev/null; do
	i=$((i + 1)); [ $i -lt 500 ] || { echo "server did not come up"; exit 1; }
	sleep 0.02
done
GITLAB_URL="http://127.0.0.1:$port" GITLAB_TOKEN=t0k GITLAB_PROJECT_ID=100 CONFORMANCE_ENFORCE_BASELINE=1 \
	uv run --quiet --with python-gitlab==8.2.0 --with pytest pytest -q conformance
