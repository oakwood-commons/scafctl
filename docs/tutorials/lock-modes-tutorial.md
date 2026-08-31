---
title: "Plugin Lock Modes"
weight: 137
---

# Plugin Lock Modes

When a solution depends on **external plugin providers** (anything declared in
`bundle.plugins`), scafctl has to decide *which version* of each plugin to load
at run time. The **lock mode** controls that decision -- whether a lock file is
consulted, and whether the exact pinned version or the original constraint range
is used.

This tutorial covers the three lock modes, how the `--lock-mode` flag works, and
the source-based defaults that apply when you do not set one.

> **Note:** Lock modes only affect **external plugin providers** declared in
> `bundle.plugins`. Solutions that use only built-in providers (`cel`, `http`,
> `static`, ...) are unaffected -- there is nothing to pin.

## The Three Modes

| Mode | Requires a lock file? | Version used | Behavior |
| ------------- | :---: | ------------- | ---------- |
| `strict` | Yes | Exact pinned version from the lock | Fully deterministic. No network resolution -- the plugin is loaded at the version the lock recorded. |
| `constrained` | Yes | The lock entry's original constraint (range) | May fetch to resolve a concrete version that satisfies the constraint. |
| `bestEffort` | No | The lock entry's constraint (range) when present, otherwise the `bundle.plugins` constraint | Consults the lock opportunistically to refine the constraint, then resolves that range to a concrete version; falls back when the lock is absent or incomplete. |

### `strict`

Strict mode pins every external plugin to the exact version recorded in the lock
file and performs **no version resolution over the network**. This is the mode
you want for reproducible, auditable runs (CI/CD, releases).

Strict mode **requires a lock file**. If none is available, or if the lock is
out of sync with `bundle.plugins`, preparation fails:

~~~text
provider "exec": strict mode requires a lock file but none was provided
~~~

~~~text
provider "exec": no lock entry matches constraint "^1.0.0"; the solution lock is out of sync with bundle.plugins
~~~

Regenerate the lock with `scafctl package solution` to fix an out-of-sync lock.

### `constrained`

Constrained mode keeps the lock entry's original semver **constraint** (for
example `^1.5.0`) rather than the exact resolved version, then resolves a
concrete version satisfying that constraint at run time. Use it when you want the
lock to fix the *catalog* and *range* but still allow newer patch/minor releases
within the constraint. It also requires a lock file.

### `bestEffort`

Best-effort mode does **not** require a lock file. When a lock is present it is
consulted opportunistically -- a matching entry refines the version and catalog.
When the lock is absent or the entry is incomplete, resolution falls back to the
plugin's `bundle.plugins` constraint. This is the developer-iteration mode: it
never fails just because you have not packaged a lock yet.

## The `--lock-mode` Flag

Both `scafctl run` and `scafctl render` accept `--lock-mode`:

{{< tabs "lock-modes-flag" >}}
{{% tab "Bash" %}}

```bash
# Pin exact versions from the lock (deterministic)
scafctl run solution -f ./solution.yaml --lock-mode strict

# Resolve within the locked constraint range
scafctl run resolver -f ./solution.yaml --lock-mode constrained

# Use the lock when present, fall back when absent
scafctl render solution -f ./solution.yaml --lock-mode bestEffort
```

{{% /tab %}}
{{% tab "PowerShell" %}}

```powershell
# Pin exact versions from the lock (deterministic)
scafctl run solution -f ./solution.yaml --lock-mode strict

# Resolve within the locked constraint range
scafctl run resolver -f ./solution.yaml --lock-mode constrained

# Use the lock when present, fall back when absent
scafctl render solution -f ./solution.yaml --lock-mode bestEffort
```

{{% /tab %}}
{{< /tabs >}}

When you omit `--lock-mode`, scafctl picks a **source-based default** (below).

## Source-Based Defaults

If you do not pass `--lock-mode`, the default depends on where the solution came
from:

| Solution source | Default mode | Why |
| ----------------- | ------------- | ----- |
| Local file or stdin (`-f ./solution.yaml`, `-f -`) | `bestEffort` | Developer iteration -- lock hints are advisory, runs never fail for lack of a lock. |
| Catalog / remote artifact **with** an embedded lock layer | `strict` | The artifact carries pinned versions; fetch-by-digest makes the run reproducible. |
| Catalog / remote artifact **without** a lock layer | `bestEffort` (with a warning) | An older artifact predating the lock feature; plugins resolve unpinned. |

The warning emitted for a lock-less remote artifact looks like:

~~~text
solution has no embedded lock layer; external plugin versions are unpinned;
re-package to embed a lock if it uses external providers
~~~

> **API server:** the HTTP API defaults `lockMode` to `strict` when the request
> omits it (rather than using the source-based default). Pass an explicit
> `lockMode` of `strict`, `constrained`, or `bestEffort` in the request body to
> override.

## Producing a Lock File

Strict and constrained modes need a lock file. Generate one with:

{{< tabs "lock-modes-package" >}}
{{% tab "Bash" %}}

```bash
# Resolve every bundle.plugins entry and write a lock file with pinned
# versions, digests, and source catalogs
scafctl package solution -f ./solution.yaml
```

{{% /tab %}}
{{% tab "PowerShell" %}}

```powershell
# Resolve every bundle.plugins entry and write a lock file with pinned
# versions, digests, and source catalogs
scafctl package solution -f ./solution.yaml
```

{{% /tab %}}
{{< /tabs >}}

For catalog/remote solutions, the lock is embedded as a dedicated layer in the
packaged artifact, so a consumer that pulls the artifact gets strict pinning
automatically.

## Choosing a Mode

- **CI/CD and releases** -- use `strict` (explicitly, or rely on the strict
  default for packaged catalog artifacts). Commit the lock file to version
  control and review digest changes in diffs.
- **Controlled upgrades** -- use `constrained` when you want to stay within a
  locked range but pick up new patch/minor releases.
- **Local development** -- the `bestEffort` default is usually what you want; no
  lock file is required to iterate.

## Related

- [Plugin Auto-Fetching](plugin-auto-fetch-tutorial.md) -- how plugins are
  resolved, cached, and verified, and why external providers must be declared in
  `bundle.plugins`.
- [Security Hardening](security-hardening.md) -- supply-chain best practices,
  digest and signature verification.
