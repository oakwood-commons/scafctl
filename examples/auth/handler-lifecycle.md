# Auth Handler Lifecycle Management

Official auth handlers (github, gcp, entra) are delivered as plugin binaries.
They are downloaded automatically on first use, but you can manage them
explicitly for air-gapped environments, debugging, or disk cleanup.

Third-party auth handlers published to a configured catalog are resolved the
same way -- by name -- with no official-allowlist gate. See
[Third-Party Handlers](#third-party-handlers) below.

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

## Inspecting a Single Handler

Pass a handler name to show its details (name, display name, source, status,
login state, supported flows, and capabilities):

~~~bash
# Show details for one handler
scafctl auth handlers github

# Machine-readable output
scafctl auth handlers github -o json
scafctl auth handlers github -o yaml
~~~

Handlers that are available but not yet installed print an install hint instead.

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

## Third-Party Handlers

A non-official auth handler published to a configured catalog resolves by name,
just like the official trio -- there is no allowlist to add it to.

~~~bash
# Publish a third-party handler into a catalog
scafctl package plugin --name openshift --kind auth-handler --version 0.1.0 \
  --platform darwin/arm64=./dist/scafctl-plugin-auth-openshift

# It is now resolvable by name
scafctl catalog list --kind auth-handler   # shows: openshift
scafctl auth handlers install openshift
scafctl auth login openshift
scafctl kube login prod --handler openshift
~~~

To pin the exact artifact and declare trust (third-party handlers do not inherit
the official device-code verification domains), use the per-handler config
namespace -- see [third-party-handler-config.yaml](third-party-handler-config.yaml):

~~~yaml
auth:
  handlers:
    openshift:
      plugin:
        ref: openshift-auth
        version: "^0.1.0"
      trustedVerificationDomains:
        - sso.openshift.example.com
~~~

Locked-down deployments can enforce an official-only policy with
`settings.disableThirdPartyAuthHandlers: true`, which rejects any non-official
handler name even if present in a catalog.

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
