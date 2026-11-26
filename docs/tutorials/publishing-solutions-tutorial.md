---
title: "Authoring & Publishing Catalog Solutions"
weight: 62
---

# Authoring & Publishing Catalog Solutions

This guide walks end to end through designing, bundling, testing, publishing, signing, and running a reusable catalog solution -- from a clean `sln-<name>` repository to zero-clone execution with `scafctl run solution <name>`.

```mermaid
flowchart LR
    A["Author (sln-service-scaffold)"] --> B["Verify (validate --strict + test)"]
    B --> C["Package (package solution + solution.lock)"]
    C --> D["Publish (catalog push + SPDX SBOM)"]
    D --> E["Sign (Cosign Keyless OIDC)"]
    E --> F["Consume (run solution service-scaffold)"]
```

## Prerequisites

- `scafctl` installed and available on your `PATH`
- Familiarity with resolvers, actions, and local catalogs ([Catalog Tutorial](catalog-tutorial.md))
- Access to an OCI-compliant registry such as GitHub Container Registry (`ghcr.io`) for remote publishing

---

## Choosing an Archetype: `actions.yaml` vs. `solution.yaml`

`scafctl` uses the same `apiVersion: scafctl.io/v1` / `kind: Solution` schema for two distinct workflows. Picking the right archetype up front keeps repository task runners lightweight and catalog packages self-contained:

| Dimension | `actions.yaml` (Repo Task Runner) | `solution.yaml` (Distributable Catalog Package) |
|-----------|-----------------------------------|-------------------------------------------------|
| **Primary goal** | Standardize repo-local `build`, `test`, `lint`, and `release` tasks | Package reusable scaffolding or automation for other teams and repos |
| **Repository layout** | Lives at the root of an application or library repo | Dedicated `sln-<name>` repo (or self-contained subdirectory) |
| **Auto-discovery** | Discovered first by `scafctl run action <name>` | Discovered by `run solution`, `validate`, `lint`, `test`, `package` |
| **Action `explicit` policy** | Tasks are invoked selectively (`scafctl run action test`); all actions may set `explicit: true` | **At least one default workflow action omits `explicit: true`** (`explicit: false`) so `scafctl run solution <name>` does real work; auxiliary tasks (`clean`, `teardown`) set `explicit: true` |
| **Bundling & lockfile** | Not packaged; reads the working tree directly | Declares `bundle.include` + `bundle.plugins` and commits `solution.lock` |
| **Consumer discovery** | `scafctl run action --list` | `metadata.usage` + `scafctl inspect solution <name> --usage` |
| **Distribution** | Checked out via `git clone` | Published to `ghcr.io/<org>/solutions/<name>:<version>` and run with zero clone |

> [!IMPORTANT]
> **Avoid `no-default-action` in catalog solutions.** When a consumer runs `scafctl run solution <name>` without `-a`/`--action`, `scafctl` executes every workflow action where `explicit` is `false` (the default) plus its dependencies. If *every* action in a `solution.yaml` sets `explicit: true`, a bare `run solution` executes zero actions -- `scafctl lint` flags this with the `no-default-action` warning (fatal under `scafctl validate --strict`).

---

## Walkthrough: Build and Publish `service-scaffold`

In the six steps below, you will create `sln-service-scaffold`, verify it locally with strict linting and functional tests, package it into an OCI bundle, push it with an automatic SPDX SBOM referrer, sign it with keyless Cosign OIDC in GitHub Actions, and run it from a consumer workspace without cloning the repository.

{{% steps %}}
1. **Step 1: Repository Layout & Bundle Boundaries (`sln-service-scaffold`)**

   By convention, each distributable solution lives in a repository named `sln-<name>` (for example, `github.com/my-org/sln-service-scaffold`), which publishes to `ghcr.io/my-org/solutions/<name>`.

   Create the project directory, environment configs, and a Go template:

   ~~~bash
   mkdir -p sln-service-scaffold/configs sln-service-scaffold/templates
   cd sln-service-scaffold
   ~~~

   Create `configs/dev.yaml`:

   ~~~yaml
   replicas: 1
   port: 8080
   ~~~

   Create `configs/prod.yaml`:

   ~~~yaml
   replicas: 3
   port: 8443
   ~~~

   Create `templates/service.yaml.tmpl`:

   ~~~yaml
   apiVersion: apps/v1
   kind: Deployment
   metadata:
     name: {{ .serviceName }}
     labels:
       app: {{ .serviceName }}
       environment: {{ .environment }}
   spec:
     replicas: {{ .replicas }}
     selector:
       matchLabels:
         app: {{ .serviceName }}
     template:
       metadata:
         labels:
           app: {{ .serviceName }}
       spec:
         containers:
           - name: {{ .serviceName }}
             image: ghcr.io/my-org/{{ .serviceName }}:latest
             ports:
               - containerPort: {{ .port }}
   ~~~

   ### Bundle Root Boundary & Plugin Locking Rules

   - **Bundle root is the directory containing `solution.yaml`**: Every static file reference, `bundle.include` glob, `compose` partial, and symlink target **must** resolve inside this directory. Parent-directory traversals (`../shared/common.yaml`) and symlinks escaping the bundle root are rejected at build and extraction time (`path escapes bundle root`).
   - **Static vs. dynamic paths & test sandboxes**: Literal paths such as `templates/service.yaml.tmpl` are discovered automatically during `package solution`, while dynamic paths computed at runtime (such as `'configs/' + _.environment + '.yaml'`) **must** be covered by `bundle.include`. Because `scafctl test` populates its isolated test sandbox from `bundle.include` (plus any per-test `files:`), listing both `"configs/*.yaml"` and `"templates/**/*.tmpl"` in `bundle.include` ensures `builtin:resolve-defaults` and `spec.testing.cases` stage every template and config file automatically (`package solution` deduplicates overlapping entries).
   - **External plugins & `solution.lock`**: If your solution uses external or official catalog plugins (such as `exec`, `directory`, `git`, or `hcl`), declare them in `bundle.plugins`:

     ~~~yaml
     bundle:
       include:
         - "configs/*.yaml"
         - "templates/**/*.tmpl"
       plugins:
         - name: exec
           kind: provider
           version: "^1.0.0"
     ~~~

     Running `scafctl package solution` resolves matching plugin versions and multi-platform SHA-256 digests into `solution.lock`. Commit `solution.lock` to Git for reproducible builds, and run `scafctl vendor update` whenever you want to upgrade locked dependencies. Never list built-in providers (`cel`, `file`, `go-template`, `http`, `message`, `parameter`, `solution`, `state`, `static`, `validation`) in `bundle.plugins` -- `scafctl lint` flags them with `builtin-in-bundle-plugins`.

2. **Step 2: Author `solution.yaml` with Default Workflow, Opt-in Actions, Usage, and Tests**

   Create `solution.yaml` in `sln-service-scaffold/` combining typed parameters, dynamic config loading, a default `explicit: false` scaffold workflow, an opt-in `explicit: true` `clean` action, consumer `metadata.usage` docs, and embedded functional tests:

   ~~~yaml
   apiVersion: scafctl.io/v1
   kind: Solution
   metadata:
     name: service-scaffold
     version: 1.0.0
     displayName: Service Manifest Scaffold
     description: Render a Kubernetes Deployment manifest for dev or prod environments
     category: scaffolding
     tags:
       - kubernetes
       - scaffolding
     usage:
       synopsis: Generate a Kubernetes Deployment manifest for a service and target environment.
       details: |
         Renders templates/service.yaml.tmpl using environment-specific sizing from
         configs/<environment>.yaml and writes the resulting manifest to outputPath.
       examples:
         - description: Generate a dev manifest using defaults
           command: scafctl run solution service-scaffold
         - description: Generate a production manifest for billing-api
           command: scafctl run solution service-scaffold serviceName=billing-api environment=prod
         - description: Remove the generated manifest file
           command: scafctl run solution service-scaffold -a clean

   bundle:
     include:
       - "configs/*.yaml"
       - "templates/**/*.tmpl"

   spec:
     resolvers:
       serviceName:
         description: Lowercase kebab-case service name
         type: string
         example: billing-api
         resolve:
           with:
             - provider: parameter
               inputs:
                 key: serviceName
                 default: sample-service
         validate:
           with:
             - provider: validation
               inputs:
                 expression: "__self.matches('^[a-z][a-z0-9-]{1,38}[a-z0-9]$')"
                 message: serviceName must be lowercase kebab-case (3-40 chars)

       environment:
         description: Target deployment environment (dev or prod)
         type: string
         example: dev
         resolve:
           with:
             - provider: parameter
               inputs:
                 key: environment
                 default: dev
         validate:
           with:
             - provider: validation
               inputs:
                 expression: "__self in ['dev', 'prod']"
                 message: environment must be either dev or prod

       outputPath:
         description: Destination path for the rendered manifest
         type: string
         example: ./generated/service.yaml
         resolve:
           with:
             - provider: parameter
               inputs:
                 key: outputPath
                 default: ./generated/service.yaml

       envConfig:
         description: Parsed environment sizing configuration
         type: any
         resolve:
           with:
             - provider: file
               inputs:
                 operation: read
                 path:
                   expr: "'configs/' + _.environment + '.yaml'"
                 parse: yaml
         transform:
           with:
             - provider: cel
               inputs:
                 expression: "__self.object"

       renderedManifest:
         description: Rendered Kubernetes Deployment YAML
         type: string
         resolve:
           with:
             - provider: file
               inputs:
                 operation: read
                 path: templates/service.yaml.tmpl
         transform:
           with:
             - provider: cel
               inputs:
                 expression: "__self.content"
             - provider: go-template
               inputs:
                 name: service-manifest
                 template:
                   expr: "__self"
                 data:
                   expr: >
                     {
                       'serviceName': _.serviceName,
                       'environment': _.environment,
                       'replicas': _.envConfig.replicas,
                       'port': _.envConfig.port
                     }

     workflow:
       actions:
         write-manifest:
           description: Write the rendered Kubernetes manifest to outputPath
           explicit: false
           provider: file
           inputs:
             operation: write
             path:
               rslvr: outputPath
             content:
               rslvr: renderedManifest
             onConflict: overwrite

         print-summary:
           description: Print a confirmation summary after writing the manifest
           dependsOn:
             - write-manifest
           provider: message
           inputs:
             type: success
             message:
               tmpl: "Wrote {{ .serviceName }} ({{ .environment }}) manifest to {{ .outputPath }}"

         clean:
           description: Remove the generated manifest file
           explicit: true
           provider: file
           inputs:
             operation: delete
             path:
               rslvr: outputPath

     testing:
       cases:
         default-scaffold:
           description: Default workflow writes the dev manifest
           command: [run, solution]
           assertions:
             - expression: "__exitCode == 0"
             - contains: "Wrote sample-service (dev)"
         prod-resolvers:
           description: Prod environment resolves 3 replicas and port 8443
           command: [run, resolver]
           args: ["serviceName=billing-api", "environment=prod", "-o", "json"]
           assertions:
             - expression: "__exitCode == 0"
             - contains: "replicas: 3"
             - contains: "containerPort: 8443"
         clean-dry-run:
           description: Opt-in clean action previews deletion cleanly
           command: [run, solution]
           args: ["-a", "clean", "--dry-run"]
           assertions:
             - expression: "__exitCode == 0"
   ~~~

3. **Step 3: Verify Locally (Strict Gate, Tests, Usage View, and Dry-Run Bundle)**

   Before packaging, run the four pre-publish verification checks:

   ~~~bash
   # 1. Strict validation gate (schema + lint; warnings fail under --strict)
   scafctl validate -f ./solution.yaml --strict

   # 2. Functional test suite (runs builtin parse/lint/resolve-defaults + spec.testing.cases)
   scafctl test -f ./solution.yaml

   # 3. Consumer-facing usage projection
   scafctl inspect solution -f ./solution.yaml --usage

   # 4. Bundle discovery dry-run (confirms static + bundle.include coverage)
   scafctl package solution -f ./solution.yaml --dry-run
   ~~~

   > [!TIP]
   > **AI Agent / MCP Verification**: When authoring inside an editor connected to `scafctl mcp serve` (configured via `scafctl mcp init`), your agent can run the exact same checks without leaving chat via `lint_solution`, `run_solution_tests`, `get_solution_usage`, and `package_solution` (which defaults to `dry_run: true`).

4. **Step 4: Package into the Local Catalog & Push with SPDX SBOM**

   Package the solution into your local OCI catalog and emit a structured JSON build report to capture the content digest:

   ~~~bash
   scafctl package solution -f ./solution.yaml -o json
   ~~~

   Authenticate to your OCI registry and push `service-scaffold@1.0.0` using `-c` / `--catalog`:

   ~~~bash
   # Bridge GitHub credentials to GHCR (no Docker daemon required)
   scafctl auth login github --registry ghcr.io

   # Push to ghcr.io/my-org/solutions/service-scaffold:1.0.0 (attaches SPDX 2.3 SBOM by default)
   scafctl catalog push service-scaffold@1.0.0 -c ghcr.io/my-org --latest
   ~~~

   Verify the remote artifact and its attached SPDX SBOM referrer:

   ~~~bash
   scafctl catalog inspect service-scaffold@1.0.0 -c ghcr.io/my-org
   scafctl catalog inspect service-scaffold@1.0.0 --referrers -c ghcr.io/my-org
   ~~~

5. **Step 5: Automate Releases & Keyless Cosign OIDC Signing in GitHub Actions**

   In production, publish and sign catalog solutions from a tag-triggered GitHub Actions workflow using Sigstore/Cosign keyless OIDC -- no long-lived signing keys required.

   ### Option A: Delegated Signing via Reusable `sign-plugin.yml`

   Call `oakwood-commons/scafctl/.github/workflows/sign-plugin.yml@sign-plugin/v1`, which signs both the primary solution manifest and its attached SPDX SBOM referrer automatically:

   ~~~yaml
   # .github/workflows/release.yml
   name: Release Solution

   on:
     push:
       tags:
         - "v*"

   permissions:
     contents: read
     packages: write
     id-token: write

   jobs:
     publish:
       runs-on: ubuntu-latest
       outputs:
         image-ref: ${{ steps.push.outputs.image_ref }}
       steps:
         - uses: actions/checkout@v4

         - name: Install scafctl
           run: |
             TAG=$(basename "$(curl -fsSLI -o /dev/null -w '%{url_effective}' https://github.com/oakwood-commons/scafctl/releases/latest)")
             curl -fsSL "https://github.com/oakwood-commons/scafctl/releases/download/${TAG}/scafctl_${TAG#v}_Linux_x86_64.tar.gz" \
               | sudo tar -xz -C /usr/local/bin scafctl

         - name: Validate, test, package, and push
           id: push
           env:
             REGISTRY_USER: ${{ github.actor }}
             REGISTRY_TOKEN: ${{ secrets.GITHUB_TOKEN }}
             REPO_OWNER: ${{ github.repository_owner }}
           run: |
             VERSION="${GITHUB_REF_NAME#v}"
             scafctl validate -f ./solution.yaml --strict
             scafctl test -f ./solution.yaml
             echo "$REGISTRY_TOKEN" | scafctl catalog login ghcr.io --username "$REGISTRY_USER" --password @-
             scafctl package solution -f ./solution.yaml --version "$VERSION"
             scafctl catalog push "service-scaffold@${VERSION}" -c "ghcr.io/${REPO_OWNER}" --latest
             DIGEST=$(scafctl catalog inspect "service-scaffold@${VERSION}" -c "ghcr.io/${REPO_OWNER}" -o json | jq -r '.digest')
             echo "image_ref=ghcr.io/${REPO_OWNER}/solutions/service-scaffold@${DIGEST}" >> "$GITHUB_OUTPUT"

     sign:
       needs: publish
       permissions:
         packages: write
         id-token: write
       uses: oakwood-commons/scafctl/.github/workflows/sign-plugin.yml@sign-plugin/v1
       with:
         image-ref: ${{ needs.publish.outputs.image-ref }}
   ~~~

   ### Option B: Repository-Local Signing for Strict `sln-<name>` Identity Binding

   Fulcio populates the X.509 certificate URI Subject Alternative Name (SAN) from GitHub's `job_workflow_ref`. When you delegate signing to a shared reusable workflow, the URI SAN identifies `sign-plugin.yml` rather than the calling repository. To enforce **1-to-1 repository-to-solution isolation** so only `github.com/my-org/sln-service-scaffold` can sign `service-scaffold`, run `cosign sign` directly inside `sln-service-scaffold/.github/workflows/release.yml` (signing both the solution manifest and its attached SPDX SBOM referrer):

   ~~~yaml
         - name: Install Cosign
           uses: sigstore/cosign-installer@v4

         - name: Sign solution and SBOM referrers locally
           env:
             REGISTRY_USER: ${{ github.actor }}
             REGISTRY_TOKEN: ${{ secrets.GITHUB_TOKEN }}
             REPO_OWNER: ${{ github.repository_owner }}
             IMAGE_REF: ${{ steps.push.outputs.image_ref }}
           run: |
             VERSION="${GITHUB_REF_NAME#v}"
             echo "$REGISTRY_TOKEN" | cosign login ghcr.io -u "$REGISTRY_USER" --password-stdin
             cosign sign --yes --recursive "$IMAGE_REF"
             REPO="${IMAGE_REF%@*}"
             SBOM_DIGESTS=$(scafctl catalog inspect "service-scaffold@${VERSION}" \
               --referrers --artifact-type application/spdx+json \
               -c "ghcr.io/${REPO_OWNER}" -o json | jq -r '.[].digest')
             for ref_digest in $SBOM_DIGESTS; do
               cosign sign --yes "${REPO}@${ref_digest}"
             done
   ~~~

   ### Verifying Solution Signatures vs. Plugin Signature Policy

   > [!IMPORTANT]
   > **Scope of `plugins.signatures`**: The `plugins.signatures` block in `~/.config/scafctl/config.yaml` enforces automatic runtime Cosign verification for **plugin binaries** (`provider` and `auth-handler`, when `scafctl` is built with `-tags cosign`) -- it does **not** automatically verify catalog **solution** OCI artifacts during `catalog pull` or `run solution`.

   To verify a signed **solution** before executing it, resolve its manifest digest, run `cosign verify` against your expected OIDC issuer and workflow identity, and chain execution to verification with `&&` so the signature gate fails closed while `@sha256:...` digest pinning prevents tag substitution between verification and execution:

   ~~~bash
   # Resolve the immutable manifest digest, verify its keyless OIDC signature,
   # and execute the verified digest only when verification succeeds (&& fails closed).
   # (Option B repo-local identity shown; for Option A, match
   #  ^https://github\.com/oakwood-commons/scafctl/\.github/workflows/sign-plugin\.yml@refs/tags/sign-plugin/v[0-9]+$)
   DIGEST=$(scafctl catalog inspect service-scaffold@1.0.0 -c ghcr.io/my-org -o json | jq -er '.digest // empty') \
     && [ -n "$DIGEST" ] \
     && IMAGE_REF="ghcr.io/my-org/solutions/service-scaffold@${DIGEST}" \
     && cosign verify \
       --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
       --certificate-identity-regexp="^https://github\.com/my-org/sln-service-scaffold/\.github/workflows/.*$" \
       "$IMAGE_REF" \
     && scafctl run solution "$IMAGE_REF" serviceName=billing-api environment=prod
   ~~~

   If your solution also declares external **plugins** in `bundle.plugins`, consumers running a `cosign`-enabled `scafctl` build can enforce per-repository `{name}` / `{kind}` templates for those plugins in `~/.config/scafctl/config.yaml`:

   ~~~yaml
   plugins:
     signatures:
       mode: enforce
       trustedIssuers:
         - "https://token.actions.githubusercontent.com"
       trustedIdentities:
         # Enforces 1-to-1 repo binding for provider / auth-handler plugins:
         - "https://github.com/my-org/scafctl-plugin-{name}/.github/workflows/*"
         - "https://github.com/my-org/scafctl-plugin-auth-{name}/.github/workflows/*"
         # Add when trusting plugins signed via the shared reusable workflow:
         # - "https://github.com/oakwood-commons/scafctl/.github/workflows/sign-plugin.yml@refs/tags/sign-plugin/v*"
   ~~~

   See [OCI Artifact Signing & Verification](../design/artifact-signing.md) for full details on `{name}` and `{kind}` expansion rules.

6. **Step 6: Zero-Clone Consumer Execution**

   A consumer on another machine can register your organization's catalog once and discover, inspect, and execute `service-scaffold` from any empty working directory -- no `git clone` needed:

   ~~~bash
   # Register the organization catalog once (scafctl appends /solutions/<name> under --url)
   scafctl catalog remote add my-org \
     --type oci \
     --url oci://ghcr.io/my-org \
     --auth-provider github \
     --artifact-kind solution \
     --default

   # Discover parameters, actions, and curated examples before running
   scafctl inspect solution service-scaffold --usage

   # Run the default workflow in a fresh project directory with positional key=value params
   mkdir -p ~/work/billing-api && cd ~/work/billing-api
   scafctl run solution service-scaffold serviceName=billing-api environment=prod

   # Pin an exact version (@1.0.0), semver range (--version), or verified digest (@sha256:...)
   scafctl run solution service-scaffold@1.0.0 serviceName=billing-api environment=prod
   scafctl run solution service-scaffold --version "^1.0" serviceName=billing-api environment=prod
   # For signature-verified execution, chain 'cosign verify ... && scafctl run solution ...' as shown in Step 5

   # Invoke the opt-in explicit: true 'clean' action via -a or 'run action -s'
   scafctl run solution service-scaffold -a clean
   scafctl run action clean -s service-scaffold
   ~~~
{{% /steps %}}

---

## Pre-Publish Checklist

Before tagging a `sln-<name>` release, confirm every item below passes:

- `solution.yaml` defines `metadata.name`, `metadata.version` (semver), `metadata.description`, and `metadata.usage`
- At least one primary action in `spec.workflow.actions` keeps `explicit: false` (default) so `scafctl run solution <name>` runs the main workflow; destructive or auxiliary tasks set `explicit: true`
- Dynamic file paths are covered by `bundle.include`, external plugins are declared in `bundle.plugins` (no built-in providers), `solution.lock` is committed when plugins or vendored sub-solutions are present, and zero paths traverse `../` outside the bundle root
- `scafctl validate -f ./solution.yaml --strict` exits `0` with zero warnings
- `scafctl test -f ./solution.yaml` passes all `spec.testing.cases`
- `scafctl package solution -f ./solution.yaml --dry-run` confirms expected bundled file counts

## Related Guides

- [Catalog Tutorial](catalog-tutorial.md) -- Local catalog operations, tar archives, and `diff bundle`
- [Bundle Plugins & Built-in Providers](bundle-plugins-tutorial.md) -- Managing `bundle.plugins`, `solution.lock`, and `vendor update`
- [Plugin Lock Modes Tutorial](lock-modes-tutorial.md) -- `strict`, `constrained`, and `bestEffort` runtime lock modes
- [Functional Testing Tutorial](functional-testing.md) -- Authoring sandboxed `spec.testing.cases` and golden snapshots
- [OCI Artifact Signing & Verification](../design/artifact-signing.md) -- Keyless Cosign OIDC architecture and `trustedIdentities` policy
