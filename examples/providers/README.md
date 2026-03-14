# Provider Examples

Full solution examples demonstrating each built-in provider. Every file is a valid, self-contained solution that can be run directly.

## Usage

```bash
# Run any example as a solution
scafctl run solution -f examples/providers/static-hello.yaml

# Run resolvers only (inspect resolved values)
scafctl run resolver -f examples/providers/http-get.yaml
```

## Static Provider

| File | Description |
|------|-------------|
| [static-hello.yaml](static-hello.yaml) | Simple static string value |

## Exec Provider

| File | Description |
|------|-------------|
| [exec-ls.yaml](exec-ls.yaml) | Execute `ls -la` command |

## HTTP Provider

| File | Description |
|------|-------------|
| [http-get.yaml](http-get.yaml) | Simple HTTP GET request |
| [http-autoparse.yaml](http-autoparse.yaml) | Auto-parse JSON responses |
| [http-poll.yaml](http-poll.yaml) | Polling until a condition is met |
| [http-entra.yaml](http-entra.yaml) | Microsoft Graph API with Entra auth |
| [github-api.yaml](github-api.yaml) | GitHub API with authentication |
| [http-pagination-cursor.yaml](http-pagination-cursor.yaml) | Cursor-based pagination |
| [http-pagination-link-header.yaml](http-pagination-link-header.yaml) | Link header pagination (RFC 8288) |
| [http-pagination-odata.yaml](http-pagination-odata.yaml) | OData / Microsoft Graph pagination |
| [http-pagination-offset.yaml](http-pagination-offset.yaml) | Offset-based pagination |
| [http-pagination-page-number.yaml](http-pagination-page-number.yaml) | Page number pagination |

## GitHub Provider

| File | Description |
|------|-------------|
| [github-provider.yaml](github-provider.yaml) | Read repository info via GraphQL |
| [github-write-operations.yaml](github-write-operations.yaml) | Write operations reference (issues, PRs, commits, releases) |

## HCL Provider

| File | Description |
|------|-------------|
| [hcl-format.yaml](hcl-format.yaml) | Format HCL content to canonical style |
| [hcl-validate.yaml](hcl-validate.yaml) | Validate HCL syntax and return diagnostics |
| [hcl-parse-variables.yaml](hcl-parse-variables.yaml) | Parse HCL content to extract variable definitions |
| [hcl-generate.yaml](hcl-generate.yaml) | Generate HCL from structured block data |
| [hcl-generate-json.yaml](hcl-generate-json.yaml) | Generate Terraform JSON (`.tf.json`) from structured block data |

## Identity Provider

| File | Description |
|------|-------------|
| [identity-list.yaml](identity-list.yaml) | List available auth handlers |
| [identity-claims.yaml](identity-claims.yaml) | Get claims from stored metadata |
| [identity-status.yaml](identity-status.yaml) | Check authentication status |
| [identity-groups.yaml](identity-groups.yaml) | Get Entra group memberships |
| [identity-scoped-claims.yaml](identity-scoped-claims.yaml) | Get claims from a scoped access token JWT |
| [identity-scoped-status.yaml](identity-scoped-status.yaml) | Check scoped token status and metadata |

## Metadata Provider

| File | Description |
|------|-------------|
| [metadata-full.yaml](metadata-full.yaml) | Full runtime metadata (version, args, cwd, solution) |
| [metadata-single-field.yaml](metadata-single-field.yaml) | Extract individual fields with CEL |

## Security

| File | Description |
|------|-------------|
| [security-example.yaml](security-example.yaml) | Security hardening patterns across providers |
