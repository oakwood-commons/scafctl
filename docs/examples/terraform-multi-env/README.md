# Terraform Multi-Environment Infrastructure Example

This example demonstrates how to scaffold multi-environment Terraform configurations using scafctl. It showcases template rendering, foreach iteration, and conditional filesystem actions.

## Overview

The solution scaffolds Terraform module directories for multiple environments with proper separation of concerns. Each environment gets its own configuration directory with backend and provider settings.

**Generated structure:**
```
terraform/
├── dev/
│   ├── backend.tf
│   └── main.tf
├── qa/
│   ├── backend.tf
│   └── main.tf
└── prod/
    ├── backend.tf
    └── main.tf
```

## Key Concepts Demonstrated

### Resolvers

- **CLI input**: `projectId` and `environments` accept user overrides
- **Environment variables**: `GCP_PROJECT_ID` and `GCP_REGION` fallbacks
- **Expression evaluation**: `stateBucket` derived from `projectId` using CEL
- **Array handling**: `environments` is an array resolver for iteration
- **Static defaults**: Sensible defaults for project and regions

### Templates (With Explicit Dependencies)

- **Resolver dependencies**: Template declares `resolvers: [projectId, region, stateBucket, env]`
- **Dependency resolution**: Engine resolves declared dependencies from `_` context
- **Foreach binding**: `as: __env` makes `__env` available for the current iteration (aliases must start with `__`)
- **Output mapping**: Template declares output path via `outputs: {path: ./terraform/{{ __env }}}`
- **Multi-file generation**: Filesystem template produces multiple files per environment
- **Conditional rendering**: Template skips rendering if `when` condition is false

### Actions (Unified Resolver Consumption)

- **No template declarations**: Actions don't declare `templates:` or `invokeTemplates`
- **Resolver consumption only**: Actions access template data via resolver context `_`
- **Implicit template rendering**: Templates render automatically when resolvers depend on them
- **Provider-agnostic**: Same template data flows to filesystem, archive, api, etc.
- **Conditional execution**: `when:` expressions filter which actions run

## Usage Examples

### Default: Write to disk

```bash
scafctl run solution:terraform-multi-env
```

Writes terraform configs to `terraform/{dev,qa,prod}/`

### Create archive

```bash
scafctl run solution:terraform-multi-env \
  -r createArchive=true
```

Creates `archives/terraform-configs.tar.gz`

### Push to GitHub

```bash
scafctl run solution:terraform-multi-env \
  -r projectId=my-project \
  -r githubRepo=my-org/infrastructure \
  -r pushToGithub=true
```

Pushes generated configs to GitHub

### Email to team

```bash
scafctl run solution:terraform-multi-env \
  -r projectId=my-project \
  -r teamEmail=infrastructure@company.com \
  -r emailConfigs=true
```

Emails terraform configs to team

### Multiple actions

```bash
scafctl run solution:terraform-multi-env \
  -r projectId=acme-corp \
  -r createArchive=true \
  -r pushToGithub=true
```

Runs both archive and GitHub push

## Execution Flow

The solution follows the standard scafctl lifecycle:

1. **Resolve Phase**: Resolvers produce `projectId`, `environments`, `region`, `stateBucket`, and `terraformFiles`
   - `terraformFiles` resolver uses `provider: template` to trigger template rendering
   - Template dependencies (`projectId`, `region`, `stateBucket`, `env`) resolved automatically
   - Template foreach iterates over `environments`, rendering for each

2. **Action Phase**: Actions consume resolver context `_` only
   - Each action references `_.terraformFiles` from resolver context
   - Actions execute conditionally based on flags (`createArchive`, `pushToGithub`, etc.)
   - No special template invocation syntax—everything flows through resolvers

## Learning Points

1. **Templates as Resolvers**: Templates are resolver sources, not separate concepts
2. **Implicit Dependencies**: Resolvers declare template dependencies explicitly via `provider: template`
3. **No Template Declarations in Actions**: Actions don't reference templates—they consume resolver context `_`
4. **Single Data Flow**: Everything flows through the resolver model
5. **Output Mapping**: Templates declare output paths via `outputs:` field
6. **Foreach Binding**: Template `foreach: {over: _.environments, as: __env}` exposes the current value as `__env`
7. **Conditional Rendering**: `when:` in templates skips rendering if condition is false
8. **Conditional Execution**: `when:` in actions skips execution if condition is false

## Multiple Actions Pattern

This solution demonstrates how a single template can be consumed by multiple actions:

- **write-terraform-configs** (filesystem) - Write to disk
- **archive-terraform-configs** (sh/archive) - Create compressed archive
- **push-to-github** (api/github) - Push to repository
- **email-terraform-configs** (api/email) - Send to team

Each action:
1. Accesses template data via `{{ _.terraformFiles }}`
2. Operates on template output independently
3. Can be controlled via resolver flags (e.g., `createArchive`, `pushToGithub`)
4. Uses `when:` condition for conditional execution

This shows the power of treating templates as resolver sources—same rendered output, multiple consumption patterns, all via unified resolver context.

## Unified Resolver-Only Model

This solution demonstrates the final architectural pattern: **templates are resolver sources, actions consume resolver context only**.

**Key pattern:**

```yaml
# Template as a resolver source
terraformFiles:
  resolve:
    from:
      - provider: template
        name: terraform-backend   # Template renders implicitly here

# Actions access template data via resolver context
actions:
  write-terraform-configs:
    provider: filesystem
    inputs:
      source: "{{ _.terraformFiles }}"   # Template data flows through _

  archive-terraform-configs:
    provider: archive
    inputs:
      source: "{{ _.terraformFiles }}"   # Same template data, different action
```

**Benefits of this model:**
- ✅ **Single data flow**: Everything through resolver context `_`
- ✅ **No special syntax**: Templates don't require unique action fields
- ✅ **Explicit dependencies**: Resolvers declare all their needs
- ✅ **Composable**: Multiple actions consume same template data
- ✅ **Deterministic**: Same inputs always produce same outputs

## Generated Output

Each environment directory contains:

**backend.tf** - GCS backend configuration with environment-specific state path:
```hcl
backend "gcs" {
  bucket  = "my-project-terraform-state"
  prefix  = "terraform/dev/state"
}
```

**main.tf** - Storage bucket resource with common labels and environment-specific naming:
```hcl
resource "google_storage_bucket" "app_data" {
  name = "my-project-app-data-dev"
  # ...
}
```

## Testing

This solution includes inline tests validating:

1. **Resolver defaults** - Verifies sensible defaults without user input
2. **CLI input override** - Tests custom project ID and environment list
3. **Dry-run mode** - Validates action plan without filesystem writes

## See Also

- `notes/templates.md` - Template source types and lifecycle
- `notes/resolvers.md` - Resolver input sources and validation
- `notes/actions.md` - Action execution with foreach and dependsOn
- `.github/copilot-instructions.md` - Architectural overview
