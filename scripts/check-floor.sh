#!/usr/bin/env bash
# The Go floor is a reviewed promise, not a side effect of a dependency bump.
# Renovate may raise the go directive; this check refuses the change until
# go.floor is raised in the same commit and the README says why.
set -euo pipefail

cd "$(dirname "$0")/.."

floor=$(tr -d '[:space:]' < go.floor)
directive=$(awk '$1 == "go" { print $2; exit }' go.mod)

case "$directive" in
  "$floor" | "$floor".*) ;;
  *)
    echo "go.mod requires go $directive, below or past the floor $floor" >&2
    echo "raise go.floor deliberately and explain the new floor in the README" >&2
    exit 1
    ;;
esac

echo "go floor ok: go.mod $directive starts with $floor"
