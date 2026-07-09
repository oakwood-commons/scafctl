# scafctl kube login / kube logout for Kubernetes and OpenShift

`scafctl kube login` automates the [client-go credential plugin][k8s-exec] setup
described in [kubectl-exec-credential.md](kubectl-exec-credential.md). Instead of
hand-editing kubeconfig, it runs your auth handler's login and writes the cluster,
user (with an `exec` block), and context entries for you. `scafctl kube logout`
reverses it: it removes the kubeconfig entry and, unless `--keep-credentials` is
set, revokes the handler's cached credentials. With `--handler` it revokes that
handler; without it, the cluster's resolver-supplied default handler is revoked
on a best-effort basis (any resolution failure is ignored and only the
kubeconfig entry is removed).

## How It Works

`scafctl kube login` performs three steps:

1. Resolves the cluster's API server, auth method, and default auth handler (via
   the configured cluster resolver, explicit flags, or auto-detection).
2. Runs the resolved auth handler's interactive login.
3. Writes a kubeconfig user entry whose `exec` block invokes this binary as a
   credential plugin. Every later `kubectl` / `oc` call mints a fresh token on
   demand -- no static cluster token is stored.

When the kubeconfig provider plugin is unavailable, `scafctl kube login` falls
back to writing a minimal static kubeconfig directly, so the workflow still works
offline.

## Log In

Target a cluster known to your cluster resolver. When the resolver records a
default handler for the cluster, you do not need `--handler`:

```bash
scafctl kube login prod
```

Pass `--handler` to override the resolver's default:

```bash
scafctl kube login prod --handler oidc
```

The named handler is resolved by name against your configured catalogs, so a
third-party handler (for example `openshift`) published to a catalog works the
same as the official ones -- no allowlist entry required. Pin its artifact and
trust domains via `auth.handlers.<name>` (see
[third-party-handler-config.yaml](third-party-handler-config.yaml)). Every
subsequent `kubectl` / `oc` call mints tokens through the same handler via the
exec-credential helper.

Or point at an API server directly, without a resolver (name the handler since
there is no resolver default):

```bash
scafctl kube login --handler oidc \
  --server https://api.example.com:6443 \
  --cluster-name prod --context prod --user prod \
  --current
```

Common flags:

- `--handler`: the auth handler to authenticate with. Optional when the resolver
  supplies a default for the cluster.
- `--server`: API server URL when no resolver is configured.
- `--audience`: OIDC audience the minted token must target.
- `--profile`: auth profile baked into the exec args (for example `work`).
- `--current`: set the new context as the current context.
- `--verify`: confirm the authenticated identity via a post-login whoami
  (requires the kubeconfig provider).
- `--kubeconfig`: target kubeconfig file (defaults to `KUBECONFIG` or
  `~/.kube/config`).

## Use It

After login, `kubectl` reuses the scafctl-managed identity automatically:

```bash
kubectl --context prod get pods
```

## Configure Cluster Resolution

The stock binary resolves clusters by name from the `kube.clusters` config
section, so `kube login <cluster>` needs neither `--server` nor `--handler` for
known clusters. Resolution precedence is: explicit `--server`/`--handler` flags,
then a concrete URL argument, then a static alias, then the dynamic inventory.

Static aliases for one-off clusters not in any inventory:

```yaml
kube:
  clusters:
    aliases:
      lab:
        server: https://api.lab.example.com:6443
        defaultHandler: openshift
```

A fleet inventory that stamps the handler and auth type on every entry (reuses
the hostname inventory contract -- source, CEL transform, ttl):

```yaml
kube:
  clusters:
    resolver:
      source:
        url: https://clusters.example.com/
      transform: '_.map(k, {"name": k, "url": _[k].apiServerURL, "defaultHandler": "openshift", "authType": "oauth"})'
      ttl: 10m
```

With either configured, all of these work:

```bash
scafctl kube login lab                      # static alias
scafctl kube login pd1020                    # from inventory
scafctl kube login https://api.x:6443 --handler oidc   # direct URL, no config
```

Embedders can still supply a `RootOptions.ClusterResolver` implementation, which
takes precedence over the config-driven resolver.

## Log Out

Remove the kubeconfig entry and revoke the handler's cached credentials:

```bash
scafctl kube logout prod --handler oidc
```

Keep the cached credentials (for example to stay logged in for other clusters)
while removing only the kubeconfig entry:

```bash
scafctl kube logout prod --keep-credentials
```

## Scripting

Both commands support structured output for automation:

```bash
scafctl kube login prod --handler oidc -o json
scafctl kube logout prod -o json
```

## Limitations

For OpenShift-style OAuth (implicit-grant) clusters, the kubeconfig `exec` block
mints credentials from the handler's cached token. If that token expires and the
handler stored no refresh token, the credential plugin returns
not-authenticated rather than starting a fresh interactive login -- kubectl calls
then fail until you run `scafctl kube login` again. OIDC and other handlers that
refresh silently are unaffected.

[k8s-exec]: https://kubernetes.io/docs/reference/access-authn-authz/authentication/#client-go-credential-plugins
