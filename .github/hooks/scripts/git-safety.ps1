#!/usr/bin/env pwsh
# PreToolUse hook: block git commit/push/amend unless user explicitly approves.
# Reads JSON from stdin, checks if the command is a git write operation.

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$inputText = [Console]::In.ReadToEnd()
if ([string]::IsNullOrWhiteSpace($inputText)) {
    exit 0
}

$match = [regex]::Match($inputText, '"command"\s*:\s*"([^"]+)"')
if (-not $match.Success) {
    exit 0
}

$cmd = $match.Groups[1].Value
if ([string]::IsNullOrWhiteSpace($cmd)) {
    exit 0
}

if ($cmd -match 'git\s+(commit|push|amend|reset\s+--hard|rebase|force-push)') {
    $out = @{
        hookSpecificOutput = @{
            hookEventName = "PreToolUse"
            permissionDecision = "ask"
            permissionDecisionReason = "Git write operation detected. This project requires explicit user approval before committing, pushing, or rewriting history."
        }
    }
    $out | ConvertTo-Json -Depth 4
}

exit 0