#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ -e .env ]]; then echo '.env already exists; refusing to overwrite.' >&2; exit 1; fi
umask 077
cp .env.example .env
app_secret=$(openssl rand -hex 32)
db_password=$(openssl rand -hex 24)
admin_password=$(openssl rand -hex 16)
# Only generated hex is interpolated; no user-provided shell text is evaluated.
sed -e "s/^APP_SECRET=.*/APP_SECRET=$app_secret/" -e "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$db_password/" -e "s/^ADMIN_PASSWORD=.*/ADMIN_PASSWORD=$admin_password/" .env > .env.generated
mv .env.generated .env
echo 'Created .env with unique secrets. Configure real Turnstile keys, domains and payment channels before startup.'
