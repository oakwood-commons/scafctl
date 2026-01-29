# Snapshot Commands - Troubleshooting Guide

The snapshot commands allow you to capture, inspect, and compare resolver execution states for debugging and troubleshooting purposes.

## Overview

When resolvers fail or produce unexpected results, snapshots provide detailed visibility into:
- Resolver execution order and dependencies
- Input parameters and values at each phase
- Execution status and timing information
- Error messages and failed attempts
- Configuration evaluation results

## Commands

### `scafctl snapshot save`

Captures the current state of resolver execution and saves it to a file.

**Usage:**
```bash
scafctl snapshot save --output <file> [--config <config-file>] [--resolver <name>] [--redact <pattern>]
```

**Flags:**
- `--output, -o` (required) - Output file path for the snapshot
- `--config, -c` - Configuration file path (default: searches for config in current directory)
- `--resolver, -r` - Only capture specific resolver(s), can be specified multiple times
- `--redact` - Regex pattern to redact sensitive values in the snapshot

**Examples:**

Capture all resolvers:
```bash
scafctl snapshot save --output snapshot.json
```

Capture specific resolvers:
```bash
scafctl snapshot save --output snapshot.json --resolver database --resolver api
```

Redact sensitive information:
```bash
scafctl snapshot save --output snapshot.json --redact "password|secret|token"
```

### `scafctl snapshot show`

Displays the contents of a saved snapshot in various formats.

**Usage:**
```bash
scafctl snapshot show <snapshot-file> [--format <format>] [--verbose]
```

**Flags:**
- `--format, -f` - Output format: `summary` (default), `json`, `resolvers`
- `--verbose, -v` - Show detailed information including phases and parameters

**Formats:**

**Summary** - High-level overview with status counts:
```bash
scafctl snapshot show snapshot.json
# Output:
# Snapshot Summary
# Total Resolvers: 5
# Status: ✓ 3 successful, ✗ 1 failed, ○ 1 skipped
# Total Duration: 2.5s
```

**JSON** - Full snapshot data in JSON format:
```bash
scafctl snapshot show snapshot.json --format json
```

**Resolvers** - List of resolvers with status icons:
```bash
scafctl snapshot show snapshot.json --format resolvers
# Output:
# ✓ database-config (500ms)
# ✗ api-client (failed: connection timeout)
# ○ optional-service (skipped: dependency failed)
```

**Verbose** - Detailed information including phases and parameters:
```bash
scafctl snapshot show snapshot.json --format resolvers --verbose
```

### `scafctl snapshot diff`

Compares two snapshots to identify changes in resolver behavior.

**Usage:**
```bash
scafctl snapshot diff <before-snapshot> <after-snapshot> [--format <format>] [--ignore-unchanged] [--ignore-fields <fields>] [--output <file>]
```

**Flags:**
- `--format, -f` - Output format: `human` (default), `json`, `unified`
- `--ignore-unchanged` - Only show resolvers that changed
- `--ignore-fields` - Comma-separated list of fields to ignore (e.g., "duration,timestamp")
- `--output, -o` - Write comparison to file instead of stdout

**Formats:**

**Human** - Readable comparison with status indicators:
```bash
scafctl snapshot diff before.json after.json
```

**JSON** - Structured diff data for programmatic processing:
```bash
scafctl snapshot diff before.json after.json --format json
```

**Unified** - Git-style unified diff format:
```bash
scafctl snapshot diff before.json after.json --format unified
```

## Troubleshooting Workflows

### 1. Debugging a Failing Resolver

When a resolver fails, capture a snapshot to see the full execution context:

```bash
# Capture the failing state
scafctl snapshot save --output failing.json

# View the summary
scafctl snapshot show failing.json

# Get detailed resolver information
scafctl snapshot show failing.json --format resolvers --verbose
```

**What to look for:**
- ✗ Failed resolver status with error message
- Failed attempts and retry information in verbose output
- Parameter values that might be incorrect
- Dependencies that failed or were skipped

### 2. Comparing Before and After Changes

When making configuration changes, capture snapshots before and after to verify the impact:

```bash
# Before making changes
scafctl snapshot save --output before.json

# Make your configuration changes
# ... edit config files ...

# After making changes
scafctl snapshot save --output after.json

# Compare the snapshots
scafctl snapshot diff before.json after.json --ignore-unchanged
```

**What to look for:**
- Status changes (success → failed or vice versa)
- Value changes in resolved parameters
- Duration changes indicating performance impact
- New or removed resolvers

### 3. Isolating Issues with Specific Resolvers

Focus on problematic resolvers to reduce noise:

```bash
# Capture only the problematic resolvers
scafctl snapshot save --output debug.json --resolver api-client --resolver database

# View detailed execution
scafctl snapshot show debug.json --format resolvers --verbose
```

**What to look for:**
- Execution phases and where failures occur
- Input parameter values at each phase
- Dependencies between resolvers
- Timing information for performance issues

### 4. Tracking Down Sensitive Data Leaks

When debugging, ensure sensitive data is redacted:

```bash
# Capture with redaction
scafctl snapshot save --output safe-snapshot.json --redact "password|secret|token|key"

# Share the redacted snapshot safely
scafctl snapshot show safe-snapshot.json --format json > sanitized.json
```

**What to look for:**
- `[REDACTED]` markers where sensitive values were filtered
- Patterns that might need additional redaction rules
- Configuration that should use secret references

### 5. Performance Analysis

Compare snapshots to identify performance regressions:

```bash
# Baseline snapshot
scafctl snapshot save --output baseline.json

# After changes
scafctl snapshot save --output current.json

# Compare durations
scafctl snapshot diff baseline.json current.json --format json | jq '.resolvers[] | select(.duration_changed) | {name, before_duration, after_duration}'
```

**What to look for:**
- Resolvers with significantly increased duration
- Total execution time changes
- Retry attempts that indicate transient failures
- Sequential vs. parallel execution patterns

## Understanding Snapshot Structure

A snapshot contains:

```json
{
  "version": "1.0",
  "timestamp": "2026-01-15T10:30:00Z",
  "total_duration": "2.5s",
  "resolvers": [
    {
      "name": "resolver-name",
      "status": "success|failed|skipped",
      "duration": "500ms",
      "error": "error message (if failed)",
      "phases": [
        {
          "name": "phase-name",
          "status": "success|failed|skipped",
          "duration": "100ms",
          "parameters": {
            "key": "value"
          }
        }
      ],
      "failed_attempts": [
        {
          "attempt": 1,
          "error": "error message",
          "timestamp": "2026-01-15T10:30:00Z"
        }
      ]
    }
  ]
}
```

## Best Practices

1. **Capture Early and Often**
   - Take snapshots before and after configuration changes
   - Keep snapshots from known-good states for comparison

2. **Use Descriptive Filenames**
   - Include timestamps: `snapshot-2026-01-15-10-30.json`
   - Include context: `snapshot-before-api-change.json`

3. **Redact Sensitive Data**
   - Always use `--redact` when sharing snapshots
   - Review snapshots before sharing with team members

4. **Focus Your Investigation**
   - Use `--resolver` to capture only relevant resolvers
   - Use `--ignore-unchanged` in diffs to reduce noise

5. **Automate Comparison**
   - Integrate snapshot diff in CI/CD pipelines
   - Alert on resolver status changes or performance regressions

## Common Issues and Solutions

### Issue: "No resolvers found in configuration"
**Solution:** Verify your config file path with `--config` flag and ensure it contains resolver definitions.

### Issue: Snapshot file is too large
**Solution:** Use `--resolver` to capture only specific resolvers, or implement resolver filtering in your configuration.

### Issue: Cannot see detailed error information
**Solution:** Use `--verbose` flag with `show` command to see phases, parameters, and failed attempts.

### Issue: Diff shows too many changes
**Solution:** Use `--ignore-fields duration,timestamp` to exclude time-based changes, or `--ignore-unchanged` to only see modified resolvers.

## Integration with CI/CD

Example GitHub Actions workflow:

```yaml
- name: Capture resolver snapshot
  run: scafctl snapshot save --output snapshot-${{ github.sha }}.json --redact "password|secret"

- name: Compare with baseline
  if: github.event_name == 'pull_request'
  run: |
    scafctl snapshot diff baseline.json snapshot-${{ github.sha }}.json \
      --ignore-fields duration,timestamp \
      --format json > diff.json
    
- name: Upload snapshot artifact
  uses: actions/upload-artifact@v3
  with:
    name: resolver-snapshot
    path: snapshot-*.json
```

## Additional Resources

- See [examples/resolvers/](../../../../examples/resolvers/) for sample configurations
- Review [docs/design/resolvers.md](../../../../docs/design/resolvers.md) for resolver architecture
- Check [docs/cel-global-environment-implementation.md](../../../../docs/cel-global-environment-implementation.md) for CEL expression debugging
