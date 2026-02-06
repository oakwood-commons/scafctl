# CLI Output Writer Implementation Plan

## Overview

This document outlines the implementation plan for creating a centralized **CLI Output Writer** system for `scafctl`. The goal is to provide a single, consistent interface for all CLI commands to write general errors, messages, warnings, and informational output to the terminal.

## Problem Statement

Currently, output handling in `scafctl` is fragmented across multiple approaches:

1. **Direct `fmt.Fprintf`** calls scattered throughout commands
2. **`WriteMessageOptions`** in `pkg/terminal/output/output.go` - requires creating a new instance per message
3. **Global flags** (`--no-color`, `--quiet`) must be passed to each output call manually
4. **Inconsistent patterns** - some commands use `WriteMessageOptions`, others use raw `fmt.Fprintf`

### Current Pain Points

```go
// Current approach - verbose and error-prone
output.NewWriteMessageOptions(
    ioStreams,
    output.MessageTypeError,
    cliParams.NoColor,
    cliParams.ExitOnError,
).WriteMessage(err.Error())
```

Every call requires:
- Access to `ioStreams`
- Access to `cliParams.NoColor`
- Access to `cliParams.ExitOnError`
- Knowledge of the correct `MessageType`

## Proposed Solution

Create a **`Writer`** struct that:
1. Is instantiated once per command context (typically at root command level)
2. Stores references to `IOStreams` and `settings.Run` (CLI params)
3. Provides simple, chainable methods for writing output
4. Supports context-based access for deep call stacks
5. Enables future extensibility (structured output, progress indicators, etc.)

## Architecture

### New Package Structure

```
pkg/terminal/
├── output/
│   ├── formats.go        # Output format types (existing)
│   ├── output.go         # Low-level message formatting (existing, refactored)
│   └── output_test.go    # Tests (existing)
├── writer/               # NEW PACKAGE
│   ├── writer.go         # Main Writer implementation
│   ├── writer_test.go    # Tests
│   ├── context.go        # Context integration
│   └── options.go        # Functional options
├── styles/
│   └── styles.go         # Lipgloss styles (existing)
└── terminal.go           # IOStreams (existing)
```

### Core Components

#### 1. Writer Struct

```go
// pkg/terminal/writer/writer.go

package writer

import (
    "context"
    "fmt"
    "io"

    "github.com/oakwood-commons/scafctl/pkg/settings"
    "github.com/oakwood-commons/scafctl/pkg/terminal"
    "github.com/oakwood-commons/scafctl/pkg/terminal/output"
)

// Writer provides a centralized interface for writing CLI output.
// It respects global settings like --quiet and --no-color automatically.
type Writer struct {
    ioStreams   *terminal.IOStreams
    cliParams   *settings.Run
    exitFunc    func(code int)
}

// Option is a functional option for configuring a Writer.
type Option func(*Writer)

// New creates a new Writer with the given options.
func New(ioStreams *terminal.IOStreams, cliParams *settings.Run, opts ...Option) *Writer {
    w := &Writer{
        ioStreams: ioStreams,
        cliParams: cliParams,
    }
    for _, opt := range opts {
        opt(w)
    }
    return w
}

// WithExitFunc sets a custom exit function (useful for testing).
func WithExitFunc(fn func(code int)) Option {
    return func(w *Writer) {
        w.exitFunc = fn
    }
}
```

#### 2. Output Methods

```go
// Success writes a success message to stdout.
// Respects --quiet and --no-color flags.
func (w *Writer) Success(format string, args ...any) {
    if w.cliParams.IsQuiet {
        return
    }
    msg := fmt.Sprintf(format, args...)
    fmt.Fprintln(w.ioStreams.Out, output.SuccessMessage(msg, w.cliParams.NoColor))
}

// Warning writes a warning message to stdout.
// Respects --quiet and --no-color flags.
func (w *Writer) Warning(format string, args ...any) {
    if w.cliParams.IsQuiet {
        return
    }
    msg := fmt.Sprintf(format, args...)
    fmt.Fprintln(w.ioStreams.Out, output.WarningMessage(msg, w.cliParams.NoColor))
}

// Error writes an error message to stderr.
// Does NOT respect --quiet (errors should always be visible).
// Respects --no-color flag.
func (w *Writer) Error(format string, args ...any) {
    msg := fmt.Sprintf(format, args...)
    fmt.Fprintln(w.ioStreams.ErrOut, output.ErrorMessage(msg, w.cliParams.NoColor))
}

// ErrorWithExit writes an error message and exits with code 1.
// Uses the configured exit function or os.Exit.
func (w *Writer) ErrorWithExit(format string, args ...any) {
    w.ErrorWithCode(1, format, args...)
}

// ErrorWithCode writes an error message and exits with the specified code.
// Uses the configured exit function or os.Exit.
func (w *Writer) ErrorWithCode(code int, format string, args ...any) {
    w.Error(format, args...)
    if w.exitFunc != nil {
        w.exitFunc(code)
    } else {
        os.Exit(code)
    }
}

// Info writes an informational message to stdout.
// Respects --quiet and --no-color flags.
func (w *Writer) Info(format string, args ...any) {
    if w.cliParams.IsQuiet {
        return
    }
    msg := fmt.Sprintf(format, args...)
    fmt.Fprintln(w.ioStreams.Out, output.InfoMessage(msg, w.cliParams.NoColor))
}

// Debug writes a debug message to stdout.
// Respects --quiet and --no-color flags.
// Only writes if log level indicates debug output is enabled.
func (w *Writer) Debug(format string, args ...any) {
    if w.cliParams.IsQuiet || w.cliParams.MinLogLevel > -1 {
        return
    }
    msg := fmt.Sprintf(format, args...)
    fmt.Fprintln(w.ioStreams.Out, output.DebugMessage(msg, w.cliParams.NoColor))
}

// Plain writes a plain message to stdout without any styling.
// Respects --quiet flag only.
func (w *Writer) Plain(format string, args ...any) {
    if w.cliParams.IsQuiet {
        return
    }
    fmt.Fprintf(w.ioStreams.Out, format, args...)
}

// Plainln writes a plain message to stdout with a newline.
// Respects --quiet flag only.
func (w *Writer) Plainln(format string, args ...any) {
    if w.cliParams.IsQuiet {
        return
    }
    fmt.Fprintln(w.ioStreams.Out, fmt.Sprintf(format, args...))
}
```

#### 3. Context Integration

```go
// pkg/terminal/writer/context.go

package writer

import "context"

type contextKey struct{}

// WithWriter returns a new context with the Writer attached.
func WithWriter(ctx context.Context, w *Writer) context.Context {
    return context.WithValue(ctx, contextKey{}, w)
}

// FromContext retrieves the Writer from the context.
// Returns nil if no Writer is present.
func FromContext(ctx context.Context) *Writer {
    w, _ := ctx.Value(contextKey{}).(*Writer)
    return w
}

// MustFromContext retrieves the Writer from the context.
// Panics if no Writer is present.
func MustFromContext(ctx context.Context) *Writer {
    w := FromContext(ctx)
    if w == nil {
        panic("writer: no Writer in context")
    }
    return w
}
```

#### 4. Structured Output Support (Future Extensibility)

```go
// WriteJSON writes JSON-formatted data to stdout.
// Respects --quiet flag.
func (w *Writer) WriteJSON(data any) error {
    if w.cliParams.IsQuiet {
        return nil
    }
    return output.WriteJSONOutput(w.ioStreams, data)
}

// WriteYAML writes YAML-formatted data to stdout.
// Respects --quiet flag.
func (w *Writer) WriteYAML(data any) error {
    if w.cliParams.IsQuiet {
        return nil
    }
    return output.WriteYAMLOutput(w.ioStreams, data)
}

// Write writes data in the format specified by cliParams or the given format.
func (w *Writer) Write(data any, format output.OutputFormat) error {
    if output.IsQuietFormat(format) || w.cliParams.IsQuiet {
        return nil
    }
    switch format {
    case output.OutputFormatJSON:
        return w.WriteJSON(data)
    case output.OutputFormatYAML:
        return w.WriteYAML(data)
    default:
        return fmt.Errorf("unsupported output format: %s", format)
    }
}
```

### Usage Examples

#### In Root Command

```go
// pkg/cmd/scafctl/root.go

func Root() *cobra.Command {
    cliParams := settings.NewCliParams()
    
    cCmd := &cobra.Command{
        Use:   "scafctl",
        PersistentPreRun: func(cCmd *cobra.Command, args []string) {
            // Create writer with CLI params
            ioStreams := terminal.NewIOStreams(os.Stdin, os.Stdout, os.Stderr, true)
            w := writer.New(ioStreams, cliParams)
            
            // Attach to context for subcommands
            ctx := writer.WithWriter(cCmd.Context(), w)
            cCmd.SetContext(ctx)
            
            // Example usage
            if err := validateArgs(args); err != nil {
                w.ErrorWithExit("Invalid arguments: %v", err)
            }
        },
    }
    // ...
}
```

#### In Subcommands

```go
// pkg/cmd/scafctl/run/solution.go

func (o *SolutionOptions) Run(ctx context.Context) error {
    w := writer.FromContext(ctx)
    
    // Simple, clean API
    w.Info("Loading solution from %s", o.File)
    
    sol, err := o.loadSolution(ctx)
    if err != nil {
        w.Error("Failed to load solution: %v", err)
        return err
    }
    
    w.Success("Solution loaded successfully")
    
    // Warnings
    if len(sol.Spec.Resolvers) == 0 {
        w.Warning("No resolvers defined in solution")
    }
    
    return nil
}
```

#### Testing

```go
func TestSolutionRun_OutputMessages(t *testing.T) {
    ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
    cliParams := settings.NewCliParams()
    
    // Capture exit calls
    var exitCode int
    w := writer.New(ioStreams, cliParams, writer.WithExitFunc(func(code int) {
        exitCode = code
    }))
    
    w.Success("Test passed")
    assert.Contains(t, outBuf.String(), "Test passed")
    
    w.Error("Something failed")
    assert.Contains(t, errBuf.String(), "Something failed")
}
```

## Migration Strategy

### Phase 1: Create the Writer Package (Non-Breaking)

1. Create `pkg/terminal/writer/` package with all new code
2. Add comprehensive unit tests
3. Document the API

### Phase 2: Integrate into Root Command

1. Create Writer in `PersistentPreRun`
2. Attach to context
3. Update root command's own output calls

### Phase 3: Gradual Migration of Subcommands

Migrate subcommands one at a time, prioritizing:
1. Commands with the most output calls
2. Commands that are actively being developed

For each command:
1. Add `w := writer.FromContext(ctx)` at the start
2. Replace `output.NewWriteMessageOptions(...).WriteMessage(...)` with `w.Error(...)`, etc.
3. Replace direct `fmt.Fprintf` calls with appropriate `w.*` methods

### Phase 4: Cleanup

1. Remove `output.WriteMessageOptions` and related functions no longer needed
2. Remove any unused code paths
3. Update documentation

## Benefits

### Immediate Benefits

1. **Cleaner API**: Single line instead of 5+ lines per message
2. **Automatic Flag Handling**: No need to pass `NoColor`, `ExitOnError`, etc.
3. **Consistency**: Same output style across all commands
4. **Testability**: Easy to mock and verify output in tests

### Future Extensibility

1. **Verbosity Levels**: Add `--verbose` flag support easily
2. **Structured Logging**: Route CLI output to structured logs for automation
3. **Progress Indicators**: Centralized spinners/progress bars
4. **Output Buffering**: Buffer output for atomic writes
5. **Color Themes**: Support custom color themes
6. **Accessibility**: Add screen reader support, high-contrast mode

## Alternative Approaches Considered

### 1. Global Writer Singleton

```go
var DefaultWriter *Writer

func init() {
    DefaultWriter = New(...)
}
```

**Rejected because:**
- Hard to test
- Not thread-safe without extra work
- Doesn't fit well with Cobra's command structure

### 2. Embedding Writer in Settings

```go
type Run struct {
    MinLogLevel int8
    // ...
    Writer *Writer
}
```

**Rejected because:**
- Mixes concerns (settings vs. output)
- Circular dependency potential
- Settings should be data, not behavior

### 3. Extending IOStreams

```go
type IOStreams struct {
    In           io.ReadCloser
    Out          io.Writer
    // ...
    WriteError   func(format string, args ...any)
}
```

**Rejected because:**
- IOStreams is meant to be a simple data holder
- Would require changes to all IOStreams usages
- Functions in structs complicate serialization/testing

## Design Decisions

1. **Quiet Mode and Errors**: `--quiet` does NOT suppress errors. Errors always show.

2. **Exit Code Mapping**: Yes, support custom exit codes:
   ```go
   w.ErrorWithCode(2, "Validation failed: %v", err)
   ```

3. **Progress Integration**: The Writer does not need to be aware of progress state. Resolver execution uses the `ProgressReporter` which writes to stderr. The Writer is for general messages, not progress updates. If a warning/error is written during progress, it simply writes (may visually interleave but that's acceptable for rare edge cases).

4. **Thread Safety**: Not required. Resolver execution goes through the progress reporter, not direct Writer calls.

5. **No Deprecation**: Remove unused code (`WriteMessageOptions`, etc.) once migration is complete. No deprecation period needed since breaking changes are acceptable.

## Files to Create/Modify

### New Files

- `pkg/terminal/writer/writer.go` - Main Writer implementation
- `pkg/terminal/writer/writer_test.go` - Unit tests
- `pkg/terminal/writer/context.go` - Context integration
- `pkg/terminal/writer/options.go` - Functional options
- `pkg/terminal/writer/doc.go` - Package documentation

### Files to Modify

- `pkg/cmd/scafctl/root.go` - Initialize Writer in PersistentPreRun
- All command files under `pkg/cmd/scafctl/*/` - Migrate to use Writer

## Timeline Estimate

| Phase | Effort | Description | Status |
|-------|--------|-------------|--------|
| Phase 1 | 1-2 days | Create writer package | ✅ Complete |
| Phase 2 | 0.5 day | Integrate into root command | ✅ Complete |
| Phase 3 | 2-3 days | Migrate all subcommands | ✅ Complete |
| Phase 4 | 0.5 day | Cleanup and documentation | ✅ Complete |

**Total: ~4-6 days** (Completed)

## Conclusion

This implementation provides a clean, centralized output system that:
- Reduces boilerplate code significantly
- Ensures consistent output formatting
- Respects global flags automatically
- Is easy to test
- Allows for future extensibility

The phased migration approach minimizes risk and allows for gradual adoption without breaking existing functionality.
