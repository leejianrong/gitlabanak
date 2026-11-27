#!/bin/sh
# Starts bin/gitlabanak on a free port and checks the basics against the real binary.
# Usage: tools/smoke.sh   (run `make build` first)
set -eu
port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
log=$(mktemp)
GITLAB_PRIVATE_TOKEN=t0k LISTEN_ADDR="127.0.0.1:$port" bin/gitlabanak >"$log" 2>&1 &
pid=$!
trap 'kill $pid 2>/dev/null; rm -f "$log"' EXIT

start=$(date +%s%N)
i=0
until curl -sf "127.0.0.1:$port/health" >/dev/null; do
	i=$((i + 1)); [ $i -lt 250 ] || { echo "server did not come up:"; cat "$log"; exit 1; }
	sleep 0.02
done
echo "health after $((($(date +%s%N) - start) / 1000000)) ms"

code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
[ "$(code "127.0.0.1:$port/api/v4/projects/100")" = 401 ] || { echo "expected 401 without token"; exit 1; }
[ "$(code -H 'PRIVATE-TOKEN: t0k' "127.0.0.1:$port/api/v4/projects/100")" = 200 ] || { echo "expected 200 for project 100"; exit 1; }
[ "$(code -H 'PRIVATE-TOKEN: t0k' "127.0.0.1:$port/api/v4/projects/7")" = 404 ] || { echo "expected 404 for project 7"; exit 1; }
if command -v git >/dev/null; then
	d=$(mktemp -d)
	url="http://x:t0k@127.0.0.1:$port/group/project.git"
	git clone -q "$url" "$d/c"
	echo hi >"$d/c/f.txt"
	git -C "$d/c" add f.txt
	git -c user.name=t -c user.email=t@example.com -C "$d/c" commit -qm smoke
	git -C "$d/c" push -q origin HEAD:refs/heads/smoke
	[ "$(code -H 'PRIVATE-TOKEN: t0k' "127.0.0.1:$port/api/v4/projects/100/repository/files/f.txt?ref=smoke")" = 200 ] || { echo "pushed file not readable over REST"; exit 1; }
	printf 'stages: [build, test]\ncompile:\n  stage: build\n  script: [make]\nunit:\n  stage: test\n  needs: [compile]\n  script: [make test]\n' >"$d/c/.gitlab-ci.yml"
	git -C "$d/c" add .gitlab-ci.yml
	git -c user.name=t -c user.email=t@example.com -C "$d/c" commit -qm ci
	git -C "$d/c" push -q origin HEAD:refs/heads/ci
	pid1=$(curl -s -H 'PRIVATE-TOKEN: t0k' "127.0.0.1:$port/api/v4/projects/100/pipelines?ref=ci" | python3 -c 'import json,sys; p=json.load(sys.stdin); assert len(p)==1, p; print(p[0]["id"])') || { echo "push did not create one pipeline"; exit 1; }
	curl -s -H 'PRIVATE-TOKEN: t0k' "127.0.0.1:$port/api/v4/projects/100/pipelines/$pid1/jobs" | python3 -c 'import json,sys; j=json.load(sys.stdin); assert [x["name"] for x in j]==["compile","unit"] and j[1]["needs"]==["compile"], j' || { echo "pipeline jobs wrong"; exit 1; }
	rm -rf "$d"
	echo "git clone and push ok, push created a pipeline"
fi
f=$(mktemp)
printf 'stages: [build]\njob:\n  stage: deploy\n  needs: [ghost]\n  script: [x]\n' >"$f"
if bin/gitlabanak lint "$f" >/dev/null; then echo "lint of a broken file should exit 1"; exit 1; fi
cli=$(bin/gitlabanak lint --json "$f" || true)
rest=$(python3 -c 'import json,sys; print(json.dumps({"content": open(sys.argv[1]).read()}))' "$f" |
	curl -s -H 'PRIVATE-TOKEN: t0k' -H 'Content-Type: application/json' --data-binary @- "127.0.0.1:$port/api/v4/projects/100/ci/lint")
python3 - "$cli" "$rest" <<'PY' || { echo "CLI and REST lint disagree"; exit 1; }
import json, sys
cli, rest = json.loads(sys.argv[1]), json.loads(sys.argv[2])
assert cli["errors"] and (cli["valid"], cli["errors"], cli["warnings"]) == (rest["valid"], rest["errors"], rest["warnings"]), (cli, rest)
PY
rm -f "$f"
echo "lint cli and rest agree"
echo "smoke ok"
