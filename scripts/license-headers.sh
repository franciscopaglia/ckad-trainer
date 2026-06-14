#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
# Ensure every Go source file carries the GPL SPDX header.
#
#   scripts/license-headers.sh           # add the header to any file missing it
#   scripts/license-headers.sh --check   # exit non-zero if any file lacks it (CI)
#
# Idempotent: files that already have an SPDX-License-Identifier are left alone.
set -euo pipefail

cd "$(dirname "$0")/.."

check=false
[ "${1:-}" = "--check" ] && check=true

add_header() {
  local f=$1 tmp
  tmp=$(mktemp)
  {
    printf '// Copyright (C) 2026 Francisco Paglia\n'
    printf '// SPDX-License-Identifier: GPL-3.0-or-later\n\n'
    cat "$f"
  } >"$tmp"
  mv "$tmp" "$f"
}

missing=()
while IFS= read -r f; do
  if head -5 "$f" | grep -q 'SPDX-License-Identifier'; then
    continue
  fi
  if $check; then
    missing+=("$f")
  else
    add_header "$f"
    echo "added header: $f"
  fi
done < <(find . -name '*.go' -not -path './vendor/*' | sort)

if $check && [ ${#missing[@]} -gt 0 ]; then
  echo "Go files missing the SPDX header:" >&2
  printf '  %s\n' "${missing[@]}" >&2
  echo "run: scripts/license-headers.sh" >&2
  exit 1
fi

$check && echo "ok: all Go files have the SPDX header"
exit 0
