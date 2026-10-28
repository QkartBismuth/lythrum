#!/usr/bin/env bash
# Установка Lythrum сессией SDDM: сборка бинарника из текущего HEAD,
# бинарник → /usr/local/bin, .desktop → /usr/share/wayland-sessions.
# Требует root: sudo ./packaging/install-sddm.sh
#
# Примечание: пока вывод на экран не реализован (ARCHITECTURE.md,
# «Отложено»), сессия работает вслепую — ядро живо, экран чёрный.
# Возврат в SDDM: Ctrl+Alt+F1…F7.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "$(id -u)" -ne 0 ]; then
    echo "lythrum: нужен root — запустите через sudo" >&2
    exit 1
fi

echo "lythrum: сборка из $(git rev-parse --short HEAD 2>/dev/null || echo 'без git')..."
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
go build -o "$tmp" ./cmd/lythrum

echo "lythrum: установка бинарника и сессии..."
install -Dm755 "$tmp" /usr/local/bin/lythrum
install -Dm644 packaging/lythrum.desktop /usr/share/wayland-sessions/lythrum.desktop

echo "lythrum: готово — «Lythrum» появится в выборе сессий SDDM"
