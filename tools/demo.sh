#!/bin/sh
# Runs tools/demo.py against a fresh bin/gitlabanak twice and checks the output is identical,
# which shows commit ids are reproducible across runs. Needs `uv`. Run `make build` first.
set -eu
tok=t0k
run_once() {
	port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
	GITLAB_PRIVATE_TOKEN=$tok LISTEN_ADDR="127.0.0.1:$port" bin/gitlabanak >/dev/null 2>&1 &
	pid=$!
	until curl -sf "127.0.0.1:$port/health" >/dev/null; do sleep 0.02; done
	uv run --quiet --with python-gitlab==8.2.0 tools/demo.py "http://127.0.0.1:$port" $tok || { kill $pid; return 1; }
	kill $pid
}
first=$(run_once)
second=$(run_once)
echo "$first"
[ "$first" = "$second" ] && echo "identical across two fresh servers" || { echo "RUNS DIFFER:"; echo "$second"; exit 1; }
