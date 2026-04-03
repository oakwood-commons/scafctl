#!/usr/bin/env bash
# PostToolUse hook: auto-format .go files after edits
# Reads JSON from stdin, checks if a .go file was edited, runs goimports.

set -euo pipefail

input=$(cat)

# Extract the file path from the tool input (handles replace_string_in_file, create_file, etc.)
file=$(echo "$input" | grep -o '"filePath"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"filePath"[[:space:]]*:[[:space:]]*"//;s/"$//' || true)

# Only proceed for .go files
if [[ -z "$file" || "$file" != *.go ]]; then
  exit 0
fi

# Only format if the file exists
if [[ ! -f "$file" ]]; then
  exit 0
fi

# Run goimports if available, fall back to gofmt
if command -v goimports &>/dev/null; then
  goimports -w "$file"
elif command -v gofmt &>/dev/null; then
  gofmt -w "$file"
fi
