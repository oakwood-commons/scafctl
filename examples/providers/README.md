# Provider Examples

Example input files for use with `scafctl run provider --input @<file>`.

## Files

| File | Provider | Description |
|------|----------|-------------|
| [static-hello.yaml](static-hello.yaml) | `static` | Simple static string value |
| [http-get.yaml](http-get.yaml) | `http` | HTTP GET request |
| [github-api.yaml](github-api.yaml) | `http` | GitHub API call with authentication |
| [exec-ls.yaml](exec-ls.yaml) | `exec` | Execute `ls -la` command |
| [hcl-parse-variables.yaml](hcl-parse-variables.yaml) | `hcl` | Parse HCL content to extract variable definitions |
| [hcl-format.yaml](hcl-format.yaml) | `hcl` | Format HCL content to canonical style |
| [hcl-validate.yaml](hcl-validate.yaml) | `hcl` | Validate HCL syntax and return diagnostics |
| [hcl-generate.yaml](hcl-generate.yaml) | `hcl` | Generate HCL from structured block data |
| [hcl-generate-json.yaml](hcl-generate-json.yaml) | `hcl` | Generate Terraform JSON (`.tf.json`) from structured block data |
| [identity-claims.yaml](identity-claims.yaml) | `identity` | Get identity claims from stored metadata |
| [identity-status.yaml](identity-status.yaml) | `identity` | Check authentication status |
| [identity-scoped-claims.yaml](identity-scoped-claims.yaml) | `identity` | Get claims from a scoped access token JWT |
| [identity-scoped-status.yaml](identity-scoped-status.yaml) | `identity` | Check scoped token status and metadata |
| [identity-groups.yaml](identity-groups.yaml) | `identity` | Get Entra group memberships |
| [identity-list.yaml](identity-list.yaml) | `identity` | List available auth handlers |

## Usage

```bash
# Run with a file-based input
scafctl run provider static --input @examples/providers/static-hello.yaml

# Run with inline inputs
scafctl run provider static --input value=hello

# Combine file and inline inputs (inline takes precedence)
scafctl run provider http --input @examples/providers/http-get.yaml --input method=POST
```
