# scafctl Documentation

Welcome to the scafctl documentation. This directory contains all documentation for using and developing scafctl.

## Documentation Structure

### [Tutorials](tutorials/)

Step-by-step guides for learning scafctl features:

- [Getting Started](tutorials/getting-started.md) - Install scafctl and run your first solution
- [Resolver Tutorial](tutorials/resolver-tutorial.md) - Master dynamic value resolution
- [Actions Tutorial](tutorials/actions-tutorial.md) - Learn how to use actions for workflow automation
- [Go Templates Tutorial](tutorials/go-templates-tutorial.md) - Generate files with Go template rendering
- [Auth Tutorial](tutorials/auth-tutorial.md) - Set up authentication for your workflows
- [Catalog Tutorial](tutorials/catalog-tutorial.md) - Store and manage solutions in your catalog
- [Snapshots Tutorial](tutorials/snapshots-tutorial.md) - Capture and compare execution snapshots
- [Config Tutorial](tutorials/config-tutorial.md) - Manage application configuration
- [CEL Expressions Tutorial](tutorials/cel-tutorial.md) - Master CEL expressions and extension functions
- [Cache Tutorial](tutorials/cache-tutorial.md) - Manage cached data and reclaim disk space
- [Provider Reference](tutorials/provider-reference.md) - Complete documentation for all built-in providers

### [Design](design/)

Architecture and design documentation:

- [Actions](design/actions.md) - Actions system design
- [Auth](design/auth.md) - Authentication architecture
- [CEL Integration](design/cel-integration.md) - CEL expression language integration
- [CLI](design/cli.md) - Command-line interface design
- [Plugins](design/plugins.md) - Plugin system architecture
- [Providers](design/providers.md) - Provider framework design
- [Resolvers](design/resolvers.md) - Resolver system design
- [Solutions](design/solutions.md) - Solutions architecture

### [Internal](internal/)

Developer documentation and implementation details (for contributors):

- Implementation details for core features
- TODO and development notes

## Examples

Runnable examples are located in the [`/examples`](../examples/) directory at the repository root:

- [`examples/actions/`](../examples/actions/) - Action workflow examples
- [`examples/resolvers/`](../examples/resolvers/) - Resolver examples
- [`examples/solutions/`](../examples/solutions/) - Complete solution examples
- [`examples/snapshots/`](../examples/snapshots/) - Execution snapshot examples
- [`examples/catalog/`](../examples/catalog/) - Catalog bundling examples
- [`examples/config/`](../examples/config/) - Configuration examples
- [`examples/plugins/`](../examples/plugins/) - Plugin examples

## Getting Help

- Run `scafctl --help` for CLI usage
- Run `scafctl <command> --help` for command-specific help

## Documentation Site

This documentation can be served as a static site using [Hugo](https://gohugo.io/):

```bash
# Install Hugo (macOS)
brew install hugo

# Download theme module (first time only)
hugo mod get -u

# Start local dev server
hugo server

# Build for production
hugo --minify
```

See [internal/hugo-guide.md](internal/hugo-guide.md) for detailed instructions.
