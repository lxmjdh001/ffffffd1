#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go test ./...
go vet ./...
npm --prefix web run test:deps
npm --prefix web test
npm --prefix web run build
if [[ -n "${TEST_DATABASE_URL:-}" ]]; then
  go test -race -count=1 ./internal/platform
fi
