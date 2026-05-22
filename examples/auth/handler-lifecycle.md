# Auth Handler Lifecycle Management

Official auth handlers (github, gcp, entra) are delivered as plugin binaries.
They are downloaded automatically on first use, but you can manage them
explicitly for air-gapped environments, debugging, or disk cleanup.

## Listing Handlers

~~~bash
# Show all registered handlers with status, flows, and source
scafctl auth handlers

# Machine-readable output
scafctl auth handlers -o json
scafctl auth handlers -o yaml
~~~

Example table output:

~~~text
NAME     DISPLAY NAME   STATUS      SOURCE    FLOWS                 LOGGED IN
entra    Microsoft      installed   official  interactive,device    true
github   GitHub         installed   official  interactive,device    true
gcp      Google Cloud   available   official  interactive           false
gitlab   GitLab         installed   custom    interactive           true
~~~

## Pre-Installing a Handler

Download a handler plugin before first use. Useful for:
- Air-gapped environments (download once, deploy everywhere)
- Inspecting handler capabilities before logging in
- Ensuring a specific handler version is cached

~~~bash
# Install a handler
scafctl auth handlers install github
scafctl auth handlers install entra

# Force re-download (replaces cached binary)
scafctl auth handlers install entra --force
~~~

## Removing a Handler

Remove cached handler plugin binaries to free disk space or force a fresh
download on next use:

~~~bash
# Remove a handler's cached binary
scafctl auth handlers remove github
scafctl auth handlers remove entra
~~~

After removal, the handler will be re-downloaded automatically on next
`scafctl auth login <handler>`.

## Lazy Loading

Auth handlers use lazy loading -- plugin subprocesses are started only when an
operation requires I/O (login, token refresh, listing cached tokens). This
means:

- `scafctl auth handlers` lists registered handler names without network I/O
- `scafctl auth list` queries all eagerly-registered handlers (initializing
  them as needed) but does not trigger auto-download of unconfigured handlers
- `scafctl auth login <handler>` triggers auto-download if not cached

The optimization avoids surprise network I/O and plugin downloads unless the
user explicitly requests a handler by name.

## Shell Completion

The `login` and `logout` commands support shell completion for handler names:

~~~bash
# Bash
scafctl auth login <Tab>
# Suggests: entra  gcp  github  gitlab

scafctl auth logout <Tab>
# Suggests: entra  github  gitlab
~~~

## Workflow: Air-Gapped Setup

~~~bash
# On a machine with internet access:
scafctl auth handlers install github
scafctl auth handlers install entra

# Copy the plugin cache directory to the air-gapped machine:
# Default location: ~/.cache/scafctl/plugins/
tar czf auth-plugins.tar.gz ~/.cache/scafctl/plugins/

# On the air-gapped machine:
tar xzf auth-plugins.tar.gz -C ~/

# Verify handlers are available:
scafctl auth handlers
scafctl auth diagnose
~~~

## Related

- [Auth Tutorial](../../docs/tutorials/auth-tutorial.md) -- full auth walkthrough
- [Diagnose Workflow](diagnose-workflow.md) -- troubleshooting auth issues
- [Auth README](README.md) -- overview of auth examples
