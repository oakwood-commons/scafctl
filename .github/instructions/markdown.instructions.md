---
description: "Markdown formatting rules: tilde fences for nested backticks, ASCII-only characters. Use when writing or editing markdown."
applyTo: "**/*.md"
---

# Markdown Authoring Rules

## Code Blocks

When a markdown code block contains backticks (Go raw strings, heredocs, shell, template literals), use tilde fences instead of backtick fences to avoid delimiter collisions.

If tilde fences are not suitable, use 4+ backtick fences as the outer delimiter.

## Characters

Use only ASCII characters in markdown files:

- Use `--` instead of em dashes
- Use `---` for horizontal rules
- Use straight quotes (`"`, `'`) instead of curly/smart quotes
- Use `...` instead of ellipsis characters
- Use standard hyphens (`-`) instead of en dashes

## Publish Hygiene

This repo is public. Before committing any markdown file, check it against
`.github/skills/publish-hygiene/SKILL.md` for employer- or organization-specific
references (internal tool names, hostnames/domains, codenames, employee
identifiers, credentials).
