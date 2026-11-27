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
	rm -rf "$d"
	echo "git clone and push ok"
fi
echo "smoke ok"
