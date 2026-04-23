#!/usr/bin/env bash
# push-catalog-index.sh
#
# Pushes the catalog-index artifact to the official catalog registry
# using scafctl's own ORAS-based Go tooling (no external tools needed).
#
# Prerequisites:
#   - Authenticated to ghcr.io (docker login, gh auth, or GITHUB_TOKEN)
#
# Usage:
#   ./scripts/push-catalog-index.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "Building and running catalog index push..."
cd "$REPO_ROOT"
go run ./cmd/scafctl/scafctl.go -- catalog index push
