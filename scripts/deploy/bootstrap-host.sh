#!/bin/sh
set -eu

BINDER_DEVICES=${BINDER_DEVICES:-binder,hwbinder,vndbinder}
SUDO_PASS=${SUDO_PASS:-}

if [ -z "$SUDO_PASS" ] && [ -n "${SUDO_PASS_B64:-}" ]; then
  SUDO_PASS=$(printf '%s' "$SUDO_PASS_B64" | base64 -d)
fi

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require_cmd sudo
require_cmd docker

sudo_run() {
  if sudo -n true 2>/dev/null; then
    sudo "$@"
    return
  fi

  if [ -n "$SUDO_PASS" ]; then
    printf '%s\n' "$SUDO_PASS" | sudo -S -p '' "$@"
    return
  fi

  sudo "$@"
}

if ! docker compose version >/dev/null 2>&1; then
  echo "docker compose is not available" >&2
  exit 1
fi

if ! lsmod | grep -q '^binder_linux'; then
  sudo_run modprobe binder_linux "devices=$BINDER_DEVICES"
fi

if ! lsmod | grep -q '^binder_linux'; then
  echo "binder_linux failed to load" >&2
  exit 1
fi

sudo_run mkdir -p /etc/modules-load.d /etc/modprobe.d /dev/binderfs
printf 'binder_linux\n' | sudo_run tee /etc/modules-load.d/jucobot-redroid.conf >/dev/null
printf 'options binder_linux devices=%s\n' "$BINDER_DEVICES" | sudo_run tee /etc/modprobe.d/jucobot-redroid.conf >/dev/null

if ! mountpoint -q /dev/binderfs; then
  sudo_run mount -t binder binder /dev/binderfs || true
fi

if [ -d /dev/binderfs ]; then
  [ -e /dev/binder ] || [ ! -e /dev/binderfs/binder ] || sudo_run ln -sf /dev/binderfs/binder /dev/binder
  [ -e /dev/hwbinder ] || [ ! -e /dev/binderfs/hwbinder ] || sudo_run ln -sf /dev/binderfs/hwbinder /dev/hwbinder
  [ -e /dev/vndbinder ] || [ ! -e /dev/binderfs/vndbinder ] || sudo_run ln -sf /dev/binderfs/vndbinder /dev/vndbinder
fi

for dev in /dev/binder /dev/hwbinder /dev/vndbinder; do
  if [ ! -e "$dev" ]; then
    echo "missing binder device: $dev" >&2
    exit 1
  fi
done

printf 'binder bootstrap ready\n'
ls -l /dev/binder /dev/hwbinder /dev/vndbinder
