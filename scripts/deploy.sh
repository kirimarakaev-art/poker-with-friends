#!/usr/bin/env bash
# Run from the repo checkout on the server. Settings come from .env.
set -euo pipefail

cd "$(dirname "$0")/.."
git pull --ff-only
docker network inspect web >/dev/null 2>&1 || docker network create web
docker compose -f compose.yaml -f compose.prod.yaml up -d --build
docker image prune -f
