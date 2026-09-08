#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ENV_FILE:-$ROOT/.env}"
TRACKED_ENV_FILE="$ROOT/.env.example"
cd "$ROOT"

read_version() {
  local line version="" count=0
  [[ -f "$1" ]] || { echo "release: missing $1" >&2; exit 1; }
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%$'\r'}"
    [[ "$line" == version=* ]] || continue
    count=$((count + 1))
    version="${line#version=}"
  done < "$1"
  if [[ "$count" != 1 || ! "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    echo "release: $1 must contain exactly one valid version=vX.Y.Z entry" >&2
    exit 1
  fi
  printf '%s\n' "$version"
}

write_version() {
  local temporary
  temporary="$(mktemp)"
  if awk -v version="$NEW_VERSION" '
    /^version=/ { print "version=" version; next }
    { print }
  ' "$1" > "$temporary" && cat "$temporary" > "$1"; then
    rm -f "$temporary"
  else
    rm -f "$temporary"
    return 1
  fi
}

CURRENT_VERSION="$(read_version "$ENV_FILE")"
# Validate both files before modifying either; never source local configuration.
read_version "$TRACKED_ENV_FILE" >/dev/null

dirty="$(git status --porcelain --untracked-files=normal)"
if [[ -n "$dirty" ]]; then
  echo "release: worktree must be clean:" >&2
  printf '%s\n' "$dirty" >&2
  exit 1
fi

if [[ -n "${V:-}" ]]; then
  NEW_VERSION="${V//$'\r'/}"
else
  if [[ "$CURRENT_VERSION" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    NEW_VERSION="v${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.$((BASH_REMATCH[3] + 1))"
  fi
fi

if [[ ! "${NEW_VERSION:-}" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  echo "release: expected V=vX.Y.Z, got ${NEW_VERSION:-<empty>}" >&2
  exit 1
fi
if git show-ref --verify --quiet "refs/tags/$NEW_VERSION"; then
  echo "release: git tag already exists: $NEW_VERSION" >&2
  exit 1
fi

write_version "$ENV_FILE"
write_version "$TRACKED_ENV_FILE"

git add -- .env.example
if ! git diff --cached --quiet -- .env.example; then
  git commit -m "chore: release $NEW_VERSION" -- .env.example
fi
git tag -a "$NEW_VERSION" -m "release $NEW_VERSION"
echo "release: version=$NEW_VERSION committed and tagged"
