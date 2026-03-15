---
title: "CLI Contributing"
weight: 12
---

# CLI Implementation Guide

This document describes how to implement CLI commands in scafctl. It provides patterns, code examples, and best practices based on the existing codebase.

---

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Package Structure](#package-structure)
3. [Creating a New Command](#creating-a-new-command)
4. [Command Components](#command-components)
5. [Terminal Output](#terminal-output)
6. [Data Output with kvx](#data-output-with-kvx)
7. [Flags and Parameters](#flags-and-parameters)
8. [Context Management](#context-management)
9. [Testing Commands](#testing-commands)
10. [Common Patterns](#common-patterns)
11. [Checklist](#checklist)

---

## Architecture Overview

The scafctl CLI follows a kubectl-style command structure:

```text
scafctl <verb> <kind> <name[@version]> [flags]
```

The CLI is built using [Cobra](https://github.com/spf13/cobra) and organized into a hierarchical command tree:

```
root (scafctl)
├── version
├── get
│   ├── solution / solutions      # Get or list solutions
│   ├── provider / providers      # Get or list providers
│   └── catalog / catalogs        # Get or list catalogs
├── run
│   └── solution                  # Execute resolvers + actions
├── render
│   └── solution                  # Dry-run with options:
│                                 #   --graph: Show dependency graph
│                                 #   --snapshot: Save execution snapshot
├── build
│   ├── solution                  # Build solution into local catalog
│   └── plugin                    # Build plugin into local catalog
├── push
│   ├── solution                  # Push solution to remote catalog
│   └── plugin                    # Push plugin to remote catalog
├── pull
│   ├── solution                  # Pull solution from remote catalog
│   └── plugin                    # Pull plugin from remote catalog
├── inspect
│   ├── solution                  # Inspect solution metadata
│   └── plugin                    # Inspect plugin metadata/providers
├── tag
│   ├── solution                  # Tag solution version
│   └── plugin                    # Tag plugin version
├── save
│   ├── solution                  # Export solution to tar
│   └── plugin                    # Export plugin to tar
├── load                          # Import artifact from tar
├── explain
│   ├── solution                  # Explain solution metadata
│   └── provider                  # Explain provider schema
├── snapshot
│   ├── show                      # Display saved snapshot
│   └── diff                      # Compare two snapshots
├── delete
│   └── solution                  # Delete solution from catalog
├── plugins
│   ├── install                   # Pre-fetch plugin binaries from catalogs
│   └── list                      # List cached plugin binaries
└── config
    ├── view                      # View current config
    ├── get                       # Get a config value
    ├── set                       # Set a config value
    ├── unset                     # Remove a config value
    ├── add-catalog               # Add a catalog
    ├── remove-catalog            # Remove a catalog
    └── use-catalog               # Set default catalog
```

> **Note**: Singular and plural forms are supported for listing (e.g., `get solution` and `get solutions` both list all solutions when no name is provided).

---

## Package Structure

Commands live under `pkg/cmd/scafctl/`:

```
pkg/cmd/scafctl/
├── root.go              # Root command and global flags
├── root_test.go
├── flags/               # Shared flag helpers
│   ├── output.go        # kvx output flags
│   └── output_test.go
├── get/                 # 'get' verb
│   ├── get.go           # Parent command
│   ├── solution/        # 'get solution' subcommand
│   │   ├── solution.go
│   │   └── solution_test.go
│   ├── provider/        # 'get provider' subcommand
│   └── catalog/         # 'get catalog' subcommand
├── run/                 # 'run' verb
│   ├── run.go           # Parent command
│   ├── solution.go      # 'run solution' (resolvers + actions)
│   ├── solution_test.go
│   ├── common.go        # Shared helpers
│   ├── params.go        # Parameter parsing
│   └── progress.go      # Progress reporting
├── render/              # 'render' verb
│   ├── render.go
│   ├── solution.go      # 'render solution' (dry-run, --graph, --snapshot)
│   └── graph.go         # Graph rendering logic
├── build/               # 'build' verb (analogous to docker build)
│   ├── build.go
│   ├── solution.go      # 'build solution' to local catalog
│   └── plugin.go        # 'build plugin' to local catalog
├── push/                # 'push' verb (analogous to docker push)
│   ├── push.go
│   ├── solution.go      # 'push solution' to remote catalog
│   └── plugin.go        # 'push plugin' to remote catalog
├── pull/                # 'pull' verb (analogous to docker pull)
│   ├── pull.go
│   ├── solution.go      # 'pull solution' from remote catalog
│   └── plugin.go        # 'pull plugin' from remote catalog
├── inspect/             # 'inspect' verb
│   ├── inspect.go
│   ├── solution.go      # 'inspect solution' metadata
│   └── plugin.go        # 'inspect plugin' metadata/providers
├── tag/                 # 'tag' verb
│   ├── tag.go
│   ├── solution.go      # 'tag solution' create alias
│   └── plugin.go        # 'tag plugin' create alias
├── save/                # 'save' verb (analogous to docker save)
│   ├── save.go
│   ├── solution.go      # 'save solution' export to tar
│   └── plugin.go        # 'save plugin' export to tar
├── load/                # 'load' verb (analogous to docker load)
│   └── load.go          # 'load' import from tar
├── explain/             # 'explain' verb
│   ├── explain.go
│   ├── solution.go      # 'explain solution' metadata
│   └── provider.go      # 'explain provider' schema
├── snapshot/            # 'snapshot' verb (analysis only)
│   ├── snapshot.go
│   ├── show.go          # 'snapshot show' display saved snapshot
│   └── diff.go          # 'snapshot diff' compare snapshots
├── delete/              # 'delete' verb
│   ├── delete.go
│   └── solution.go      # 'delete solution' from catalog
├── config/              # 'config' verb
│   ├── config.go
│   ├── view.go
│   ├── get.go
│   ├── set.go
│   ├── unset.go
│   ├── add_catalog.go
│   ├── remove_catalog.go
│   └── use_catalog.go
└── version/
    ├── version.go
    └── version_test.go
```

### Naming Conventions

| File | Purpose |
|------|---------|
| `<verb>.go` | Parent command (e.g., `run.go`, `get.go`) |
| `<kind>.go` | Subcommand implementation (e.g., `solution.go`) |
| `<kind>_test.go` | Unit tests for the subcommand |
| `common.go` | Shared code within a verb package |
| `params.go` | Parameter/flag parsing logic |

---

## Creating a New Command

### Step 1: Create the Command Package

For a new verb `foo`:

```go
// pkg/cmd/scafctl/foo/foo.go
package foo

import (
    "fmt"

    "github.com/oakwood-commons/scafctl/pkg/settings"
    "github.com/oakwood-commons/scafctl/pkg/terminal"
    "github.com/spf13/cobra"
)

// CommandFoo creates the 'foo' command.
func CommandFoo(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
    cCmd := &cobra.Command{
        Use:     "foo",
        Aliases: []string{"f"},
        Short:   fmt.Sprintf("Does foo things with %s", settings.CliBinaryName),
        Long: `Longer description of what foo does.

SUBCOMMANDS:
  bar    Do bar things`,
        SilenceUsage: true,
    }

    // Add subcommands
    cCmd.AddCommand(CommandBar(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))

    return cCmd
}
```

### Step 2: Create the Subcommand

```go
// pkg/cmd/scafctl/foo/bar.go
package foo

import (
    "context"
    "fmt"
    "path/filepath"

    "github.com/oakwood-commons/scafctl/pkg/cmd/flags"
    "github.com/oakwood-commons/scafctl/pkg/logger"
    "github.com/oakwood-commons/scafctl/pkg/settings"
    "github.com/oakwood-commons/scafctl/pkg/terminal"
    "github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
    "github.com/oakwood-commons/scafctl/pkg/terminal/output"
    "github.com/oakwood-commons/scafctl/pkg/terminal/writer"
    "github.com/spf13/cobra"
)

// BarOptions holds configuration for the bar command.
type BarOptions struct {
    IOStreams  *terminal.IOStreams
    CliParams  *settings.Run
    
    // Command-specific flags
    File       string
    Verbose    bool
    
    // kvx output flags (for data-returning commands)
    flags.KvxOutputFlags
}

// CommandBar creates the 'foo bar' subcommand.
func CommandBar(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
    options := &BarOptions{}

    cCmd := &cobra.Command{
        Use:     "bar",
        Aliases: []string{"b"},
        Short:   "Do bar things",
        Long: `Detailed description of the bar command.

Examples:
  # Basic usage
  scafctl foo bar -f config.yaml

  # With verbose output
  scafctl foo bar -f config.yaml --verbose`,
        RunE: func(cCmd *cobra.Command, args []string) error {
            // Set up entry point path for tracing
            cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
            ctx := settings.IntoContext(context.Background(), cliParams)

            // Get logger from parent context
            lgr := logger.FromContext(cCmd.Context())
            if lgr != nil {
                ctx = logger.WithLogger(ctx, lgr)
            }

            // Get or create writer
            w := writer.FromContext(cCmd.Context())
            if w == nil {
                w = writer.New(ioStreams, cliParams)
            }
            ctx = writer.WithWriter(ctx, w)

            // Attach streams and params
            options.IOStreams = ioStreams
            options.CliParams = cliParams

            // Validate arguments if needed
            if err := output.ValidateCommands(args); err != nil {
                w.Error(err.Error())
                return err
            }

            // Validate output format
            if err := flags.ValidateKvxOutputFormat(options.Output); err != nil {
                w.Error(err.Error())
                return err
            }

            return options.Run(ctx)
        },
        SilenceUsage: true,
    }

    // Add flags
    cCmd.Flags().StringVarP(&options.File, "file", "f", "", "Path to config file")
    cCmd.Flags().BoolVar(&options.Verbose, "verbose", false, "Enable verbose output")
    
    // Add kvx output flags (-o, -i, -e)
    flags.AddKvxOutputFlagsToStruct(cCmd, &options.KvxOutputFlags)

    return cCmd
}

// Run executes the bar command.
func (o *BarOptions) Run(ctx context.Context) error {
    lgr := logger.FromContext(ctx)
    lgr.V(1).Info("running bar command", "file", o.File, "verbose", o.Verbose)

    // Your command logic here...
    results := map[string]any{
        "status": "success",
        "file":   o.File,
    }

    return o.writeOutput(ctx, results)
}

// writeOutput writes results using kvx infrastructure.
func (o *BarOptions) writeOutput(ctx context.Context, data any) error {
    kvxOpts := flags.NewKvxOutputOptionsFromFlags(
        o.Output,
        o.Interactive,
        o.Expression,
        kvx.WithOutputContext(ctx),
        kvx.WithOutputNoColor(o.CliParams.NoColor),
        kvx.WithOutputAppName("scafctl foo bar"),
    )
    kvxOpts.IOStreams = o.IOStreams

    return kvxOpts.Write(data)
}
```

### Step 3: Register with Root Command

```go
// pkg/cmd/scafctl/root.go

import (
    // ... existing imports
    "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/foo"
)

func Root() *cobra.Command {
    // ... existing setup

    cCmd.AddCommand(foo.CommandFoo(cliParams, ioStreams, settings.CliBinaryName))
    
    return cCmd
}
```

---

## Command Components

### Options Struct

Every command should have an options struct that holds:

1. **IOStreams** and **CliParams** - Required for output handling
2. **Command-specific flags** - With JSON/YAML tags and doc annotations
3. **KvxOutputFlags** (optional) - For commands returning structured data

```go
type CommandOptions struct {
    IOStreams  *terminal.IOStreams
    CliParams  *settings.Run
    
    // Flags with proper tags (see Struct Tags section below)
    File       string   `json:"file,omitempty" yaml:"file,omitempty" doc:"Path to file" example:"/path/to/file" maxLength:"4096"`
    Timeout    time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty" doc:"Operation timeout" example:"30s"`
    Items      []string `json:"items,omitempty" yaml:"items,omitempty" doc:"List of items" maxItems:"100"`
    
    // For data-returning commands
    flags.KvxOutputFlags
    
    // For testing dependency injection
    getter SomeInterface
}
```

### Struct Tags

Always add JSON/YAML tags and [Huma validation tags](https://huma.rocks/features/request-validation/#validation-tags):

| Field Type | Required Tags |
|------------|---------------|
| All fields | `doc` |
| Strings | `maxLength`, `example`, `pattern` (optional), `patternDescription` (optional) |
| Integers | `maximum`, `example` |
| Arrays | `maxItems` (no `example`) |
| Objects/maps | No `example` tag |

### Run Method

The `Run` method contains the main command logic:

```go
func (o *CommandOptions) Run(ctx context.Context) error {
    lgr := logger.FromContext(ctx)
    lgr.V(1).Info("starting command", "file", o.File)

    // 1. Load/validate input
    data, err := o.loadData(ctx)
    if err != nil {
        return fmt.Errorf("failed to load data: %w", err)
    }

    // 2. Execute main logic
    result, err := o.process(ctx, data)
    if err != nil {
        return fmt.Errorf("processing failed: %w", err)
    }

    // 3. Write output
    return o.writeOutput(ctx, result)
}
```

---

## Terminal Output

### Using Writer

The `writer` package provides centralized terminal output. **Never use `fmt.Fprintf` directly**.

```go
import "github.com/oakwood-commons/scafctl/pkg/terminal/writer"

func (o *Options) Run(ctx context.Context) error {
    w := writer.FromContext(ctx)
    if w == nil {
        return fmt.Errorf("writer not initialized in context")
    }
    
    // Success message (respects --quiet and --no-color)
    w.Success("Operation completed")
    w.Successf("Created %d items", count)
    
    // Warning message (respects --quiet and --no-color)
    w.Warning("This is deprecated")
    w.Warningf("File %s not found, using default", path)
    
    // Error message (always shown, respects --no-color)
    w.Error("Something went wrong")
    w.Errorf("Failed to open %s: %v", path, err)
    
    // Info message (respects --quiet and --no-color)
    w.Info("Processing started")
    w.Infof("Found %d files", count)
    
    // Debug message (respects --quiet and log level)
    w.Debug("Internal state")
    w.Debugf("Value: %v", value)
    
    // Plain output (respects --quiet only)
    w.Plainln("Raw output line")
    w.Plainlnf("Count: %d", n)
    
    // Error with exit
    w.ErrorWithExit("Fatal error")        // exits with code 1
    w.ErrorWithCode(2, "Validation failed") // exits with specified code
    
    return nil
}
```

### Writer Methods

| Method | Respects `--quiet` | Respects `--no-color` | Output Stream |
|--------|-------------------|----------------------|---------------|
| `Success` / `Successf` | ✅ | ✅ | stdout |
| `Warning` / `Warningf` | ✅ | ✅ | stdout |
| `Error` / `Errorf` | ❌ | ✅ | stderr |
| `Info` / `Infof` | ✅ | ✅ | stdout |
| `Debug` / `Debugf` | ✅ | ✅ | stdout |
| `Plain` / `Plainln` | ✅ | ❌ | stdout |

### Creating Writer in Tests

```go
func TestCommand(t *testing.T) {
    streams, outBuf, errBuf := terminal.NewTestIOStreams()
    cliParams := settings.NewCliParams()
    
    // Create writer with test exit function
    var exitCode int
    w := writer.New(streams, cliParams, writer.WithExitFunc(func(code int) {
        exitCode = code
    }))
    
    ctx := writer.WithWriter(context.Background(), w)
    
    // Run command...
    
    // Verify output
    assert.Contains(t, outBuf.String(), "expected output")
    assert.Equal(t, 1, exitCode)
}
```

---

## Data Output with kvx

For commands that return structured data, use the **kvx** package for flexible output:

### Adding kvx Flags

```go
import "github.com/oakwood-commons/scafctl/pkg/cmd/flags"

type Options struct {
    // ... other fields
    flags.KvxOutputFlags  // Embeds Output, Interactive, Expression
}

func CommandFoo(...) *cobra.Command {
    options := &Options{}
    
    cCmd := &cobra.Command{...}
    
    // Add -o/--output, -i/--interactive, -e/--expression flags
    flags.AddKvxOutputFlagsToStruct(cCmd, &options.KvxOutputFlags)
    
    return cCmd
}
```

### Writing kvx Output

```go
import (
    "github.com/oakwood-commons/scafctl/pkg/cmd/flags"
    "github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
)

func (o *Options) writeOutput(ctx context.Context, data any) error {
    kvxOpts := flags.NewKvxOutputOptionsFromFlags(
        o.Output,       // "table", "json", "yaml", "quiet"
        o.Interactive,  // Launch TUI
        o.Expression,   // CEL filter expression
        
        // Optional configuration
        kvx.WithOutputContext(ctx),
        kvx.WithOutputNoColor(o.CliParams.NoColor),
        kvx.WithOutputAppName("scafctl foo bar"),
        kvx.WithOutputHelp("Results", []string{
            "Navigate: ↑↓ arrows",
            "Search: /",
            "Quit: q",
        }),
    )
    kvxOpts.IOStreams = o.IOStreams

    return kvxOpts.Write(data)
}
```

### Output Formats

| Format | Flag | Description |
|--------|------|-------------|
| `table` | `-o table` | Interactive table view (default for terminals) |
| `json` | `-o json` | JSON output for piping |
| `yaml` | `-o yaml` | YAML output for piping |
| `quiet` | `-o quiet` | No output, exit code only |

### Interactive Mode

Enable with `-i` or `--interactive`:

```bash
scafctl run solution -f config.yaml -i
```

### CEL Filtering

Use `-e` or `--expression` to filter/transform output:

```bash
# Select specific field
scafctl run solution -f config.yaml -e '_.database'

# Filter array
scafctl run solution -f config.yaml -e '_.items.filter(x, x.enabled)'

# Compute values
scafctl run solution -f config.yaml -e 'size(_.results)'
```

---

## Flags and Parameters

### Standard Flags

```go
// String flag
cCmd.Flags().StringVarP(&options.File, "file", "f", "", "Path to file")

// Bool flag
cCmd.Flags().BoolVar(&options.Verbose, "verbose", false, "Enable verbose")

// Duration flag
cCmd.Flags().DurationVar(&options.Timeout, "timeout", 30*time.Second, "Timeout")

// String slice (repeatable)
cCmd.Flags().StringArrayVarP(&options.Params, "param", "p", nil, "Parameters")

// Int flag
cCmd.Flags().Int64Var(&options.MaxSize, "max-size", 1024*1024, "Max size in bytes")
```

### Key-Value Flags

For `key=value` parameters, use the flags package:

```go
import "github.com/oakwood-commons/scafctl/pkg/flags"

// In command setup
cCmd.Flags().StringArrayVarP(&options.Params, "param", "p", nil, 
    "Parameters (key=value or @file.yaml)")

// In Run method
params, err := flags.ParseKeyValueCSV(options.Params)
if err != nil {
    return fmt.Errorf("invalid parameters: %w", err)
}
```

Supported formats:
- `key=value` - Simple key-value
- `key=val1,key=val2` - Multiple values (becomes array)
- `@file.yaml` - Load from file
- `"key=value with spaces"` - Quoted values

### Validating Input Keys Against a Schema

When a command accepts dynamic `key=value` inputs and has a known set of valid keys
(e.g. from a provider's JSON Schema or a solution's parameter resolvers), use
`flags.ValidateInputKeys` for early detection of typos:

```go
import "github.com/oakwood-commons/scafctl/pkg/flags"

// After parsing inputs and looking up valid keys
validKeys := []string{"url", "method", "headers", "body", "timeout"}
if err := flags.ValidateInputKeys(inputs, validKeys, `provider "http"`); err != nil {
    // Error: provider "http" does not accept input "urll" — did you mean "url"?
    return err
}
```

This uses Levenshtein distance to suggest the closest valid key when a typo is detected.

### Hidden Flags

```go
cCmd.Flags().String("internal-flag", "", "For internal use")
if err := cCmd.Flags().MarkHidden("internal-flag"); err != nil {
    return nil
}
```

---

## Context Management

### Setting Up Context

```go
func RunE(cCmd *cobra.Command, args []string) error {
    // 1. Create base context with settings
    cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
    ctx := settings.IntoContext(context.Background(), cliParams)

    // 2. Attach logger from parent (or create new)
    lgr := logger.FromContext(cCmd.Context())
    if lgr != nil {
        ctx = logger.WithLogger(ctx, lgr)
    }

    // 3. Attach writer
    w := writer.FromContext(cCmd.Context())
    if w == nil {
        w = writer.New(ioStreams, cliParams)
    }
    ctx = writer.WithWriter(ctx, w)

    return options.Run(ctx)
}
```

### Accessing Context Values

```go
func (o *Options) Run(ctx context.Context) error {
    // Logger
    lgr := logger.FromContext(ctx)
    lgr.V(1).Info("message", "key", value)

    // Writer
    w := writer.FromContext(ctx)
    if w == nil {
        return fmt.Errorf("writer not initialized in context")
    }
    w.Success("Done")

    // Settings
    settings := settings.FromContext(ctx)
}
```

---

## Testing Commands

### Basic Command Test

```go
func TestCommandFoo(t *testing.T) {
    t.Parallel()

    streams, _, _ := terminal.NewTestIOStreams()
    cliParams := settings.NewCliParams()

    cmd := CommandFoo(cliParams, streams, "")

    // Verify command setup
    assert.Equal(t, "foo", cmd.Use)
    assert.NotEmpty(t, cmd.Short)

    // Verify flags
    flags := cmd.Flags()
    assert.NotNil(t, flags.Lookup("file"))
    assert.NotNil(t, flags.Lookup("output"))
}
```

### Testing Command Execution

```go
func TestFooOptions_Run(t *testing.T) {
    t.Parallel()

    // Create test streams
    var stdout, stderr bytes.Buffer
    streams := &terminal.IOStreams{
        In:           io.NopCloser(bytes.NewReader(nil)),
        Out:          &stdout,
        ErrOut:       &stderr,
        ColorEnabled: false,
    }

    // Create test context
    lgr := logger.Get(0)
    ctx := logger.WithLogger(context.Background(), lgr)
    
    cliParams := settings.NewCliParams()
    w := writer.New(streams, cliParams)
    ctx = writer.WithWriter(ctx, w)

    // Set up options with test dependencies
    options := &FooOptions{
        IOStreams: streams,
        CliParams: cliParams,
        File:      "/path/to/file",
        KvxOutputFlags: flags.KvxOutputFlags{
            Output: "json",
        },
        // Inject mock dependencies
        getter: &mockGetter{...},
    }

    // Run and verify
    err := options.Run(ctx)
    require.NoError(t, err)

    // Check output
    assert.Contains(t, stdout.String(), `"status":"success"`)
}
```

### Testing with Exit Capture

```go
func TestErrorWithExit(t *testing.T) {
    streams, _, errBuf := terminal.NewTestIOStreams()
    cliParams := settings.NewCliParams()

    var exitCode int
    w := writer.New(streams, cliParams, writer.WithExitFunc(func(code int) {
        exitCode = code
    }))

    w.ErrorWithExit("fatal error")

    assert.Equal(t, 1, exitCode)
    assert.Contains(t, errBuf.String(), "fatal error")
}
```

### Testing Flag Defaults

```go
func TestCommandFoo_FlagDefaults(t *testing.T) {
    t.Parallel()

    streams, _, _ := terminal.NewTestIOStreams()
    cliParams := settings.NewCliParams()

    cmd := CommandFoo(cliParams, streams, "")
    flags := cmd.Flags()

    file, err := flags.GetString("file")
    require.NoError(t, err)
    assert.Empty(t, file)

    timeout, err := flags.GetDuration("timeout")
    require.NoError(t, err)
    assert.Equal(t, 30*time.Second, timeout)

    output, err := flags.GetString("output")
    require.NoError(t, err)
    assert.Equal(t, "table", output)
}
```

### Mock Dependency Injection

```go
// In options struct
type FooOptions struct {
    // ... flags
    
    // For testing
    getter GetterInterface
}

// In Run method
func (o *FooOptions) Run(ctx context.Context) error {
    getter := o.getter
    if getter == nil {
        getter = NewDefaultGetter()  // Production default
    }
    
    data, err := getter.Get(ctx, o.File)
    // ...
}

// In tests
func TestWithMock(t *testing.T) {
    options := &FooOptions{
        getter: &mockGetter{
            data: testData,
        },
    }
    // ...
}
```

---

## Common Patterns

### Exit Codes

Define and use consistent exit codes:

```go
const (
    ExitSuccess          = 0
    ExitGeneralError     = 1
    ExitValidationFailed = 2
    ExitInvalidInput     = 3
    ExitFileNotFound     = 4
)

func (o *Options) exitWithCode(err error, code int) error {
    // Could log the code or set process exit code
    return err
}
```

### Shared RunE Factory

For consistent command setup across related subcommands:

```go
// common.go
type runCommandConfig struct {
    cliParams     *settings.Run
    ioStreams     *terminal.IOStreams
    path          string
    runner        interface{ Run(context.Context) error }
    getOutputFn   func() string
    setIOStreamFn func(*terminal.IOStreams, *settings.Run)
}

func makeRunEFunc(cfg runCommandConfig, cmdUse string) func(*cobra.Command, []string) error {
    return func(cCmd *cobra.Command, args []string) error {
        cfg.cliParams.EntryPointSettings.Path = filepath.Join(cfg.path, cmdUse)
        ctx := settings.IntoContext(context.Background(), cfg.cliParams)

        lgr := logger.FromContext(cCmd.Context())
        if lgr != nil {
            ctx = logger.WithLogger(ctx, lgr)
        }

        w := writer.FromContext(cCmd.Context())
        if w == nil {
            w = writer.New(cfg.ioStreams, cfg.cliParams)
        }
        ctx = writer.WithWriter(ctx, w)

        cfg.setIOStreamFn(cfg.ioStreams, cfg.cliParams)

        if err := output.ValidateCommands(args); err != nil {
            w.Error(err.Error())
            return err
        }

        return cfg.runner.Run(ctx)
    }
}
```

### Reading from File or Stdin

```go
func (o *Options) loadData(ctx context.Context) ([]byte, error) {
    // Handle stdin
    if o.File == "-" {
        data, err := io.ReadAll(o.IOStreams.In)
        if err != nil {
            return nil, fmt.Errorf("failed to read from stdin: %w", err)
        }
        return data, nil
    }

    // Handle file
    if o.File != "" {
        data, err := os.ReadFile(o.File)
        if err != nil {
            return nil, fmt.Errorf("failed to read file: %w", err)
        }
        return data, nil
    }

    // Auto-discovery
    return o.autoDiscover(ctx)
}
```

### Progress Reporting

For long-running operations:

```go
if o.Progress {
    progress := NewProgressReporter(o.IOStreams.ErrOut, totalItems)
    defer progress.Wait()
    
    // Update progress
    progress.Update(itemName, "processing")
    progress.Complete(itemName)
}
```

---

## Checklist

Before submitting a new command:

- [ ] Options struct has proper JSON/YAML tags and Huma validation tags
- [ ] Command has `Use`, `Aliases`, `Short`, `Long`, and examples
- [ ] `SilenceUsage: true` is set
- [ ] Logger is retrieved from context: `logger.FromContext(ctx)`
- [ ] Writer is used for all terminal output (no `fmt.Fprintf`)
- [ ] Errors are wrapped with context: `fmt.Errorf("context: %w", err)`
- [ ] Data output uses kvx for structured data
- [ ] Unit tests cover command setup and flag defaults
- [ ] Unit tests cover main execution paths
- [ ] Tests use dependency injection for external services
- [ ] `golangci-lint run` passes
- [ ] Command is registered in parent command
- [ ] Documentation in `Long` field includes examples

---

## Quick Reference

### Imports

```go
import (
    "github.com/oakwood-commons/scafctl/pkg/cmd/flags"       // Shared flag helpers
    "github.com/oakwood-commons/scafctl/pkg/logger"          // Logging
    "github.com/oakwood-commons/scafctl/pkg/settings"        // CLI settings
    "github.com/oakwood-commons/scafctl/pkg/terminal"        // IOStreams
    "github.com/oakwood-commons/scafctl/pkg/terminal/kvx"    // Data output
    "github.com/oakwood-commons/scafctl/pkg/terminal/output" // Validation helpers
    "github.com/oakwood-commons/scafctl/pkg/terminal/writer" // Terminal writer
    "github.com/spf13/cobra"                                 // CLI framework
)
```

### Minimal Command Template

```go
package mycommand

import (
    "context"
    "path/filepath"

    "github.com/oakwood-commons/scafctl/pkg/logger"
    "github.com/oakwood-commons/scafctl/pkg/settings"
    "github.com/oakwood-commons/scafctl/pkg/terminal"
    "github.com/oakwood-commons/scafctl/pkg/terminal/writer"
    "github.com/spf13/cobra"
)

type Options struct {
    IOStreams *terminal.IOStreams
    CliParams *settings.Run
    Name      string `json:"name" yaml:"name" doc:"Name" example:"example" maxLength:"255"`
}

func Command(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
    opts := &Options{}
    
    cmd := &cobra.Command{
        Use:          "mycommand",
        Short:        "Does something",
        SilenceUsage: true,
        RunE: func(cmd *cobra.Command, args []string) error {
            cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Use)
            ctx := settings.IntoContext(context.Background(), cliParams)
            
            if lgr := logger.FromContext(cmd.Context()); lgr != nil {
                ctx = logger.WithLogger(ctx, lgr)
            }
            
            w := writer.FromContext(cmd.Context())
            if w == nil {
                w = writer.New(ioStreams, cliParams)
            }
            ctx = writer.WithWriter(ctx, w)
            
            opts.IOStreams = ioStreams
            opts.CliParams = cliParams
            
            return opts.Run(ctx)
        },
    }
    
    cmd.Flags().StringVarP(&opts.Name, "name", "n", "", "Name to use")
    
    return cmd
}

func (o *Options) Run(ctx context.Context) error {
    w := writer.FromContext(ctx)
    if w == nil {
        return fmt.Errorf("writer not initialized in context")
    }
    w.Successf("Hello, %s!", o.Name)
    return nil
}
```

---

## Implementation Priority

Recommended order for implementing CLI changes:

### Phase 1: Core Fixes (High Priority) ✅ COMPLETED

1. **Fix `run solution`** - Execute resolvers AND actions (not just resolvers) ✅
2. **Remove `run workflow`** - Merge functionality into `run solution` ✅
3. **Add singular/plural aliases** - `solution`/`solutions`, `provider`/`providers`, `catalog`/`catalogs`

### Phase 2: Render Enhancements

4. **Add `--graph` to `render solution`** - Move from `resolver graph`
5. **Add `--snapshot` to `render solution`** - Move from `snapshot save`
6. **Remove old commands** - Delete `resolver graph`, `snapshot save`

### Phase 3: Discovery Commands

7. **Implement `get providers`** - List all registered providers
8. **Implement `explain provider`** - Show provider schema/docs
9. **Implement `explain solution`** - Show solution metadata

### Phase 4: Configuration

10. **Implement `config view`** - View current configuration
11. **Implement `config get/set/unset`** - Manage config values
12. **Implement catalog config commands** - `add-catalog`, `remove-catalog`, `use-catalog`

### Phase 5: Catalog Integration

13. **Implement `get catalogs`** - List configured catalogs
14. **Implement `get catalog`** - Show catalog details
15. **Implement `get solutions`** - List solutions from catalog
16. **Implement `publish solution`** - Publish to catalog
17. **Implement `delete solution`** - Remove from catalog
18. **Add `name@version` resolution** - Catalog lookup for all commands

---

## Command Implementation Status

This section tracks which commands from the design are implemented and what work remains.

### Legend

| Status | Meaning |
|--------|---------|
| ✅ | Implemented |
| ⚠️ | Partially implemented (needs changes) |
| ❌ | Not implemented |

### Current Status

| Command | Status | Notes |
|---------|--------|-------|
| `version` | ✅ | Complete |
| `get solution` | ✅ | Complete with catalog support |
| `get solutions` | ✅ | List all solutions (plural alias) |
| `get provider` | ✅ | Show provider metadata |
| `get providers` | ✅ | List all registered providers (plural alias) |
| `get resolver` | ✅ | Show resolver details |
| `get authhandler` | ✅ | Show auth handler details |
| `get celfunction` | ✅ | Show CEL function details |
| `run solution` | ✅ | Executes resolvers AND actions |
| `run resolver` | ✅ | Executes resolvers only (for debugging and inspection) |
| `render solution` | ✅ | Includes `--graph`, `--action-graph`, `--snapshot`, `--redact` flags |
| `build solution` | ✅ | Build solution into local catalog |
| `catalog push` | ✅ | Push artifacts to remote catalog |
| `catalog pull` | ✅ | Pull artifacts from remote catalog |
| `catalog list` | ✅ | List catalog contents |
| `catalog inspect` | ✅ | Inspect artifact metadata |
| `catalog delete` | ✅ | Delete artifact from catalog |
| `catalog prune` | ✅ | Prune unused catalog entries |
| `catalog tag` | ✅ | Create version aliases |
| `catalog save` | ✅ | Export artifact to tar |
| `catalog load` | ✅ | Import artifact from tar |
| `explain solution` | ✅ | Show solution metadata |
| `explain provider` | ✅ | Show provider schema/docs |
| `snapshot show` | ✅ | Display saved snapshot |
| `snapshot diff` | ✅ | Compare two snapshots |
| `config view` | ✅ | View current configuration |
| `config get` | ✅ | Get specific config value |
| `config set` | ✅ | Set config value |
| `config unset` | ✅ | Remove config value |
| `config add-catalog` | ✅ | Add catalog configuration |
| `config remove-catalog` | ✅ | Remove catalog |
| `config use-catalog` | ✅ | Set default catalog |
| `config init` | ✅ | Initialize configuration |
| `config schema` | ✅ | Show config schema |
| `config validate` | ✅ | Validate config file |
| `eval cel` | ✅ | Evaluate CEL expressions |
| `eval template` | ✅ | Evaluate Go templates |
| `eval validate` | ✅ | Validate expressions |
| `new solution` | ✅ | Scaffold new solution |
| `lint` | ✅ | Lint solution files |
| `lint rules` | ✅ | List lint rules |
| `lint explain` | ✅ | Explain a lint rule |
| `test functional` | ✅ | Run functional tests |
| `test list` | ✅ | List test cases |
| `test init` | ✅ | Scaffold test suite |
| `examples list` | ✅ | List available examples |
| `examples get` | ✅ | Get an example |
| `bundle verify` | ✅ | Verify bundle integrity |
| `bundle diff` | ✅ | Diff two bundles |
| `bundle extract` | ✅ | Extract bundle contents |
| `vendor update` | ✅ | Update vendored dependencies |
| `secrets list` | ✅ | List secrets |
| `secrets get` | ✅ | Get a secret |
| `secrets set` | ✅ | Set a secret |
| `secrets delete` | ✅ | Delete a secret |
| `secrets exists` | ✅ | Check if a secret exists |
| `secrets export` | ✅ | Export secrets |
| `secrets import` | ✅ | Import secrets |
| `secrets rotate` | ✅ | Rotate encryption key |
| `auth login` | ✅ | Authenticate with a handler |
| `auth logout` | ✅ | Clear stored credentials |
| `auth status` | ✅ | Show auth status |
| `auth token` | ✅ | Get an access token |
| `auth list` | ✅ | List auth handlers |
| `mcp serve` | ✅ | Start MCP server |
| `cache clear` | ✅ | Clear caches |

### Commands Removed/Refactored

| Command | Action | Reason |
|---------|--------|--------|
| `run workflow` | ✅ **Removed** | Merged into `run solution` (solution now runs resolvers + actions) |
| `snapshot save` | ✅ **Removed** | Replaced with `render solution --snapshot` |
| `resolver graph` | ✅ **Removed** | Replaced with `render solution --graph` |

### Code Changes Required

#### 1. Fix `run solution` ✅ COMPLETED

**Previous behavior**: Executed resolvers only, output resolver results.

**New behavior**: Executes resolvers AND actions. Per design doc:
> "Execute a solution's resolver and perform its actions."

**Changes made**:
- Removed `pkg/cmd/scafctl/run/workflow.go` (merged into solution.go)
- Updated `run/solution.go` to execute actions after resolvers complete
- Added `run resolver` command for resolver-only execution (debugging/inspection)
- Added `--dry-run` flag to show what would execute
- Added `--action-timeout` and `--max-action-concurrency` flags
- Actions run using the action executor with resolver results in context

```go
// After resolver execution succeeds:
if sol.Spec.HasWorkflow() {
    actionExecutor := action.NewExecutor(...)
    result, err := actionExecutor.Execute(ctx, sol.Spec.Workflow)
    // ...
}
```

#### 2. Add `--graph` and `--snapshot` to `render solution`

**Changes needed**:
- Move graph logic from `pkg/cmd/scafctl/resolver/graph.go` to `pkg/cmd/scafctl/render/`
- Remove `pkg/cmd/scafctl/resolver/` directory
- Remove `pkg/cmd/scafctl/snapshot/save.go`
- Add flags to `render solution`:

```go
type RenderOptions struct {
    // ... existing fields
    
    // Graph mode - show dependency graph instead of rendering
    Graph       bool   `json:"graph,omitempty" yaml:"graph,omitempty" doc:"Show dependency graph"`
    GraphFormat string `json:"graphFormat,omitempty" yaml:"graphFormat,omitempty" doc:"Graph format: ascii, dot, mermaid, json" example:"ascii" maxLength:"10"`
    
    // Snapshot mode - save execution snapshot to file
    Snapshot    string `json:"snapshot,omitempty" yaml:"snapshot,omitempty" doc:"Save snapshot to file" maxLength:"4096"`
    Redact      bool   `json:"redact,omitempty" yaml:"redact,omitempty" doc:"Redact sensitive values in snapshot"`
}
```

**Examples**:
```bash
# Normal render (resolvers + action preview)
scafctl render solution -f solution.yaml

# Show dependency graph
scafctl render solution -f solution.yaml --graph
scafctl render solution -f solution.yaml --graph --graph-format dot | dot -Tpng > graph.png
scafctl render solution -f solution.yaml --graph --graph-format mermaid

# Save snapshot
scafctl render solution -f solution.yaml --snapshot output.json
scafctl render solution -f solution.yaml --snapshot output.json --redact
```

#### 3. Implement `get provider` / `get providers`

```go
// pkg/cmd/scafctl/get/provider/provider.go

func CommandProvider(...) *cobra.Command {
    // get provider <name> - show provider details
    // get provider (no name) OR get providers - list all
}
```

**Output for `get provider <name>`**:
- Provider name and version
- Description
- Supported operations
- Configuration schema
- Example usage

**Output for `get providers`**:
- Table of all registered providers with name, version, description

#### 4. Implement Singular/Plural Aliases

Use Cobra aliases for plural forms:

```go
func CommandSolution(...) *cobra.Command {
    cmd := &cobra.Command{
        Use:     "solution [name[@version]]",
        Aliases: []string{"solutions"},  // Plural alias
        Short:   "Get solution(s)",
        // ...
    }
    // ...
}
```

Apply to all `get` subcommands:
- `solution` / `solutions`
- `provider` / `providers`
- `catalog` / `catalogs`

#### 5. Implement `get catalog` / `get catalogs`

```go
// pkg/cmd/scafctl/get/catalog/catalog.go

func CommandCatalog(...) *cobra.Command {
    // get catalog <name> - show catalog details
    // get catalog (no name) OR get catalogs - list all configured
}
```

#### 6. Implement Catalog Artifact Commands

##### `build solution` / `build plugin`

```go
// pkg/cmd/scafctl/build/solution.go

type BuildSolutionOptions struct {
    File string // -f flag
}
```

**Flags**:
- `-f, --file` - Solution/plugin file path

**Examples**:
```bash
scafctl build solution -f ./solution.yaml
scafctl build plugin -f ./plugin-config.yaml
```

##### `push solution` / `push plugin`

```go
// pkg/cmd/scafctl/push/solution.go

type PushOptions struct {
    Name    string // From args (name@version)
    Catalog string // --catalog for target
}
```

**Flags**:
- `--catalog` - Target catalog for publishing

**Examples**:
```bash
scafctl push solution my-solution@1.7.0
scafctl push plugin aws-provider@1.5.0
scafctl push solution my-solution@1.7.0 --catalog=production
```

##### `pull solution` / `pull plugin`

```go
// pkg/cmd/scafctl/pull/solution.go

type PullOptions struct {
    Name string // From args (name@version)
}
```

**Examples**:
```bash
scafctl pull solution example@1.7.0
scafctl pull plugin aws-provider@1.5.0
```

##### `inspect solution` / `inspect plugin`

```go
// pkg/cmd/scafctl/inspect/solution.go

type InspectOptions struct {
    Name   string // From args (name@version)
    Output string // Output format
}
```

**Examples**:
```bash
scafctl inspect solution example@1.7.0
scafctl inspect plugin aws-provider@1.5.0
```

##### `tag solution` / `tag plugin`

```go
// pkg/cmd/scafctl/tag/solution.go

type TagOptions struct {
    Source string // Source reference
    Target string // Target tag
}
```

**Examples**:
```bash
scafctl tag solution my-solution@1.2.3 my-solution:latest
scafctl tag plugin aws-provider@1.5.0 aws-provider:stable
```

##### `save solution` / `save plugin`

```go
// pkg/cmd/scafctl/save/solution.go

type SaveOptions struct {
    Name   string // From args (name@version)
    Output string // -o flag for output file
}
```

**Examples**:
```bash
scafctl save solution my-solution@1.2.3 -o solution.tar
scafctl save plugin aws-provider@1.5.0 -o plugin.tar
```

##### `load`

```go
// pkg/cmd/scafctl/load/load.go

type LoadOptions struct {
    Input string // -i flag for input file
}
```

**Examples**:
```bash
scafctl load -i solution.tar
scafctl load -i plugin.tar
```

#### 7. Implement `explain solution`

```go
// pkg/cmd/scafctl/explain/solution.go

type ExplainSolutionOptions struct {
    File   string // -f flag for local file
    Name   string // Solution name from catalog
    Output string // Output format
}
```

**Output for `explain solution`**:
- Name, version, description
- List of resolvers with their providers
- List of actions with types
- Required parameters (from parameter provider usage)
- Dependencies between resolvers (summary)

**Examples**:
```bash
scafctl explain solution -f solution.yaml
scafctl explain solution example
scafctl explain solution example@1.0.0
```

#### 8. Implement `explain provider`

```go
// pkg/cmd/scafctl/explain/provider.go

type ExplainOptions struct {
    ProviderName string
}
```

**Output**: Detailed documentation for a provider including:
- Description
- Configuration schema with types and validation
- Example configurations
- Supported features

#### 9. Implement `config` Commands

```go
// pkg/cmd/scafctl/config/config.go

func CommandConfig(...) *cobra.Command {
    cmd.AddCommand(CommandView(...))
    cmd.AddCommand(CommandGet(...))
    cmd.AddCommand(CommandSet(...))
    cmd.AddCommand(CommandUnset(...))
    cmd.AddCommand(CommandAddCatalog(...))
    cmd.AddCommand(CommandRemoveCatalog(...))
    cmd.AddCommand(CommandUseCatalog(...))
}
```

**Config file location**: `~/.scafctl/config.yaml`

**Config structure**:
```yaml
catalogs:
  - name: default
    type: filesystem
    path: ./
  - name: internal
    type: oci
    url: oci://registry.example.com/scafctl
settings:
  defaultCatalog: default
```

#### 10. Implement `delete solution`

```go
// pkg/cmd/scafctl/delete/solution.go

type DeleteSolutionOptions struct {
    Name    string // Solution name (from args)
    Version string // Version (parsed from name@version)
    Catalog string // --catalog flag
    Force   bool   // --force skip confirmation
}
```

**Flags**:
- `--catalog` - Target catalog (inherited from global)
- `--force` - Skip confirmation prompt

**Examples**:
```bash
scafctl delete solution example@1.7.0
scafctl delete solution example@1.7.0 --catalog=staging
scafctl delete solution example@1.7.0 --force
```

**Behavior**:
- Requires `name@version` (cannot delete all versions at once)
- Prompts for confirmation unless `--force`
- Requires catalog support

### Catalog Dependency

Many commands require catalog functionality for `name@version` resolution:

| Command | Catalog Required? |
|---------|-------------------|
| `run solution -f file.yaml` | No |
| `run solution example` | Yes (lookup by name) |
| `run solution example@1.0.0` | Yes (lookup by name + version) |
| `get solutions` | Yes (list from catalog) |
| `publish solution` | Yes (publish to catalog) |
| `delete solution` | Yes (delete from catalog) |

**Recommendation**: Implement CLI structure first with file-based operations (`-f` flag), then add catalog support. Commands requiring catalog can return helpful errors:

```go
if o.File == "" && name != "" {
    return fmt.Errorf("catalog lookup not yet implemented; use -f flag to specify a file")
}
```

---

## Global Flags

These flags should be available on the root command and inherited by all subcommands:

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--cwd` | `-C` | string | `""` | Change the working directory before executing the command (similar to `git -C`) |
| `--catalog` | | string | `""` | Target a specific configured catalog |
| `--output` | `-o` | string | `table` | Output format: `table`, `json`, `yaml`, `quiet` |
| `--quiet` | `-q` | bool | `false` | Suppress non-essential output |
| `--no-color` | | bool | `false` | Disable colored output |
| `--config` | | string | `~/.scafctl/config.yaml` | Path to config file |
| `--log-level` | | string | `none` | Log level: none, error, warn, info, debug, trace, or numeric V-level |
| `--debug` | `-d` | bool | `false` | Enable debug logging (shorthand for --log-level debug) |
| `--log-format` | | string | `console` | Log format: console (colored) or json (structured) |
| `--log-file` | | string | `""` | Write logs to a file path |

### Adding Global Flags

Global flags are defined in `root.go`:

```go
func Root() *cobra.Command {
    cCmd.PersistentFlags().StringVar(&cliParams.Catalog, "catalog", "", 
        "Target a specific configured catalog")
    cCmd.PersistentFlags().StringVar(&cliParams.ConfigFile, "config", "", 
        "Path to config file (default: ~/.scafctl/config.yaml)")
    // ... existing flags
}
```