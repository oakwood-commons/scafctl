# kubectl / oc Exec Credential Plugin

Use scafctl as a Kubernetes [client-go credential plugin][k8s-exec] so `kubectl`
and `oc` reuse the same scafctl-managed identity you already logged in with. No
separate cluster token to mint or rotate -- scafctl returns a fresh access token
on demand and tells kubectl when it expires.

## How It Works

When `kubectl` needs a credential it executes the configured command and reads a
JSON `ExecCredential` from stdout. `scafctl auth token <handler>
--exec-credential` emits exactly that envelope:

```json
{
  "apiVersion": "client.authentication.k8s.io/v1",
  "kind": "ExecCredential",
  "status": {
    "token": "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiI...",
    "expirationTimestamp": "2026-02-04T16:30:00Z"
  }
}
```

kubectl caches the token until `expirationTimestamp`, then re-invokes scafctl.

## Prerequisites

Log in once with the handler your cluster trusts (Entra shown here):

```bash
scafctl auth login entra
```

## Configure kubeconfig

Add an `exec` user to your kubeconfig and bind it to the cluster context:

```yaml
apiVersion: v1
kind: Config
clusters:
  - name: my-cluster
    cluster:
      server: https://my-cluster.example.com:6443
contexts:
  - name: my-context
    context:
      cluster: my-cluster
      user: my-cluster-user
current-context: my-context
users:
  - name: my-cluster-user
    user:
      exec:
        apiVersion: client.authentication.k8s.io/v1
        command: scafctl
        args:
          - auth
          - token
          - entra
          - --scope
          - "<cluster-scope>/.default"
          - --exec-credential
        # Forwards KUBERNETES_EXEC_INFO so scafctl echoes the requested apiVersion.
        provideClusterInfo: true
        interactiveMode: IfAvailable
```

Replace `<cluster-scope>` with the audience/scope your cluster's OIDC config
expects (for example an Entra application ID URI).

## Auto-Detection

You can also drop `--exec-credential` from the args. When kubectl invokes the
plugin it sets the `KUBERNETES_EXEC_INFO` environment variable; scafctl detects
it and emits an `ExecCredential` automatically -- as long as no other render
flag (`--raw`, `--curl`, `--export`) or explicit `-o` format is requested.

scafctl echoes back whichever `apiVersion` kubectl sends, so both
`client.authentication.k8s.io/v1` and `client.authentication.k8s.io/v1beta1`
clients work without changes.

## Verify

Force a manual run to confirm the JSON envelope before pointing kubectl at it:

```bash
KUBERNETES_EXEC_INFO='{"apiVersion":"client.authentication.k8s.io/v1"}' \
  scafctl auth token entra --scope "<cluster-scope>/.default"
```

Then exercise it through kubectl:

```bash
kubectl --context my-context get pods
```

[k8s-exec]: https://kubernetes.io/docs/reference/access-authn-authz/authentication/#client-go-credential-plugins
