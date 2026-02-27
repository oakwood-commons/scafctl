// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hclprovider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tfVariables = `
variable "region" {
  type        = string
  default     = "us-east-1"
  description = "AWS region for resources"
}

variable "instance_count" {
  type    = number
  default = 3
}

variable "enable_monitoring" {
  type      = bool
  default   = true
  sensitive = false
  nullable  = false
}

variable "tags" {
  type = map(string)
  default = {
    env  = "dev"
    team = "platform"
  }
}
`

const tfResources = `
resource "aws_instance" "web" {
  ami           = "ami-12345678"
  instance_type = "t3.micro"
  tags = {
    Name = "web-server"
  }
}

resource "aws_s3_bucket" "data" {
  bucket = "my-data-bucket"
}
`

const tfResourceSubBlocks = `
resource "aws_instance" "web" {
  ami           = "ami-12345678"
  instance_type = "t3.micro"

  ebs_block_device {
    device_name = "/dev/sda1"
    volume_size = 50
  }

  lifecycle {
    create_before_destroy = true
  }
}
`

const tfDataSources = `
data "aws_ami" "ubuntu" {
  most_recent = true

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-focal-20.04-amd64-server-*"]
  }
}
`

const tfModules = `
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.0.0"
  name    = "my-vpc"
  cidr    = "10.0.0.0/16"
}

module "eks" {
  source          = "./modules/eks"
  cluster_name    = "my-cluster"
  cluster_version = "1.28"
}
`

const tfOutputs = `
output "vpc_id" {
  value       = "vpc-12345"
  description = "The VPC ID"
  sensitive   = false
}

output "secret_key" {
  value     = "super-secret"
  sensitive = true
}
`

const tfLocals = `
locals {
  environment = "production"
  region      = "us-west-2"
}

locals {
  extra = "value"
}
`

const tfProviders = `
provider "aws" {
  region = "us-east-1"
}

provider "aws" {
  alias  = "west"
  region = "us-west-2"
}
`

const tfTerraformBlock = `
terraform {
  required_version = ">= 1.5.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  backend "s3" {
    bucket = "my-terraform-state"
    key    = "state.tfstate"
    region = "us-east-1"
  }
}
`

const tfMoved = `
moved {
  from = aws_instance.old
  to   = aws_instance.new
}
`

const tfImport = `
import {
  to = aws_instance.existing
  id = "i-1234567890abcdef0"
}
`

const tfExpressions = `
resource "aws_instance" "web" {
  ami           = var.ami_id
  instance_type = local.instance_type
  count         = var.enabled ? 1 : 0
}
`

const tfValidation = `
variable "environment" {
  type    = string
  default = "dev"

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "Must be one of: dev, staging, prod."
  }
}
`

const tfCheck = `
check "health" {
  data "http" "health" {
    url = "https://example.com/health"
  }

  assert {
    condition     = data.http.health.status_code == 200
    error_message = "Health check failed"
  }
}
`

const tfListValues = `
variable "availability_zones" {
  type    = list(string)
  default = ["us-east-1a", "us-east-1b", "us-east-1c"]
}
`

const tfFullConfig = `
terraform {
  required_version = ">= 1.5.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = "us-east-1"
}

variable "environment" {
  type        = string
  default     = "dev"
  description = "Deployment environment"
}

variable "instance_type" {
  type    = string
  default = "t3.micro"
}

locals {
  name_prefix = "myapp"
}

data "aws_ami" "amazon_linux" {
  most_recent = true
}

resource "aws_instance" "app" {
  ami           = "ami-123"
  instance_type = "t3.micro"
  tags = {
    Name = "app-server"
  }
}

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.0.0"
  name    = "main-vpc"
}

output "instance_id" {
  value       = "i-mock"
  description = "The instance ID"
}
`

func TestParseHCL_Variables(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfVariables), "variables.tf")
	require.NoError(t, err)

	vars := result["variables"].([]any)
	require.Len(t, vars, 4)

	region := vars[0].(map[string]any)
	assert.Equal(t, "region", region["name"])
	assert.Equal(t, "us-east-1", region["default"])
	assert.Equal(t, "AWS region for resources", region["description"])

	count := vars[1].(map[string]any)
	assert.Equal(t, "instance_count", count["name"])
	assert.Equal(t, int64(3), count["default"])

	monitoring := vars[2].(map[string]any)
	assert.Equal(t, "enable_monitoring", monitoring["name"])
	assert.Equal(t, true, monitoring["default"])
	assert.Equal(t, false, monitoring["sensitive"])
	assert.Equal(t, false, monitoring["nullable"])

	tags := vars[3].(map[string]any)
	assert.Equal(t, "tags", tags["name"])
	tagDefault, ok := tags["default"].(map[string]any)
	require.True(t, ok, "tags default should be a map")
	assert.Equal(t, "dev", tagDefault["env"])
	assert.Equal(t, "platform", tagDefault["team"])
}

func TestParseHCL_VariableValidation(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfValidation), "test.tf")
	require.NoError(t, err)

	vars := result["variables"].([]any)
	require.Len(t, vars, 1)

	v := vars[0].(map[string]any)
	assert.Equal(t, "environment", v["name"])

	validations := v["validation"].([]any)
	require.Len(t, validations, 1)

	val := validations[0].(map[string]any)
	assert.Equal(t, "Must be one of: dev, staging, prod.", val["error_message"])
	assert.Contains(t, val["condition"].(string), "contains")
}

func TestParseHCL_Resources(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfResources), "main.tf")
	require.NoError(t, err)

	resources := result["resources"].([]any)
	require.Len(t, resources, 2)

	web := resources[0].(map[string]any)
	assert.Equal(t, "aws_instance", web["type"])
	assert.Equal(t, "web", web["name"])
	attrs := web["attributes"].(map[string]any)
	assert.Equal(t, "ami-12345678", attrs["ami"])
	assert.Equal(t, "t3.micro", attrs["instance_type"])

	bucket := resources[1].(map[string]any)
	assert.Equal(t, "aws_s3_bucket", bucket["type"])
	assert.Equal(t, "data", bucket["name"])
}

func TestParseHCL_ResourceWithSubBlocks(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfResourceSubBlocks), "main.tf")
	require.NoError(t, err)

	resources := result["resources"].([]any)
	require.Len(t, resources, 1)

	web := resources[0].(map[string]any)
	blocks := web["blocks"].([]any)
	require.Len(t, blocks, 2)

	ebs := blocks[0].(map[string]any)
	assert.Equal(t, "ebs_block_device", ebs["type"])
	ebsAttrs := ebs["attributes"].(map[string]any)
	assert.Equal(t, "/dev/sda1", ebsAttrs["device_name"])
	assert.Equal(t, int64(50), ebsAttrs["volume_size"])

	lifecycle := blocks[1].(map[string]any)
	assert.Equal(t, "lifecycle", lifecycle["type"])
}

func TestParseHCL_DataSources(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfDataSources), "data.tf")
	require.NoError(t, err)

	dataSources := result["data"].([]any)
	require.Len(t, dataSources, 1)

	ami := dataSources[0].(map[string]any)
	assert.Equal(t, "aws_ami", ami["type"])
	assert.Equal(t, "ubuntu", ami["name"])
	attrs := ami["attributes"].(map[string]any)
	assert.Equal(t, true, attrs["most_recent"])

	blocks := ami["blocks"].([]any)
	require.Len(t, blocks, 1)
	filter := blocks[0].(map[string]any)
	assert.Equal(t, "filter", filter["type"])
}

func TestParseHCL_Modules(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfModules), "modules.tf")
	require.NoError(t, err)

	modules := result["modules"].([]any)
	require.Len(t, modules, 2)

	vpc := modules[0].(map[string]any)
	assert.Equal(t, "vpc", vpc["name"])
	assert.Equal(t, "terraform-aws-modules/vpc/aws", vpc["source"])
	assert.Equal(t, "5.0.0", vpc["version"])

	eks := modules[1].(map[string]any)
	assert.Equal(t, "eks", eks["name"])
	assert.Equal(t, "./modules/eks", eks["source"])
}

func TestParseHCL_Outputs(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfOutputs), "outputs.tf")
	require.NoError(t, err)

	outputs := result["outputs"].([]any)
	require.Len(t, outputs, 2)

	vpcOut := outputs[0].(map[string]any)
	assert.Equal(t, "vpc_id", vpcOut["name"])
	assert.Equal(t, "vpc-12345", vpcOut["value"])
	assert.Equal(t, "The VPC ID", vpcOut["description"])
	assert.Equal(t, false, vpcOut["sensitive"])

	secretOut := outputs[1].(map[string]any)
	assert.Equal(t, "secret_key", secretOut["name"])
	assert.Equal(t, true, secretOut["sensitive"])
}

func TestParseHCL_Locals(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfLocals), "locals.tf")
	require.NoError(t, err)

	locals := result["locals"].(map[string]any)
	assert.Equal(t, "production", locals["environment"])
	assert.Equal(t, "us-west-2", locals["region"])
	assert.Equal(t, "value", locals["extra"])
}

func TestParseHCL_Providers(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfProviders), "providers.tf")
	require.NoError(t, err)

	providers := result["providers"].([]any)
	require.Len(t, providers, 2)

	p1 := providers[0].(map[string]any)
	assert.Equal(t, "aws", p1["name"])
	assert.Equal(t, "us-east-1", p1["region"])

	p2 := providers[1].(map[string]any)
	assert.Equal(t, "aws", p2["name"])
	assert.Equal(t, "west", p2["alias"])
	assert.Equal(t, "us-west-2", p2["region"])
}

func TestParseHCL_TerraformBlock(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfTerraformBlock), "versions.tf")
	require.NoError(t, err)

	tf := result["terraform"].(map[string]any)
	assert.Equal(t, ">= 1.5.0", tf["required_version"])

	rp := tf["required_providers"].(map[string]any)
	assert.NotNil(t, rp["aws"])

	backend := tf["backend"].(map[string]any)
	assert.Equal(t, "s3", backend["type"])
	backendAttrs := backend["attributes"].(map[string]any)
	assert.Equal(t, "my-terraform-state", backendAttrs["bucket"])
}

func TestParseHCL_Moved(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfMoved), "moved.tf")
	require.NoError(t, err)

	moved := result["moved"].([]any)
	require.Len(t, moved, 1)

	m := moved[0].(map[string]any)
	assert.NotEmpty(t, m["from"])
	assert.NotEmpty(t, m["to"])
}

func TestParseHCL_Import(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfImport), "import.tf")
	require.NoError(t, err)

	imports := result["import"].([]any)
	require.Len(t, imports, 1)

	imp := imports[0].(map[string]any)
	assert.NotEmpty(t, imp["to"])
	assert.Equal(t, "i-1234567890abcdef0", imp["id"])
}

func TestParseHCL_Expressions(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfExpressions), "main.tf")
	require.NoError(t, err)

	resources := result["resources"].([]any)
	require.Len(t, resources, 1)

	web := resources[0].(map[string]any)
	attrs := web["attributes"].(map[string]any)

	assert.Equal(t, "var.ami_id", attrs["ami"])
	assert.Equal(t, "local.instance_type", attrs["instance_type"])
	assert.Contains(t, attrs["count"].(string), "var.enabled")
}

func TestParseHCL_EmptyContent(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(""), "empty.tf")
	require.NoError(t, err)

	assert.Empty(t, result["variables"].([]any))
	assert.Empty(t, result["resources"].([]any))
	assert.Empty(t, result["modules"].([]any))
}

func TestParseHCL_InvalidHCL(t *testing.T) {
	t.Parallel()
	_, err := ParseHCL([]byte("this is { not valid hcl !!!"), "bad.tf")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse HCL")
}

func TestParseHCL_DefaultFilename(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte("variable \"x\" { default = \"y\" }"), "")
	require.NoError(t, err)

	vars := result["variables"].([]any)
	require.Len(t, vars, 1)
	assert.Equal(t, "x", vars[0].(map[string]any)["name"])
}

func TestParseHCL_Check(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfCheck), "checks.tf")
	require.NoError(t, err)

	checks := result["check"].([]any)
	require.Len(t, checks, 1)

	check := checks[0].(map[string]any)
	assert.Equal(t, "health", check["name"])

	dataBlocks := check["data"].([]any)
	require.Len(t, dataBlocks, 1)
	assert.Equal(t, "http", dataBlocks[0].(map[string]any)["type"])

	assertions := check["assertions"].([]any)
	require.Len(t, assertions, 1)
	assert.Equal(t, "Health check failed", assertions[0].(map[string]any)["error_message"])
}

func TestParseHCL_ListValues(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfListValues), "test.tf")
	require.NoError(t, err)

	vars := result["variables"].([]any)
	require.Len(t, vars, 1)

	v := vars[0].(map[string]any)
	def := v["default"].([]any)
	assert.Equal(t, []any{"us-east-1a", "us-east-1b", "us-east-1c"}, def)
}

func TestParseHCL_FullTerraformConfig(t *testing.T) {
	t.Parallel()
	result, err := ParseHCL([]byte(tfFullConfig), "full.tf")
	require.NoError(t, err)

	assert.Len(t, result["variables"].([]any), 2)
	assert.Len(t, result["resources"].([]any), 1)
	assert.Len(t, result["data"].([]any), 1)
	assert.Len(t, result["modules"].([]any), 1)
	assert.Len(t, result["outputs"].([]any), 1)
	assert.Len(t, result["providers"].([]any), 1)

	locals := result["locals"].(map[string]any)
	assert.Equal(t, "myapp", locals["name_prefix"])

	tf := result["terraform"].(map[string]any)
	assert.Equal(t, ">= 1.5.0", tf["required_version"])
}
