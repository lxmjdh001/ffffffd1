#!/usr/bin/env bash
# Local launcher: build and run D1 Apk in the background.
set -euo pipefail
cd "$(dirname "$0")"
D1_ROOT="$(pwd -P)"

for tool in curl lsof; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "缺少启动工具：$tool" >&2
    exit 1
  fi
done

if [[ -f .env.development ]]; then
  source .env.development
fi
D1_ADDR="${HTTP_ADDR:-127.0.0.1:8088}"
D1_PORT="${D1_ADDR##*:}"
D1_HOST="${D1_ADDR%:*}"
case "$D1_HOST" in "" | "0.0.0.0" | "[::]") D1_HOST="127.0.0.1" ;; esac
D1_URL="${APP_URL:-http://localhost:8088}"
D1_LOG="$D1_ROOT/data/dev-server.log"
D1_PID_FILE="$D1_ROOT/data/dev-server.pid"
mkdir -p "$D1_ROOT/data"

healthy() {
  [[ "$(curl --noproxy '*' -fsS --connect-timeout 1 --max-time 2 \
    "http://$D1_HOST:$D1_PORT/healthz" 2>/dev/null)" == '{"status":"ok"}' ]]
}

our_process() {
  local pid="$1" process_dir process_command
  [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null || return 1
  process_dir="$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p')"
  [[ "$process_dir" == "$D1_ROOT" ]] || return 1
  process_command="$(ps -p "$pid" -o command=)"
  [[ "$process_command" == "./bin/d1apk" ||
     "$process_command" == "$D1_ROOT/bin/d1apk" ||
     "$process_command" == *"$D1_ROOT/scripts/dev.sh" ]]
}

show_access() {
  echo "登录地址：${D1_URL%/}/#/login"
  echo "管理后台：${D1_URL%/}/#/admin/settings"
  echo "账号配置：$D1_ROOT/.env.development"
  echo "运行日志：$D1_LOG"
}

# Recognize services started earlier by dev.sh as well as this launcher.
D1_LISTENERS="$(lsof -nP -t -iTCP:"$D1_PORT" -sTCP:LISTEN 2>/dev/null || true)"
if [[ -n "$D1_LISTENERS" ]]; then
  for D1_PID in $D1_LISTENERS; do
    if our_process "$D1_PID" && healthy; then
      echo "$D1_PID" > "$D1_PID_FILE"
      echo "D1 Apk 已运行，无需重复启动（PID：${D1_PID}）。"
      show_access
      exit 0
    fi
  done
  echo "端口 $D1_PORT 已被占用或已有服务未就绪，请先检查，未启动新进程。" >&2
  exit 1
fi

if [[ -f "$D1_PID_FILE" ]]; then
  D1_PID="$(cat "$D1_PID_FILE")"
  if our_process "$D1_PID"; then
    echo "D1 Apk 正在启动（PID：${D1_PID}），请查看日志：$D1_LOG"
    exit 0
  fi
fi

for tool in go npm python3; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "缺少 $tool，请先安装 Go、Node.js（含 npm）和 Python 3。" >&2
    exit 1
  fi
done
if [[ ! -f .env.development ]] && ! command -v openssl >/dev/null 2>&1; then
  echo "首次启动需要 openssl 生成本地账号配置。" >&2
  exit 1
fi

echo "正在构建并启动 D1 Apk，首次启动可能需要下载依赖……"
echo "构建与运行日志：$D1_LOG"
# A separate session survives both terminal closure and launcher process cleanup.
D1_PID="$(python3 - "$D1_ROOT" "$D1_LOG" <<'PY'
import subprocess
import sys

root, log_path = sys.argv[1:]
with open(log_path, "ab") as log:
    process = subprocess.Popen(
        [root + "/scripts/dev.sh"],
        cwd=root,
        stdin=subprocess.DEVNULL,
        stdout=log,
        stderr=subprocess.STDOUT,
        start_new_session=True,
    )
print(process.pid)
PY
)"
echo "$D1_PID" > "$D1_PID_FILE"

for ((D1_TRY = 0; D1_TRY < 120; D1_TRY++)); do
  if ! kill -0 "$D1_PID" 2>/dev/null; then
    rm -f "$D1_PID_FILE"
    echo "启动失败，最近日志如下：" >&2
    tail -n 25 "$D1_LOG" >&2
    exit 1
  fi
  if healthy; then
    echo "D1 Apk 启动成功（PID：${D1_PID}），关闭终端后仍会运行。"
    show_access
    exit 0
  fi
  sleep 1
done

echo "构建仍在后台进行，请稍后再次执行本脚本检查状态。"
echo "查看进度：tail -f \"$D1_LOG\""
