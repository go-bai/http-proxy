#!/bin/sh
set -e

if command -v systemctl >/dev/null 2>&1; then
    case "$1" in
        remove|deconfigure)
            systemctl stop http-proxy.service >/dev/null 2>&1 || true
            systemctl disable http-proxy.service >/dev/null 2>&1 || true
            ;;
    esac
fi

exit 0
