# scafctl Documentation

Welcome to the scafctl documentation. This directory contains all documentation for using and developing scafctl.

## Documentation Structure

### [Tutorials](tutorials/)

Step-by-step guides for learning scafctl features:

- [Actions Tutorial](tutorials/actions-tutorial.md) - Learn how to use actions for workflow automation
- [Auth Tutorial](tutorials/auth-tutorial.md) - Set up authentication for your workflows
- [Resolver Tutorial](tutorials/resolver-tutorial.md) - Master dynamic value resolution

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
- [`examples/config/`](../examples/config/) - Configuration examples
- [`examples/resolvers/`](../examples/resolvers/) - Resolver examples
- [`examples/solutions/`](../examples/solutions/) - Complete solution examples
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
