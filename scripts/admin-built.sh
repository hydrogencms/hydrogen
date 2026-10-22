#!/usr/bin/env bash
# Build the admin (make admin) unless admin/dist/index.html exists: for scripts that embed or drive the admin.
# Rebuild by hand after changing admin/ui. Run from anywhere.
set -euo pipefail
cd "$(dirname "$0")/.."
[ -f admin/dist/index.html ] || make admin
