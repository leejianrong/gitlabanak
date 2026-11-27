#!/bin/sh
# Checks the built image against the requirements: under 50 MB, answers /health in under 2 s,
# and runs as a non-root user. Usage: tools/imagecheck.sh IMAGE
set -eu
image=${1:?usage: imagecheck.sh IMAGE}

size=$(docker image inspect "$image" --format '{{.Size}}')
echo "image size: $((size / 1024)) KiB"
[ "$size" -lt $((50 * 1024 * 1024)) ] || { echo "FAIL: image is 50 MB or larger"; exit 1; }

user=$(docker image inspect "$image" --format '{{.Config.User}}')
[ -n "$user" ] && [ "$user" != "root" ] && [ "$user" != "0" ] || { echo "FAIL: image runs as root"; exit 1; }

port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
start=$(date +%s%N)
cid=$(docker run -d --rm -e GITLAB_PRIVATE_TOKEN=t0k -p "127.0.0.1:$port:8080" "$image")
trap 'docker rm -f "$cid" >/dev/null 2>&1 || true' EXIT
until curl -sf "127.0.0.1:$port/health" >/dev/null; do
	[ $((($(date +%s%N) - start) / 1000000)) -lt 2000 ] || { echo "FAIL: no /health within 2 s"; docker logs "$cid"; exit 1; }
	sleep 0.02
done
echo "container start to /health: $((($(date +%s%N) - start) / 1000000)) ms"
echo "image ok"
