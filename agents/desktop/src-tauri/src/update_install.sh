#!/bin/bash
# Arguments are passed directly by Rust, never interpolated into shell source.
set -eu
parent=$1
target=$2
stage=$3
work=$4
backup="${stage%.app}.previous.app"

# Give the running Agent time to close its files. Never replace a live process.
for ((attempt=0; attempt<60; attempt++)); do
  if ! kill -0 "$parent" 2>/dev/null; then break; fi
  sleep 1
done
if kill -0 "$parent" 2>/dev/null; then
  rm -rf "$stage"
  exit 1
fi

if ! mv "$target" "$backup"; then
  rm -rf "$stage"
  /usr/bin/open "$target"
  exit 1
fi
if ! mv "$stage" "$target"; then
  mv "$backup" "$target"
  /usr/bin/open "$target"
  exit 1
fi
if ! /usr/bin/open "$target"; then
  rm -rf "$target"
  mv "$backup" "$target"
  /usr/bin/open "$target"
  exit 1
fi
rm -rf "$backup" "$work"
