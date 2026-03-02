// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package execprovider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/shellexec"
)

// ProviderName is the name of the exec provider.
const ProviderName = "exec"

// ExecProvider provides shell command execution operations.
type ExecProvider struct {
	descriptor *provider.Descriptor
}

// NewExecProvider creates a new exec provider instance.
func NewExecProvider() *ExecProvider {
	version, _ := semver.NewVersion("2.0.0")
	return &ExecProvider{
		descriptor: &provider.Descriptor{
			Name:        "exec",
			DisplayName: "Exec Provider",
			APIVersion:  "v1",
			Version:     version,
			Description: "Executes shell commands using an embedded cross-platform POSIX shell interpreter. " +
				"Commands work identically on Linux, macOS, and Windows without requiring external shell binaries. " +
				"Supports pipes, redirections, variable expansion, and common coreutils on all platforms. " +
				"Optionally use external shells (bash, pwsh, cmd) for platform-specific features.",
			MockBehavior: "Returns mock command output without executing actual shell command",
			Capabilities: []provider.Capability{
				provider.CapabilityAction, // Side effects (command execution)
				provider.CapabilityFrom,   // Read-only commands
				provider.CapabilityTransform,
			},
			Schema: schemahelper.ObjectSchema([]string{"command"}, map[string]*jsonschema.Schema{
				"command": schemahelper.StringProp("Command to execute. Supports POSIX shell syntax including pipes (|), redirections (>, >>), variable expansion ($VAR), command substitution ($(cmd)), and conditionals by default",
					schemahelper.WithExample("echo hello | tr a-z A-Z"),
					schemahelper.WithMaxLength(1000)),
				"args": schemahelper.ArrayProp("Additional arguments appended to the command. Arguments are automatically shell-quoted for safety",
					schemahelper.WithMaxItems(100)),
				"stdin": schemahelper.StringProp("Standard input to provide to the command",
					schemahelper.WithMaxLength(1000000)),
				"workingDir": schemahelper.StringProp("Working directory for command execution",
					schemahelper.WithExample("/tmp"),
					schemahelper.WithMaxLength(500)),
				"env": schemahelper.AnyProp("Environment variables to set (key-value pairs). Merged with the parent process environment"),
				"timeout": schemahelper.IntProp("Timeout in seconds (0 or omit for no timeout)",
					schemahelper.WithExample("30"),
					schemahelper.WithMaximum(3600.0)),
				"shell": schemahelper.StringProp(
					"Shell interpreter to use. "+
						"'auto' (default): embedded POSIX shell that works identically on all platforms (Linux, macOS, Windows). "+
						"'sh': alias for 'auto'. "+
						"'bash': external bash binary from PATH. "+
						"'pwsh': external PowerShell Core (pwsh) from PATH — use for PowerShell cmdlets. "+
						"'cmd': external cmd.exe (Windows only)",
					schemahelper.WithEnum("auto", "sh", "bash", "pwsh", "cmd"),
					schemahelper.WithDefault("auto"),
					schemahelper.WithExample("auto"),
					schemahelper.WithMaxLength(10)),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"stdout":   schemahelper.StringProp("Standard output from the command"),
					"stderr":   schemahelper.StringProp("Standard error output from the command"),
					"exitCode": schemahelper.IntProp("Command exit code"),
					"command":  schemahelper.StringProp("The full command that was executed"),
					"shell":    schemahelper.StringProp("The shell interpreter that was used"),
				}),
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"stdout":   schemahelper.StringProp("Standard output from the command"),
					"stderr":   schemahelper.StringProp("Standard error output from the command"),
					"exitCode": schemahelper.IntProp("Command exit code"),
					"command":  schemahelper.StringProp("The full command that was executed"),
					"shell":    schemahelper.StringProp("The shell interpreter that was used"),
				}),
				provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"success":  schemahelper.BoolProp("Whether the command succeeded (exit code 0)"),
					"stdout":   schemahelper.StringProp("Standard output from the command"),
					"stderr":   schemahelper.StringProp("Standard error output from the command"),
					"exitCode": schemahelper.IntProp("Command exit code"),
					"command":  schemahelper.StringProp("The full command that was executed"),
					"shell":    schemahelper.StringProp("The shell interpreter that was used"),
				}),
			},
			Examples: []provider.Example{
				{
					Name:        "Simple command execution",
					Description: "Execute a simple echo command — pipes and shell features work by default",
					YAML: `name: echo-hello
provider: exec
inputs:
  command: echo "Hello, World!"`,
				},
				{
					Name:        "Command with arguments",
					Description: "Pass explicit arguments that are automatically shell-quoted",
					YAML: `name: echo-args
provider: exec
inputs:
  command: echo
  args:
    - "Hello"
    - "World"`,
				},
				{
					Name:        "Pipeline command",
					Description: "Use pipes, redirections, and shell features — works on all platforms",
					YAML: `name: pipeline
provider: exec
inputs:
  command: "echo 'hello world' | tr a-z A-Z"`,
				},
				{
					Name:        "Command with timeout",
					Description: "Run a command with a 30 second timeout",
					YAML: `name: curl-with-timeout
provider: exec
inputs:
  command: curl -s https://api.example.com/data
  timeout: 30`,
				},
				{
					Name:        "Command with custom environment",
					Description: "Execute a script with custom environment variables and working directory",
					YAML: `name: custom-env-script
provider: exec
inputs:
  command: ./build.sh
  workingDir: /project/src
  env:
    BUILD_ENV: production
    VERSION: "1.0.0"`,
				},
				{
					Name:        "PowerShell command",
					Description: "Use PowerShell for Windows-specific operations",
					YAML: `name: pwsh-example
provider: exec
inputs:
  command: "Get-ChildItem | Select-Object Name"
  shell: pwsh`,
				},
				{
					Name:        "External bash",
					Description: "Use an external bash shell for bash-specific features",
					YAML: `name: bash-specific
provider: exec
inputs:
  command: 'shopt -s globstar; echo **/*.go'
  shell: bash`,
				},
			},
		},
	}
}

// Descriptor returns the provider's descriptor.
func (p *ExecProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute performs the command execution.
func (p *ExecProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}
	command, ok := inputs["command"].(string)
	if !ok || command == "" {
		return nil, fmt.Errorf("%s: command is required and must be a non-empty string", ProviderName)
	}

	// Parse shell type
	shell := shellexec.ShellAuto
	if shellRaw, ok := inputs["shell"]; ok && shellRaw != nil {
		shellStr, ok := shellRaw.(string)
		if !ok {
			return nil, fmt.Errorf("%s: shell must be a string (one of: %s)", ProviderName,
				strings.Join(shellexec.ValidShellTypes(), ", "))
		}
		shell = shellexec.ShellType(shellStr)
		if !shell.IsValid() {
			return nil, fmt.Errorf("%s: unsupported shell type %q, valid values: %s", ProviderName,
				shellStr, strings.Join(shellexec.ValidShellTypes(), ", "))
		}
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName, "command", command, "shell", string(shell))

	// Check for dry-run mode
	if dryRun := provider.DryRunFromContext(ctx); dryRun {
		output, err := p.executeDryRun(command, inputs, shell)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ProviderName, err)
		}
		lgr.V(1).Info("provider completed (dry-run)", "provider", ProviderName)
		return output, nil
	}

	output, err := p.executeCommand(ctx, command, inputs, shell)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}
	lgr.V(1).Info("provider completed", "provider", ProviderName)
	return output, nil
}

func (p *ExecProvider) executeCommand(ctx context.Context, command string, inputs map[string]any, shell shellexec.ShellType) (*provider.Output, error) {
	// Parse arguments
	var args []string
	if argsRaw, ok := inputs["args"]; ok && argsRaw != nil {
		switch v := argsRaw.(type) {
		case []any:
			for _, arg := range v {
				args = append(args, fmt.Sprint(arg))
			}
		case []string:
			args = v
		default:
			return nil, fmt.Errorf("args must be an array")
		}
	}

	// Parse timeout to set up context
	cmdCtx := ctx
	var cancel context.CancelFunc

	if timeoutRaw, ok := inputs["timeout"]; ok && timeoutRaw != nil {
		var timeoutSecs int
		switch v := timeoutRaw.(type) {
		case int:
			timeoutSecs = v
		case float64:
			timeoutSecs = int(v)
		default:
			return nil, fmt.Errorf("timeout must be an integer")
		}

		if timeoutSecs > 0 {
			cmdCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
			defer cancel()
		}
	}

	// Build environment
	var env []string
	if envRaw, ok := inputs["env"]; ok && envRaw != nil {
		envMap, ok := envRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("env must be an object with string keys")
		}
		env = shellexec.MergeEnv(envMap)
	}

	// Set up stdin
	var stdin *strings.Reader
	if stdinStr, ok := inputs["stdin"].(string); ok && stdinStr != "" {
		stdin = strings.NewReader(stdinStr)
	}

	// Parse working directory
	var workingDir string
	if dir, ok := inputs["workingDir"].(string); ok && dir != "" {
		workingDir = dir
	}

	// Capture stdout and stderr into buffers.
	// If IOStreams are available in context, also stream to the terminal in real-time
	// using io.MultiWriter so output appears immediately while still being captured
	// for inter-action dependencies.
	var stdout, stderr bytes.Buffer
	var stdoutWriter, stderrWriter io.Writer = &stdout, &stderr
	streamed := false

	if ioStreams, ok := provider.IOStreamsFromContext(ctx); ok && ioStreams != nil {
		if ioStreams.Out != nil {
			stdoutWriter = io.MultiWriter(&stdout, ioStreams.Out)
			streamed = true
		}
		if ioStreams.ErrOut != nil {
			stderrWriter = io.MultiWriter(&stderr, ioStreams.ErrOut)
		}
	}

	// Build run options
	opts := &shellexec.RunOptions{
		Command: command,
		Args:    args,
		Shell:   shell,
		Dir:     workingDir,
		Env:     env,
		Stdout:  stdoutWriter,
		Stderr:  stderrWriter,
	}
	if stdin != nil {
		opts.Stdin = stdin
	}

	// Execute command — uses RunWithContext so tests can inject a mock RunFunc
	result, err := shellexec.RunWithContext(cmdCtx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	// Build full command string for output
	fullCmd := shellexec.BuildFullCommand(command, args)

	return &provider.Output{
		Data: map[string]any{
			"stdout":   stdout.String(),
			"stderr":   stderr.String(),
			"exitCode": result.ExitCode,
			"success":  result.ExitCode == 0,
			"command":  fullCmd,
			"shell":    string(result.Shell),
		},
		Streamed: streamed,
	}, nil
}

//nolint:unparam // Error return kept for consistent interface - may return errors in future
func (p *ExecProvider) executeDryRun(command string, inputs map[string]any, shell shellexec.ShellType) (*provider.Output, error) {
	// Parse arguments
	var args []string
	if argsRaw, ok := inputs["args"]; ok && argsRaw != nil {
		if argSlice, ok := argsRaw.([]any); ok {
			for _, arg := range argSlice {
				args = append(args, fmt.Sprint(arg))
			}
		}
	}

	fullCmd := shellexec.BuildFullCommand(command, args)

	message := fmt.Sprintf("Would execute via %s shell: %s", shell, fullCmd)
	if workingDir, ok := inputs["workingDir"].(string); ok && workingDir != "" {
		message += fmt.Sprintf(" in directory: %s", workingDir)
	}

	return &provider.Output{
		Data: map[string]any{
			"stdout":   "",
			"stderr":   "",
			"exitCode": 0,
			"success":  true,
			"command":  fullCmd,
			"shell":    string(shell),
			"_dryRun":  true,
			"_message": message,
		},
	}, nil
}
