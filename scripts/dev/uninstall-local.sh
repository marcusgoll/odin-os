#!/usr/bin/env bash
set -euo pipefail

bin_dir="${ODIN_INSTALL_BIN_DIR:-${HOME}/.local/bin}"
target_path="$bin_dir/odin"

rm -f "$target_path"

echo "removed $target_path"
