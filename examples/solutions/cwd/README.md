# Working Directory (`--cwd`) Example

Demonstrates the `--cwd` (`-C`) global flag for running commands from a different working directory without `cd`-ing into it.

## Concept

The `--cwd` flag changes where scafctl resolves relative paths. This is useful for:

- **Scripting**: Run commands against projects in different directories
- **CI/CD**: Execute from a checkout root while targeting a subdirectory
- **MCP agents**: Specify the project context when the server runs elsewhere

## Solution

The solution reads a local file (`data.txt`) using the `file` provider, demonstrating that relative paths resolve against the `--cwd` directory.

## Running

```bash
# From the repository root — this would fail without --cwd because
# data.txt is not in the repo root:
scafctl --cwd examples/solutions/cwd run resolver -f solution.yaml -o json

# Short-form flag (same as git -C):
scafctl -C examples/solutions/cwd run resolver -f solution.yaml -o json

# Combine with --output-dir (output-dir resolves against --cwd too):
scafctl -C examples/solutions/cwd run solution -f solution.yaml --output-dir /tmp/cwd-demo

# Equivalent to cd-ing into the directory first:
cd examples/solutions/cwd && scafctl run resolver -f solution.yaml -o json
```

## MCP Usage

```json
{
  "tool": "preview_resolvers",
  "arguments": {
    "path": "solution.yaml",
    "cwd": "/path/to/examples/solutions/cwd"
  }
}
```

## Key Takeaway

`--cwd` affects **all** relative paths — the solution file (`-f`), files read by resolvers, files written by actions, and `--output-dir` itself.
