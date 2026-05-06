#!/usr/bin/env pwsh
# PostToolUse hook: auto-format .go files after edits.
# Reads JSON from stdin, checks if a .go file was edited, runs goimports/gofmt.

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$inputText = [Console]::In.ReadToEnd()
if ([string]::IsNullOrWhiteSpace($inputText)) {
    exit 0
}

$match = [regex]::Match($inputText, '"filePath"\s*:\s*"([^"]+)"')
if (-not $match.Success) {
    exit 0
}

$file = $match.Groups[1].Value
if (-not $file.EndsWith('.go', [System.StringComparison]::OrdinalIgnoreCase)) {
    exit 0
}

if (-not (Test-Path -LiteralPath $file -PathType Leaf)) {
    exit 0
}

$goimports = Get-Command -Name goimports -ErrorAction SilentlyContinue
if ($null -ne $goimports) {
    & $goimports.Source -w $file
    exit $LASTEXITCODE
}

$gofmt = Get-Command -Name gofmt -ErrorAction SilentlyContinue
if ($null -ne $gofmt) {
    & $gofmt.Source -w $file
    exit $LASTEXITCODE
}

exit 0