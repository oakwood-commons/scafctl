# <% .name %> - AI Agent Instructions

## Overview

<% if eq .plugin_type "auth-handler" -%>
scafctl auth handler plugin implementing the **<% .provider_name %>** handler.
<%- else -%>
scafctl provider plugin implementing the **<% .provider_name %>** provider.
<%- end %>

## Key Patterns

- **Plugin SDK**: Uses `github.com/oakwood-commons/scafctl-plugin-sdk`
- **Entry point**: `cmd/<% .name %>/main.go` calls `sdkplugin.<% if eq .plugin_type "auth-handler" %>ServeAuthHandler<% else %>Serve<% end %>()`
<% if eq .plugin_type "auth-handler" -%>
- **Handler impl**: `internal/*/auth_handler.go` implements AuthHandlerPlugin interface
<%- else -%>
- **Provider impl**: `internal/*/provider.go` implements ProviderPlugin interface
<%- end %>

## Conventions

- **Commits**: Use [conventional commits](https://www.conventionalcommits.org/en/v1.0.0/)
- **Signing**: All commits must be GPG/SSH signed (`-S`) and include DCO sign-off (`-s`)
- **Errors**: Return errors with `fmt.Errorf("context: %w", err)`, don't panic

## Build & Test Commands

~~~bash
task build    # Build the plugin binary
task test     # Run tests
task lint     # Run linter
task lint:fix # Run linter with auto-fix
~~~

## Critical Rules

- **No hardcoded paths**: Use the SDK interfaces for all host interactions
- **Test coverage**: Every new file must have tests. Target 70%+ patch coverage
- **Git safety**: Never run git commit/push unless explicitly asked
