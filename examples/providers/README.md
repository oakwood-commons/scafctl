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

## Usage

```bash
# Run with a file-based input
scafctl run provider static --input @examples/providers/static-hello.yaml

# Run with inline inputs
scafctl run provider static --input value=hello

# Combine file and inline inputs (inline takes precedence)
scafctl run provider http --input @examples/providers/http-get.yaml --input method=POST
```
