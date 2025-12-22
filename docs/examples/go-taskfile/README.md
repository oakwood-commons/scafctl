# Go Project Pipeline Example

This example demonstrates a complete Go project workflow using scafctl. It orchestrates common CI/build tasks with proper action dependencies and conditional execution.

## Overview

The solution defines a pipeline for Go projects with four sequential stages:
1. **Lint** - Run code linters
2. **Test** - Execute tests (depends on lint)
3. **Build** - Compile binary (depends on test)
4. **Container Image** - Build Docker image (depends on build, conditional on non-dev version)

## Key Concepts Demonstrated

### Resolvers
- **CLI override**: `projectName` and `version` accept CLI input via `-r` flag
- **Environment variables**: Fallback to `PROJECT_NAME` and `APP_VERSION` env vars
- **Git integration**: `version` can resolve from git tags
- **Expression evaluation**: `imageName` derives from other resolvers using CEL
- **Static defaults**: All resolvers have sensible defaults

### Actions (Keyed Map)
- **Explicit naming**: Actions are keyed (e.g., `lint:`, `test:`) for clarity and DAG construction
- **Dependency graph**: `dependsOn` references other action keys to build execution DAG
- **Environment context**: Actions access resolver values via `{{ _.resolverName }}`
- **Conditional execution**: Container image action only runs if version is not dev via `when` condition
- **Shell provider**: Demonstrates command execution with environment variables

### Configuration

```yaml
# Default behavior (no input required)
scafctl run solution:go-project-pipeline

# With custom project name and version
scafctl run solution:go-project-pipeline \
  -r projectName=my-service \
  -r version=2.0.0

# Preview actions without execution
scafctl run solution:go-project-pipeline \
  -r projectName=my-service \
  --dry-run

# With custom registry
scafctl run solution:go-project-pipeline \
  -r projectName=my-service \
  -r registry=registry.company.com
```

## Learning Points

1. **Action Dependencies**: `dependsOn` ensures lint → test → build → image order
2. **Conditional Execution**: `when: _.version != "0.0.0-dev"` prevents dev image builds
3. **Context Access**: All resolvers available in action commands via `{{ _.resolverName }}`
4. **Input Precedence**: CLI > Environment > Git > Static defaults
5. **Validation**: Project name validated against regex pattern

## Testing

This solution includes inline tests demonstrating resolver validation:

```bash
# Test resolver defaults
scafctl test solution:go-project-pipeline --test resolve-defaults

# Test with CLI input
scafctl test solution:go-project-pipeline --test resolve-cli-input

# Test dry-run mode
scafctl test solution:go-project-pipeline --test dry-run-default
```

## See Also

- `notes/resolvers.md` - Resolver input sources and validation
- `notes/actions.md` - Action execution model and dependencies
- `notes/providers.md` - Provider types and contracts
- `.github/copilot-instructions.md` - Architectural overview
