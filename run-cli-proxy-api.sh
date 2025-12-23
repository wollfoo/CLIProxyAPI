#!/usr/bin/env bash
set -euo pipefail

APP="/home/azureuser/CLIProxyAPI/cli-proxy-api"
CFG="/home/azureuser/CLIProxyAPI/config.yaml"
LOG_DIR="/home/azureuser/CLIProxyAPI/logs"
PID_FILE="/home/azureuser/CLIProxyAPI/cli-proxy-api.pid"
LOG_FILE="$LOG_DIR/cli-proxy-api.log"

mkdir -p "$LOG_DIR"

is_running() {
  if [[ -f "$PID_FILE" ]]; then
    local pid
    pid="$(cat "$PID_FILE" || true)"
    if [[ -n "${pid:-}" ]] && kill -0 "$pid" 2>/dev/null; then
      return 0
    fi
  fi
  return 1
}

start() {
  if is_running; then
    echo "Đang chạy rồi. PID=$(cat "$PID_FILE")"
    exit 0
  fi

  nohup "$APP" --config "$CFG" >>"$LOG_FILE" 2>&1 &
  echo $! > "$PID_FILE"
  echo "Đã start. PID=$(cat "$PID_FILE"). Log: $LOG_FILE"
}

stop() {
  if ! is_running; then
    echo "Không thấy đang chạy."
    rm -f "$PID_FILE" 2>/dev/null || true
    exit 0
  fi

  local pid
  pid="$(cat "$PID_FILE")"
  kill "$pid" 2>/dev/null || true

  # chờ tối đa 10s
  for _ in {1..20}; do
    if kill -0 "$pid" 2>/dev/null; then
      sleep 0.5
    else
      rm -f "$PID_FILE"
      echo "Đã stop."
      exit 0
    fi
  done

  echo "Không dừng kịp, kill -9..."
  kill -9 "$pid" 2>/dev/null || true
  rm -f "$PID_FILE"
  echo "Đã stop (force)."
}

status() {
  if is_running; then
    echo "RUNNING PID=$(cat "$PID_FILE")"
  else
    echo "STOPPED"
  fi
}

restart() {
  stop
  start
}

case "${1:-start}" in
  start) start ;;
  stop) stop ;;
  restart) restart ;;
  status) status ;;
  *)
    echo "Usage: $0 {start|stop|restart|status}"
    exit 1
    ;;
esac
