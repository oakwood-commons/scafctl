#!/usr/bin/env bash
# PreToolUse hook: block git commit/push/amend unless user explicitly approves.
# Reads JSON from stdin, checks if the command is a git write operation.

set -euo pipefail

input=$(cat)

# Extract the command being run
cmd=$(echo "$input" | grep -o '"command"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"command"[[:space:]]*:[[:space:]]*"//;s/"$//')

# Check for git write operations
if echo "$cmd" | grep -qE 'git\s+(commit|push|amend|reset\s+--hard|rebase|force-push)'; then
  cat <<'EOF'
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "ask",
    "permissionDecisionReason": "Git write operation detected. This project requires explicit user approval before committing, pushing, or rewriting history."
  }
}
EOF
  exit 0
fi
