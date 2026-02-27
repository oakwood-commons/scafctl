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
3. [Extracting Variables](#extracting-variables)
4. [Extracting Resources](#extracting-resources)
5. [Working with Modules](#working-with-modules)
6. [Terraform Block Extraction](#terraform-block-extraction)
7. [Combining with CEL Expressions](#combining-with-cel-expressions)
8. [Transform Capability](#transform-capability)
9. [Expression Handling](#expression-handling)
10. [Common Patterns](#common-patterns)

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

```bash
scafctl run resolver -f parse-hcl.yaml -o json
```

Expected output:

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
    "variables": [
      {
        "default": "us-east-1",
        "description": "AWS region",
        "name": "region",
        "type": "string"
      }
    ]
  },
  "metadata": {
    "bytes": 102,
    "filename": "input.tf"
  }
}
```

All block types are represented in the output — empty arrays/maps for types not present in the input, and populated entries for those that are.

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

```bash
# Parse a file
scafctl run provider hcl --input path=./main.tf -o json

# Dry run
scafctl run provider hcl --input path=./main.tf --dry-run
```

Dry-run output returns the empty structure with a `dryRun` flag instead of actually parsing:

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
    "mode": "dry-run"
  }
}
```
