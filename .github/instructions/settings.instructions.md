---
description: "Settings and configuration conventions for scafctl. Binary name safety, xdg paths, env var prefixes, and timeout defaults. Use when editing settings or config packages."
applyTo: "{pkg/settings,pkg/config}/**/*.go"
---

# Settings & Configuration

## Binary Name Safety

scafctl is embedded by external CLIs -- never assume the binary is called `"scafctl"`.

- Use `settings.CliBinaryName` as the fallback constant
- Use `settings.SanitizeBinaryName()` to normalize raw binary names
- Use `settings.SafeEnvPrefix()` for environment variable prefixes -- never hardcode `SCAFCTL_`
- Functions producing paths, cache keys, or display strings must accept a binary name parameter or read from `settings.Run.BinaryName` in context
- Guard against empty binary names by falling back to `CliBinaryName`

## XDG Paths

Use `xdg` package paths for all file locations:
- Config: `xdg.ConfigHome` / binary name
- Cache: `xdg.CacheHome` / binary name
- Data: `xdg.DataHome` / binary name

## Constants

- Define timeout defaults as named constants (e.g., `DefaultResolverTimeout`, `DefaultHTTPTimeout`)
- Define conflict strategy defaults as constants
- No magic values -- all defaults must be exported constants with doc comments

## Configuration (`pkg/config/`)

- Use `config.Config` for runtime application configuration
- Configuration loading must support layered overrides (file, env, flags)
- Always validate configuration after loading
