# <% .name %>

<% .description %>

## Installation

```bash
# Build from source
task build

# Or download from releases
gh release download --repo <% .module %>
```

## Usage

<% if eq .plugin_type "auth-handler" -%>
Register this plugin in your scafctl configuration, then use
the **<% .provider_name %>** auth handler:

```bash
scafctl auth login <% .provider_name %>
```

Once authenticated, reference it in HTTP requests:

```yaml
resolvers:
  data:
    resolve:
      with:
        - provider: http
          inputs:
            url: https://api.example.com/data
            auth: <% .provider_name %>
```
<%- else -%>
Register this plugin in your scafctl configuration, then reference
the **<% .provider_name %>** provider in your solutions:

```yaml
resolvers:
  my-value:
    resolve:
      with:
        - provider: <% .provider_name %>
          inputs:
            value: "hello"
```
<%- end %>

## Development

```bash
# Run tests
task test

# Run linter
task lint

# Build
task build

# Full CI pipeline
task ci
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache-2.0 -- see [LICENSE](LICENSE) for details.
