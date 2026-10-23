#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ ! -f .env.development ]]; then
  umask 077
  {
    echo 'APP_ENV=development'
    echo 'APP_URL=http://localhost:8088'
    echo 'HTTP_ADDR=127.0.0.1:8088'
    echo 'DATA_DIR=data'
    echo 'ADMIN_EMAIL=admin@d1apk.local'
    echo "APP_SECRET=$(openssl rand -hex 32)"
    echo "ADMIN_PASSWORD=$(openssl rand -hex 16)"
  } > .env.development
fi
set -a
source .env.development
set +a
if [[ ! -d web/node_modules ]]; then npm --prefix web ci; fi
npm --prefix web run build
mkdir -p bin
go build -o bin/d1apk ./cmd/server
echo 'D1 Apk: http://localhost:8088/#/welcome'
echo 'Local admin credentials are in .env.development (gitignored).'
exec ./bin/d1apk
