---
title: "HCL Provider Tutorial"
weight: 96
---

# HCL Provider Tutorial

This tutorial walks you through using the `hcl` provider to parse Terraform and OpenTofu configuration files. You'll learn how to extract variables, resources, modules, outputs, and other block types from HCL content.

## Prerequisites

- scafctl installed and available in your PATH
- Basic familiarity with YAML syntax and solution files
- Basic understanding of Terraform/OpenTofu configuration syntax

## Table of Contents

1. [Parsing Inline HCL Content](#parsing-inline-hcl-content)
2. [Parsing HCL Files](#parsing-hcl-files)
3. [Multi-File and Directory Support](#multi-file-and-directory-support)
4. [Extracting Variables](#extracting-variables)
5. [Extracting Resources](#extracting-resources)
6. [Working with Modules](#working-with-modules)
7. [Terraform Block Extraction](#terraform-block-extraction)
8. [Combining with CEL Expressions](#combining-with-cel-expressions)
9. [Transform Capability](#transform-capability)
10. [Expression Handling](#expression-handling)
11. [Common Patterns](#common-patterns)
12. [Formatting HCL Content](#formatting-hcl-content)
13. [Validating HCL Syntax](#validating-hcl-syntax)
14. [Generating HCL from Structured Data](#generating-hcl-from-structured-data)
15. [Generating Terraform JSON (`.tf.json`)](#generating-terraform-json-tfjson)

---

## Parsing Inline HCL Content

The simplest way to use the HCL provider is with inline content. Create a file called `parse-hcl.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parse-hcl-demo
  version: 1.0.0

spec:
  resolvers:
    config:
      resolve:
        with:
          - provider: hcl
            inputs:
              content: |
                variable "region" {
                  type        = string
                  default     = "us-east-1"
                  description = "AWS region"
                }
```

Run it:

{{< tabs "hcl-provider-tutorial-cmd-1" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f parse-hcl.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f parse-hcl.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

Expected output:

```json
{
  "config": {
    "check": [],
    "data": [],
    "import": [],
    "locals": {},
    "modules": [],
    "moved": [],
    "outputs": [],
    "providers": [],
    "resources": [],
    "terraform": {},
    "variables": [
      {
        "default": "us-east-1",
        "description": "AWS region",
        "name": "region",
        "type": "string"
      }
    ]
  }
}
```

All block types are represented in the output — empty arrays/maps for types not present in the input, and populated entries for those that are. The output key matches the resolver name (`config` in this case).

---

## Parsing HCL Files

Instead of inline content, you can point the provider at a `.tf` file:

```yaml
spec:
  resolvers:
    tfConfig:
      resolve:
        with:
          - provider: hcl
            inputs:
              path: ./main.tf
```

The `path` input reads the file from disk and parses it. You must provide either `content` or `path`, not both.

When using `path`, the `metadata.filename` in the output reflects the actual file path instead of the default `input.tf`.

---

## Multi-File and Directory Support

The HCL provider can process multiple files at once. This is useful for real Terraform projects that split configuration across many `.tf` files.

### Parse Multiple Files

Use the `paths` input to specify an array of files. Parse results are **merged** — arrays are concatenated and maps are merged:

```yaml
spec:
  resolvers:
    fullConfig:
      resolve:
        with:
          - provider: hcl
            inputs:
              paths:
                - ./main.tf
                - ./variables.tf
                - ./outputs.tf
```

The metadata includes `filenames` (array) and `files` (count) instead of a single `filename`.

### Parse a Directory

Use `dir` to process all `.tf` and `.tf.json` files in a directory (non-recursive):

```yaml
spec:
  resolvers:
    infraConfig:
      resolve:
        with:
          - provider: hcl
            inputs:
              dir: ./terraform/environments/prod
```

This automatically discovers all HCL files, reads them, and merges the parsed results.

### Multi-File Format and Validate

When `paths` or `dir` is used with `format` or `validate`, results are returned **per file** rather than merged:

```yaml
# Format all .tf files in a directory
spec:
  resolvers:
    formatted:
      resolve:
        with:
          - provider: hcl
            inputs:
              operation: format
              dir: ./terraform
```

Format output with multiple files:
```json
{
  "files": [
    { "filename": "./terraform/main.tf", "formatted": "...", "changed": true },
    { "filename": "./terraform/vars.tf", "formatted": "...", "changed": false }
  ],
  "changed": true
}
```

Validate output with multiple files:
```json
{
  "valid": false,
  "error_count": 2,
  "files": [
    { "filename": "main.tf", "valid": true, "error_count": 0, "diagnostics": [] },
    { "filename": "bad.tf", "valid": false, "error_count": 2, "diagnostics": [...] }
  ]
}
```

### Source Selection Rules

Exactly one of `content`, `path`, `paths`, or `dir` must be provided — they are mutually exclusive. For the `generate` operation, use `blocks` instead.

---

## Extracting Variables

Variables are extracted with their name, type, default value, description, sensitive flag, nullable flag, and any validation blocks:

```yaml
spec:
  resolvers:
    vars:
      resolve:
        with:
          - provider: hcl
            inputs:
              content: |
                variable "environment" {
                  type        = string
                  default     = "dev"
                  description = "Deployment environment"
                  sensitive   = false

                  validation {
                    condition     = contains(["dev", "staging", "prod"], var.environment)
                    error_message = "Must be dev, staging, or prod."
                  }
                }

                variable "instance_count" {
                  type    = number
                  default = 3
                }
```

The output `variables` array will contain:

```json
[
  {
    "name": "environment",
    "type": "string",
    "default": "dev",
    "description": "Deployment environment",
    "sensitive": false,
    "validation": [
      {
        "condition": "contains([\"dev\", \"staging\", \"prod\"], var.environment)",
        "error_message": "Must be dev, staging, or prod."
      }
    ]
  },
  {
    "name": "instance_count",
    "default": 3
  }
]
```

Note that `type` for variables is returned as source text (e.g., `"string"`, `"number"`, `"list(string)"`) since HCL type expressions are not simple literals.

---

## Extracting Resources

Resources include their type, name, attributes, and any sub-blocks:

```yaml
spec:
  resolvers:
    resources:
      resolve:
        with:
          - provider: hcl
            inputs:
              content: |
                resource "aws_instance" "web" {
                  ami           = "ami-12345678"
                  instance_type = "t3.micro"

                  tags = {
                    Name = "web-server"
                  }

                  ebs_block_device {
                    device_name = "/dev/sda1"
                    volume_size = 50
                  }

                  lifecycle {
                    create_before_destroy = true
                  }
                }
```

Expected output for the `resources` array:

```json
[
  {
    "attributes": {
      "ami": "ami-12345678",
      "instance_type": "t3.micro",
      "tags": {
        "Name": "web-server"
      }
    },
    "blocks": [
      {
        "attributes": {
          "device_name": "/dev/sda1",
          "volume_size": 50
        },
        "type": "ebs_block_device"
      },
      {
        "attributes": {
          "create_before_destroy": true
        },
        "type": "lifecycle"
      }
    ],
    "name": "web",
    "type": "aws_instance"
  }
]
```

Each resource includes:
- `type`: The resource type (e.g., `"aws_instance"`)
- `name`: The resource name (e.g., `"web"`)
- `attributes`: A map of attribute key-value pairs — nested maps like `tags` are preserved
- `blocks`: An array of sub-blocks (e.g., `ebs_block_device`, `lifecycle`) each with their own `type` and `attributes`

---

## Working with Modules

Module blocks extract the source, version, and any additional attributes:

```yaml
spec:
  resolvers:
    modules:
      resolve:
        with:
          - provider: hcl
            inputs:
              content: |
                module "vpc" {
                  source  = "terraform-aws-modules/vpc/aws"
                  version = "5.0.0"
                  name    = "main-vpc"
                  cidr    = "10.0.0.0/16"
                }
```

Expected output for the `modules` array:

```json
[
  {
    "attributes": {
      "cidr": "10.0.0.0/16",
      "name": "main-vpc"
    },
    "name": "vpc",
    "source": "terraform-aws-modules/vpc/aws",
    "version": "5.0.0"
  }
]
```

Well-known fields (`source`, `version`, `count`, `for_each`, `depends_on`, `providers`) are promoted to top-level keys. Other attributes go into the `attributes` map.

---

## Terraform Block Extraction

The `terraform` block is parsed with special handling for `required_providers`, `backend`, and `cloud` sub-blocks:

```yaml
spec:
  resolvers:
    tfBlock:
      resolve:
        with:
          - provider: hcl
            inputs:
              content: |
                terraform {
                  required_version = ">= 1.5.0"

                  required_providers {
                    aws = {
                      source  = "hashicorp/aws"
                      version = "~> 5.0"
                    }
                  }

                  backend "s3" {
                    bucket = "my-state-bucket"
                    key    = "state.tfstate"
                    region = "us-east-1"
                  }
                }
```

Expected output for the `terraform` object:

```json
{
  "backend": {
    "attributes": {
      "bucket": "my-state-bucket",
      "key": "state.tfstate",
      "region": "us-east-1"
    },
    "type": "s3"
  },
  "required_providers": {
    "aws": {
      "source": "hashicorp/aws",
      "version": "~> 5.0"
    }
  },
  "required_version": ">= 1.5.0"
}
```

The terraform block contains:
- `required_version`: The version constraint string
- `required_providers`: A map of provider name to source/version requirements
- `backend`: An object with `type` (e.g., `"s3"`) and `attributes` containing the backend configuration

---

## Combining with CEL Expressions

The real power comes from combining the HCL provider with CEL expressions to query and transform the parsed data:

```yaml
spec:
  resolvers:
    config:
      resolve:
        with:
          - provider: hcl
            inputs:
              path: ./main.tf

    variableNames:
      description: Extract just variable names
      resolve:
        with:
          - provider: cel
            inputs:
              expression: |
                resolvers.config.variables.map(v, v.name)

    resourceTypes:
      description: Get unique resource types
      resolve:
        with:
          - provider: cel
            inputs:
              expression: |
                resolvers.config.resources.map(r, r.type)

    hasS3Backend:
      description: Check if using S3 backend
      resolve:
        with:
          - provider: cel
            inputs:
              expression: |
                has(resolvers.config.terraform.backend) &&
                resolvers.config.terraform.backend.type == "s3"
```

---

## Transform Capability

The HCL provider supports the `transform` capability, allowing you to chain it with other providers. For example, reading a file first, then parsing the content:

```yaml
spec:
  resolvers:
    tfData:
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: ./variables.tf
      transform:
        with:
          - provider: hcl
            inputs:
              content: "{{ .resolvers.tfData.content }}"
```

---

## Expression Handling

The HCL provider handles expressions intelligently:

- **Literal values** (strings, numbers, booleans, lists, maps) are evaluated to Go primitives
- **Complex expressions** (variable references, function calls, conditionals) are returned as raw source text

For example, given:

```hcl
resource "aws_instance" "web" {
  ami           = "ami-12345678"     # literal string → "ami-12345678"
  instance_type = var.instance_type  # reference → "var.instance_type"
  count         = var.enabled ? 1 : 0  # conditional → "var.enabled ? 1 : 0"
}
```

The `attributes` for this resource will be:

```json
{
  "ami": "ami-12345678",
  "count": "var.enabled ? 1 : 0",
  "instance_type": "var.instance_type"
}
```

- `ami` is a literal string, so it resolves to `"ami-12345678"`
- `instance_type` is a variable reference, so it's returned as the raw source text `"var.instance_type"`
- `count` is a conditional expression, so it's returned as `"var.enabled ? 1 : 0"`

This means you always get the value — either evaluated or as source text — without errors from unresolved references.

---

## Common Patterns

### Audit Terraform Configurations

```yaml
spec:
  resolvers:
    config:
      resolve:
        with:
          - provider: hcl
            inputs:
              path: ./main.tf

    audit:
      description: Audit summary of the configuration
      resolve:
        with:
          - provider: cel
            inputs:
              expression: |
                {
                  "variable_count": size(resolvers.config.variables),
                  "resource_count": size(resolvers.config.resources),
                  "module_count": size(resolvers.config.modules),
                  "has_backend": has(resolvers.config.terraform.backend)
                }
```

### Extract Required Provider Versions

```yaml
spec:
  resolvers:
    config:
      resolve:
        with:
          - provider: hcl
            inputs:
              path: ./versions.tf

    providerVersions:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: resolvers.config.terraform.required_providers
```

---

## Formatting HCL Content

The `format` operation canonically formats HCL content using the same rules as `terraform fmt`. It accepts the same `content` or `path` inputs as the `parse` operation and returns two fields:

| Field | Type | Description |
|-------|------|-------------|
| `formatted` | string | The canonically formatted HCL |
| `changed` | bool | `true` if the formatter modified the input |

### Format Inline Content

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: fmt-demo
  version: 1.0.0

spec:
  resolvers:
    result:
      resolve:
        with:
          - provider: hcl
            inputs:
              operation: format
              content: |
                variable "region" {
                type=string
                default="us-east-1"
                }
```

Output:

```json
{
  "data": {
    "changed": true,
    "formatted": "variable \"region\" {\n  type    = string\n  default = \"us-east-1\"\n}\n"
  },
  "metadata": {
    "bytes": 52,
    "filename": "input.tf",
    "operation": "format"
  }
}
```

### Format a File

```yaml
spec:
  resolvers:
    result:
      resolve:
        with:
          - provider: hcl
            inputs:
              operation: format
              path: ./main.tf
```

### Check Whether a File Needs Formatting

Combine with a CEL expression to build a lint-style check:

```yaml
spec:
  resolvers:
    fmt:
      resolve:
        with:
          - provider: hcl
            inputs:
              operation: format
              path: ./main.tf

    check:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: |
                resolvers.fmt.changed
                  ? "main.tf needs formatting — run terraform fmt"
                  : "main.tf is correctly formatted"
```

---

## Validating HCL Syntax

The `validate` operation checks HCL syntax without extracting blocks. It returns structured diagnostics, making it ideal for CI pipelines, pre-commit hooks, and configuration auditing.

### Validate Inline Content

```yaml
spec:
  resolvers:
    checkSyntax:
      resolve:
        with:
          - provider: hcl
            inputs:
              operation: validate
              content: |
                variable "region" {
                  type    = string
                  default = "us-east-1"
                }
```

Output for valid HCL:

```json
{
  "valid": true,
  "error_count": 0,
  "diagnostics": []
}
```

### Validate a File

```yaml
spec:
  resolvers:
    validateMain:
      resolve:
        with:
          - provider: hcl
            inputs:
              operation: validate
              path: ./main.tf
```

### Validate a Directory

```yaml
spec:
  resolvers:
    validateAll:
      resolve:
        with:
          - provider: hcl
            inputs:
              operation: validate
              dir: ./terraform
```

When validating multiple files, the output aggregates results:

```json
{
  "valid": false,
  "error_count": 3,
  "files": [
    {
      "filename": "main.tf",
      "valid": true,
      "error_count": 0,
      "diagnostics": []
    },
    {
      "filename": "bad.tf",
      "valid": false,
      "error_count": 3,
      "diagnostics": [
        {
          "severity": "error",
          "summary": "Invalid block definition",
          "range": { "filename": "bad.tf", "start": { "line": 1, "column": 10 } }
        }
      ]
    }
  ]
}
```

### Use Validate with CEL

```yaml
spec:
  resolvers:
    syntaxCheck:
      resolve:
        with:
          - provider: hcl
            inputs:
              operation: validate
              dir: ./terraform
      expression: |
        result.valid
          ? "All files are syntactically correct"
          : "Found " + string(result.error_count) + " syntax errors"
```

---

## Generating HCL from Structured Data

The `generate` operation produces HCL text from a structured map, using the same schema as the parse output. This enables round-trip workflows: parse HCL, transform the data, then generate updated HCL.

### Basic Generation

```yaml
spec:
  resolvers:
    generated:
      resolve:
        with:
          - provider: hcl
            inputs:
              operation: generate
              blocks:
                variables:
                  - name: region
                    type: string
                    default: us-east-1
                    description: "AWS region"
                  - name: env
                    type: string
                    default: dev
```

Output:

```json
{
  "hcl": "variable \"region\" {\n  type        = string\n  default     = \"us-east-1\"\n  description = \"AWS region\"\n}\n\nvariable \"env\" {\n  type    = string\n  default = \"dev\"\n}\n"
}
```

### Generate Resources

```yaml
spec:
  resolvers:
    infraCode:
      resolve:
        with:
          - provider: hcl
            inputs:
              operation: generate
              blocks:
                resources:
                  - type: aws_instance
                    name: web
                    attributes:
                      ami: ami-12345
                      instance_type: t3.micro
                      tags:
                        Name: web-server
                        Environment: prod
```

### Block Ordering

Generated HCL follows Terraform convention ordering:
1. `terraform`
2. `variable`
3. `locals`
4. `data`
5. `resource`
6. `module`
7. `output`
8. `provider`
9. `moved`
10. `import`
11. `check`

### Round-Trip Workflow

Parse existing HCL, transform it, and generate new HCL:

```yaml
spec:
  resolvers:
    original:
      resolve:
        with:
          - provider: hcl
            inputs:
              path: ./main.tf

    updated:
      resolve:
        with:
          - provider: hcl
            inputs:
              operation: generate
              blocks: "{{ .resolvers.original | toJson }}"
```

### Generating Terraform JSON (`.tf.json`)

Set `output_format: json` to produce [Terraform JSON Configuration Syntax](https://developer.hashicorp.com/terraform/language/syntax/json) instead of native HCL:

```yaml
spec:
  resolvers:
    jsonConfig:
      resolve:
        with:
          - provider: hcl
            inputs:
              operation: generate
              output_format: json
              blocks:
                variables:
                  - name: region
                    type: string
                    default: us-east-1
                resources:
                  - type: aws_instance
                    name: web
                    attributes:
                      ami: ami-12345
                      instance_type: t3.micro
```

Output:

```json
{
  "hcl": "{\n  \"variable\": {\n    \"region\": {\n      \"default\": \"us-east-1\",\n      \"type\": \"string\"\n    }\n  },\n  \"resource\": {\n    \"aws_instance\": {\n      \"web\": {\n        \"ami\": \"ami-12345\",\n        \"instance_type\": \"t3.micro\"\n      }\n    }\n  }\n}\n"
}
```

The JSON output follows the [Terraform JSON Configuration Syntax](https://developer.hashicorp.com/terraform/language/syntax/json) specification:
- Block types become top-level JSON keys (singular: `variable`, `resource`, not plural)
- Single-label blocks (variable, module, output) nest by name: `{"variable": {"region": {...}}}`
- Double-label blocks (resource, data) nest by type then name: `{"resource": {"aws_instance": {"web": {...}}}}`
- Provider blocks with aliases produce arrays for the same provider name
- Unlabeled blocks (moved, import) remain as arrays

---

## Supported Block Types

The HCL provider extracts the following Terraform/OpenTofu block types:

| Block Type | Output Key | Structure |
|-----------|-----------|-----------|
| `variable` | `variables` | Array of objects with name, type, default, description, validation |
| `resource` | `resources` | Array of objects with type, name, attributes, blocks |
| `data` | `data` | Array of objects with type, name, attributes, blocks |
| `module` | `modules` | Array of objects with name, source, version, attributes |
| `output` | `outputs` | Array of objects with name, value, description, sensitive |
| `locals` | `locals` | Map of key-value pairs (merged across multiple blocks) |
| `provider` | `providers` | Array of objects with name, alias, region, attributes |
| `terraform` | `terraform` | Object with required_version, required_providers, backend |
| `moved` | `moved` | Array of objects with from, to |
| `import` | `import` | Array of objects with to, id, provider |
| `check` | `check` | Array of objects with name, data, assertions |

---

## Running the Provider Directly

You can also run the HCL provider directly from the command line:

{{< tabs "hcl-provider-tutorial-cmd-2" >}}
{{% tab "Bash" %}}
```bash
# Parse a file
scafctl run provider hcl path=./main.tf -o json

# Parse a directory
scafctl run provider hcl dir=./terraform -o json

# Format inline HCL
scafctl run provider hcl operation=format 'content=variable "x" { type=string }' -o json

# Format a file
scafctl run provider hcl operation=format path=./main.tf -o json

# Format all files in a directory
scafctl run provider hcl operation=format dir=./terraform -o json

# Validate a file
scafctl run provider hcl operation=validate path=./main.tf -o json

# Validate a directory
scafctl run provider hcl operation=validate dir=./terraform -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Parse a file
scafctl run provider hcl path=./main.tf -o json

# Parse a directory
scafctl run provider hcl dir=./terraform -o json

# Format inline HCL
scafctl run provider hcl operation=format 'content=variable "x" { type=string }' -o json

# Format a file
scafctl run provider hcl operation=format path=./main.tf -o json

# Format all files in a directory
scafctl run provider hcl operation=format dir=./terraform -o json

# Validate a file
scafctl run provider hcl operation=validate path=./main.tf -o json

# Validate a directory
scafctl run provider hcl operation=validate dir=./terraform -o json
```
{{% /tab %}}
{{< /tabs >}}

{{< tabs "hcl-file-input" >}}
{{% tab "Bash" %}}
```bash
# Use a pre-built example input file
scafctl run provider hcl --input @examples/providers/hcl-format.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Wrap @file in single quotes to avoid splatting operator
scafctl run provider hcl --input '@examples/providers/hcl-format.yaml' -o json
```
{{% /tab %}}
{{< /tabs >}}

{{< tabs "hcl-provider-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
```bash
scafctl run provider hcl path=./main.tf --dry-run

# Dry run (format)
scafctl run provider hcl operation=format path=./main.tf --dry-run

# Dry run (validate)
scafctl run provider hcl operation=validate path=./main.tf --dry-run
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run provider hcl path=./main.tf --dry-run

# Dry run (format)
scafctl run provider hcl operation=format path=./main.tf --dry-run

# Dry run (validate)
scafctl run provider hcl operation=validate path=./main.tf --dry-run
```
{{% /tab %}}
{{< /tabs >}}

Dry-run returns an empty structure without reading or modifying anything. For `parse`:

```json
{
  "data": {
    "check": [],
    "data": [],
    "import": [],
    "locals": {},
    "modules": [],
    "moved": [],
    "outputs": [],
    "providers": [],
    "resources": [],
    "terraform": {},
    "variables": []
  },
  "dryRun": true,
  "metadata": {
    "mode": "dry-run",
    "operation": "parse"
  }
}
```

For `format`:

```json
{
  "data": {
    "changed": false,
    "formatted": ""
  },
  "dryRun": true,
  "metadata": {
    "mode": "dry-run",
    "operation": "format"
  }
}
```
