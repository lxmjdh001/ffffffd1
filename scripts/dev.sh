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
if [[ "${SERVER_ROLE:-all}" != "download" ]]; then
  echo '正在检查模版库真实封面截图环境……'
  if ! ./bin/d1apk check-landing-browser; then
    echo '真实封面截图检查失败，项目尚未启动。' >&2
    echo 'macOS 请安装 Google Chrome；Debian 请安装 chromium、fonts-noto-cjk 和 fonts-noto-color-emoji。' >&2
    echo '自定义安装位置可在 .env.development 配置 LANDING_SCREENSHOT_BROWSER 为浏览器可执行文件路径。' >&2
    exit 1
  fi
fi
echo 'D1 Apk: http://localhost:8088/#/welcome'
echo 'Local admin credentials are in .env.development (gitignored).'
exec ./bin/d1apk
