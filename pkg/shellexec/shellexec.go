// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package shellexec provides cross-platform shell command execution.
//
// By default, commands are executed through an embedded POSIX shell interpreter
// (mvdan.cc/sh) that works identically on Linux, macOS, and Windows without
// requiring any external shell binary. This approach is modeled after go-task/task.
//
// Users can opt into external shells (bash, pwsh, cmd) when they need
// platform-specific features like PowerShell cmdlets.
//
// On Windows, Go-native coreutils (cat, cp, mkdir, rm, etc.) are enabled by
// default so common POSIX commands work without extra tools installed. This
// behavior can be controlled via the SCAFCTL_CORE_UTILS environment variable.
package shellexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"mvdan.cc/sh/moreinterp/coreutils"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// ShellType represents the shell interpreter to use for command execution.
type ShellType string

const (
	// ShellAuto uses the embedded POSIX shell interpreter (mvdan.cc/sh).
	// This is the default and works identically on all platforms.
	ShellAuto ShellType = "auto"

	// ShellSh is an alias for ShellAuto — uses the embedded POSIX shell.
	ShellSh ShellType = "sh"

	// ShellBash shells out to an external bash binary found in PATH.
	// Use when you need bash-specific features not supported by the embedded shell.
	ShellBash ShellType = "bash"

	// ShellPwsh shells out to PowerShell Core (pwsh) found in PATH.
	// Use for PowerShell cmdlets and Windows-native scripting.
	ShellPwsh ShellType = "pwsh"

	// ShellCmd shells out to cmd.exe on Windows.
	// Use for Windows batch commands.
	ShellCmd ShellType = "cmd"
)

// ValidShellTypes returns all valid shell type values.
func ValidShellTypes() []string {
	return []string{
		string(ShellAuto),
		string(ShellSh),
		string(ShellBash),
		string(ShellPwsh),
		string(ShellCmd),
	}
}

// IsValid returns true if the shell type is a recognized value.
func (s ShellType) IsValid() bool {
	switch s {
	case ShellAuto, ShellSh, ShellBash, ShellPwsh, ShellCmd:
		return true
	default:
		return false
	}
}

// useCoreUtils determines whether to use Go-native coreutils.
// Enabled by default on Windows. Override with SCAFCTL_CORE_UTILS=0 or SCAFCTL_CORE_UTILS=1.
var useCoreUtils = initCoreUtils()

func initCoreUtils() bool {
	if v, err := strconv.ParseBool(os.Getenv("SCAFCTL_CORE_UTILS")); err == nil {
		return v
	}
	return runtime.GOOS == "windows"
}

// RunOptions configures a shell command execution.
type RunOptions struct {
	// Command is the command string to execute.
	Command string

	// Args are additional arguments appended to the command.
	// For embedded shell modes (auto, sh), args are shell-quoted and appended.
	// For external shell modes (bash, pwsh, cmd), args are shell-quoted and appended.
	Args []string

	// Shell selects the shell interpreter. Defaults to ShellAuto.
	Shell ShellType

	// Dir is the working directory. If empty, uses the current directory.
	Dir string

	// Env is a list of environment variables in "KEY=VALUE" format.
	// If nil, the parent process environment is inherited.
	// If non-nil, only the specified variables are set (use MergeEnv to inherit + add).
	Env []string

	// Stdin provides standard input to the command.
	Stdin io.Reader

	// Stdout captures standard output. If nil, output is discarded.
	Stdout io.Writer

	// Stderr captures standard error. If nil, output is discarded.
	Stderr io.Writer
}

// RunResult holds the result of a command execution.
type RunResult struct {
	// ExitCode is the process exit code. 0 indicates success.
	ExitCode int

	// Shell is the actual shell type that was used for execution.
	Shell ShellType
}

// MergeEnv creates an environment slice that inherits the parent process
// environment and adds/overrides with the provided key-value pairs.
func MergeEnv(extra map[string]any) []string {
	env := os.Environ()
	for key, val := range extra {
		env = append(env, fmt.Sprintf("%s=%v", key, val))
	}
	return env
}

// Run executes a command using the configured shell.
func Run(ctx context.Context, opts *RunOptions) (*RunResult, error) {
	if opts == nil {
		return nil, fmt.Errorf("shellexec: nil options")
	}
	if opts.Command == "" {
		return nil, fmt.Errorf("shellexec: empty command")
	}

	shell := opts.Shell
	if shell == "" {
		shell = ShellAuto
	}

	if !shell.IsValid() {
		return nil, fmt.Errorf("shellexec: unsupported shell type %q, valid values: %s",
			shell, strings.Join(ValidShellTypes(), ", "))
	}

	switch shell {
	case ShellAuto, ShellSh:
		return runEmbedded(ctx, opts, shell)
	case ShellBash:
		return runExternal(ctx, opts, "bash", []string{"--noprofile", "--norc", "-c"})
	case ShellPwsh:
		return runExternal(ctx, opts, "pwsh", []string{"-NoProfile", "-NonInteractive", "-Command"})
	case ShellCmd:
		if runtime.GOOS != "windows" {
			return nil, fmt.Errorf("shellexec: shell type %q is only available on Windows", ShellCmd)
		}
		return runExternal(ctx, opts, "cmd", []string{"/C"})
	default:
		return nil, fmt.Errorf("shellexec: unsupported shell type %q", shell)
	}
}

// runEmbedded executes a command through the embedded POSIX shell interpreter (mvdan.cc/sh).
func runEmbedded(ctx context.Context, opts *RunOptions, shell ShellType) (*RunResult, error) {
	// Build the full command string with args
	fullCommand := BuildFullCommand(opts.Command, opts.Args)

	// Resolve environment
	environ := opts.Env
	if len(environ) == 0 {
		environ = os.Environ()
	}

	// Build interpreter options
	interpOpts := []interp.RunnerOption{
		interp.Params("-e"), // errexit: exit on first error
		interp.Env(expand.ListEnviron(environ...)),
		interp.ExecHandlers(execHandlers()...),
		interp.OpenHandler(openHandler),
		interp.StdIO(opts.Stdin, opts.Stdout, opts.Stderr),
	}

	if opts.Dir != "" {
		interpOpts = append(interpOpts, dirOption(opts.Dir))
	}

	r, err := interp.New(interpOpts...)
	if err != nil {
		return nil, fmt.Errorf("shellexec: failed to create shell interpreter: %w", err)
	}

	// Parse the command
	parser := syntax.NewParser()
	prog, err := parser.Parse(strings.NewReader(fullCommand), "")
	if err != nil {
		return nil, fmt.Errorf("shellexec: failed to parse command: %w", err)
	}

	// Execute
	err = r.Run(ctx, prog)

	result := &RunResult{
		ExitCode: 0,
		Shell:    shell,
	}

	if err != nil {
		var exitStatus interp.ExitStatus
		if errors.As(err, &exitStatus) {
			result.ExitCode = int(exitStatus)
			return result, nil
		}
		// Check for context errors
		if ctx.Err() != nil {
			return nil, fmt.Errorf("shellexec: command interrupted: %w", ctx.Err())
		}
		return nil, fmt.Errorf("shellexec: failed to execute command: %w", err)
	}

	return result, nil
}

// runExternal executes a command through an external shell binary (bash, pwsh, cmd).
func runExternal(ctx context.Context, opts *RunOptions, binary string, shellArgs []string) (*RunResult, error) {
	// Find the shell binary
	shellPath, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("shellexec: shell %q not found in PATH: %w", binary, err)
	}

	// Build the full command string
	fullCommand := BuildFullCommand(opts.Command, opts.Args)

	// Build exec args: [shellArgs..., fullCommand]
	execArgs := make([]string, 0, len(shellArgs)+1)
	execArgs = append(execArgs, shellArgs...)
	execArgs = append(execArgs, fullCommand)

	cmd := exec.CommandContext(ctx, shellPath, execArgs...)

	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}

	// Set environment
	if len(opts.Env) > 0 {
		cmd.Env = opts.Env
	}

	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	}
	if opts.Stdout != nil {
		cmd.Stdout = opts.Stdout
	}
	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	}

	err = cmd.Run()

	shell := ShellType(binary)
	result := &RunResult{
		ExitCode: 0,
		Shell:    shell,
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return nil, fmt.Errorf("shellexec: failed to execute command: %w", err)
	}

	return result, nil
}

// BuildFullCommand constructs a full command string from command and args.
// Args are shell-quoted and appended to the command.
func BuildFullCommand(command string, args []string) string {
	if len(args) == 0 {
		return command
	}

	quotedArgs := make([]string, len(args))
	for i, arg := range args {
		quotedArgs[i] = ShellQuote(arg)
	}
	return fmt.Sprintf("%s %s", command, strings.Join(quotedArgs, " "))
}

// ShellQuote wraps str in single quotes with proper escaping for POSIX shells.
func ShellQuote(str string) string {
	// Use single quotes, escaping any embedded single quotes
	return "'" + strings.ReplaceAll(str, "'", "'\\''") + "'"
}

// execHandlers returns the chain of ExecHandler middleware for the embedded shell.
func execHandlers() []func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	var handlers []func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc
	if useCoreUtils {
		handlers = append(handlers, coreutils.ExecHandler)
	}
	return handlers
}

// devNull is an io.ReadWriteCloser that discards all writes and returns EOF on reads.
type devNull struct{}

func (devNull) Read([]byte) (int, error)    { return 0, io.EOF }
func (devNull) Write(b []byte) (int, error) { return len(b), nil }
func (devNull) Close() error                { return nil }

// openHandler maps /dev/null to a no-op writer on all platforms (including Windows
// where the path doesn't exist).
func openHandler(ctx context.Context, path string, flag int, perm os.FileMode) (io.ReadWriteCloser, error) {
	if path == "/dev/null" {
		return devNull{}, nil
	}
	return interp.DefaultOpenHandler()(ctx, path, flag, perm)
}

// dirOption sets the working directory for the embedded shell runner.
// If the directory doesn't exist yet (will be created later), we still set it.
func dirOption(path string) interp.RunnerOption {
	return func(r *interp.Runner) error {
		err := interp.Dir(path)(r)
		if err == nil {
			return nil
		}

		// If the directory doesn't exist yet, set it anyway —
		// it may be created by the command itself.
		if absPath, _ := filepath.Abs(path); absPath != "" {
			if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
				r.Dir = absPath
				return nil
			}
		}

		return err
	}
}

// RunSimple is a convenience function that runs a command and returns stdout, stderr, and exit code.
func RunSimple(ctx context.Context, shell ShellType, command string, args []string) (stdout, stderr string, exitCode int, err error) {
	var stdoutBuf, stderrBuf bytes.Buffer

	result, err := Run(ctx, &RunOptions{
		Command: command,
		Args:    args,
		Shell:   shell,
		Stdout:  &stdoutBuf,
		Stderr:  &stderrBuf,
	})
	if err != nil {
		return stdoutBuf.String(), stderrBuf.String(), -1, err
	}

	return stdoutBuf.String(), stderrBuf.String(), result.ExitCode, nil
}
