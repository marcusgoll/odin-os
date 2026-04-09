#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source_path="${ODIN_INSTALL_SOURCE:-$repo_root/bin/odin}"
bin_dir="${ODIN_INSTALL_BIN_DIR:-${HOME}/.local/bin}"
target_path="$bin_dir/odin"

if [[ ! -e "$source_path" ]]; then
  echo "odin install source missing: $source_path" >&2
  exit 1
fi

mkdir -p "$bin_dir"
ln -sfn "$source_path" "$target_path"

echo "installed $target_path -> $source_path"
