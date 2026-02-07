package execprovider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

// ProviderName is the name of the exec provider.
const ProviderName = "exec"

// ExecProvider provides shell command execution operations.
type ExecProvider struct {
	descriptor *provider.Descriptor
}

// NewExecProvider creates a new exec provider instance.
func NewExecProvider() *ExecProvider {
	version, _ := semver.NewVersion("1.0.0")
	return &ExecProvider{
		descriptor: &provider.Descriptor{
			Name:         "exec",
			DisplayName:  "Exec Provider",
			APIVersion:   "v1",
			Version:      version,
			Description:  "Executes shell commands in the local environment",
			MockBehavior: "Returns mock command output without executing actual shell command",
			Capabilities: []provider.Capability{
				provider.CapabilityAction, // Side effects (command execution)
				provider.CapabilityFrom,   // Read-only commands
				provider.CapabilityTransform,
			},
			Schema: schemahelper.ObjectSchema([]string{"command"}, map[string]*jsonschema.Schema{
				"command": schemahelper.StringProp("Command to execute",
					schemahelper.WithExample("echo"),
					schemahelper.WithMaxLength(1000)),
				"args": schemahelper.ArrayProp("Command arguments",
					schemahelper.WithMaxItems(100)),
				"stdin": schemahelper.StringProp("Standard input to provide to the command",
					schemahelper.WithMaxLength(1000000)),
				"workingDir": schemahelper.StringProp("Working directory for command execution",
					schemahelper.WithExample("/tmp"),
					schemahelper.WithMaxLength(500)),
				"env": schemahelper.AnyProp("Environment variables to set (key-value pairs)"),
				"timeout": schemahelper.IntProp("Timeout in seconds (0 or omit for no timeout)",
					schemahelper.WithExample("30"),
					schemahelper.WithMaximum(3600.0)),
				"shell": schemahelper.BoolProp("Execute through /bin/sh shell. When false (default), runs command directly (secure, fast, no shell features). When true, enables shell features like pipes, redirections, wildcards, and variable expansion (slower, use with caution)",
					schemahelper.WithExample("false")),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"stdout":   schemahelper.StringProp("Standard output from the command"),
					"stderr":   schemahelper.StringProp("Standard error output from the command"),
					"exitCode": schemahelper.IntProp("Command exit code"),
					"command":  schemahelper.StringProp("The full command that was executed"),
				}),
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"stdout":   schemahelper.StringProp("Standard output from the command"),
					"stderr":   schemahelper.StringProp("Standard error output from the command"),
					"exitCode": schemahelper.IntProp("Command exit code"),
					"command":  schemahelper.StringProp("The full command that was executed"),
				}),
				provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"success":  schemahelper.BoolProp("Whether the command succeeded (exit code 0)"),
					"stdout":   schemahelper.StringProp("Standard output from the command"),
					"stderr":   schemahelper.StringProp("Standard error output from the command"),
					"exitCode": schemahelper.IntProp("Command exit code"),
					"command":  schemahelper.StringProp("The full command that was executed"),
				}),
			},
			Examples: []provider.Example{
				{
					Name:        "Simple command execution",
					Description: "Execute a simple echo command with arguments",
					YAML: `name: echo-hello
provider: exec
inputs:
  command: echo
  args:
    - "Hello"
    - "World"`,
				},
				{
					Name:        "Command with timeout",
					Description: "Run a command with a 30 second timeout",
					YAML: `name: curl-with-timeout
provider: exec
inputs:
  command: curl
  args:
    - "-s"
    - "https://api.example.com/data"
  timeout: 30`,
				},
				{
					Name:        "Shell command with pipes",
					Description: "Use shell to execute complex command with pipes and redirections",
					YAML: `name: shell-pipeline
provider: exec
inputs:
  command: "cat /etc/hosts | grep localhost"
  shell: true`,
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

	lgr.V(1).Info("executing provider", "provider", ProviderName, "command", command)

	// Check for dry-run mode
	if dryRun := provider.DryRunFromContext(ctx); dryRun {
		output, err := p.executeDryRun(command, inputs)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ProviderName, err)
		}
		lgr.V(1).Info("provider completed (dry-run)", "provider", ProviderName)
		return output, nil
	}

	output, err := p.executeCommand(ctx, command, inputs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}
	lgr.V(1).Info("provider completed", "provider", ProviderName)
	return output, nil
}

func (p *ExecProvider) executeCommand(ctx context.Context, command string, inputs map[string]any) (*provider.Output, error) {
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

	// Parse timeout first to set up context
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

	// Parse shell flag
	useShell := false
	if shellRaw, ok := inputs["shell"]; ok {
		if s, ok := shellRaw.(bool); ok {
			useShell = s
		}
	}

	// Create command
	var cmd *exec.Cmd
	if useShell {
		// Execute through shell
		fullCommand := command
		if len(args) > 0 {
			// Quote arguments for shell
			quotedArgs := make([]string, len(args))
			for i, arg := range args {
				quotedArgs[i] = fmt.Sprintf("'%s'", strings.ReplaceAll(arg, "'", "'\\''"))
			}
			fullCommand = fmt.Sprintf("%s %s", command, strings.Join(quotedArgs, " "))
		}
		cmd = exec.CommandContext(cmdCtx, "/bin/sh", "-c", fullCommand)
	} else {
		// Direct execution
		cmd = exec.CommandContext(cmdCtx, command, args...)
	}

	// Set working directory if provided
	if workingDir, ok := inputs["workingDir"].(string); ok && workingDir != "" {
		cmd.Dir = workingDir
	}

	// Set environment variables if provided
	if envRaw, ok := inputs["env"]; ok && envRaw != nil {
		envMap, ok := envRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("env must be an object with string keys")
		}
		for key, val := range envMap {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%v", key, val))
		}
	}

	// Set up stdin if provided
	if stdinStr, ok := inputs["stdin"].(string); ok && stdinStr != "" {
		cmd.Stdin = strings.NewReader(stdinStr)
	}

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute command
	err := cmd.Run()

	// Get exit code
	exitCode := 0
	success := true
	if err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
			success = false
		} else {
			// Non-exit error (command not found, permission denied, etc.)
			return nil, fmt.Errorf("failed to execute command: %w", err)
		}
	}

	// Build full command string for output
	fullCmd := command
	if len(args) > 0 {
		fullCmd = fmt.Sprintf("%s %s", command, strings.Join(args, " "))
	}

	return &provider.Output{
		Data: map[string]any{
			"stdout":   stdout.String(),
			"stderr":   stderr.String(),
			"exitCode": exitCode,
			"success":  success,
			"command":  fullCmd,
		},
	}, nil
}

//nolint:unparam // Error return kept for consistent interface - may return errors in future
func (p *ExecProvider) executeDryRun(command string, inputs map[string]any) (*provider.Output, error) {
	// Build full command string
	fullCmd := command
	if argsRaw, ok := inputs["args"]; ok && argsRaw != nil {
		if args, ok := argsRaw.([]any); ok {
			argStrs := make([]string, len(args))
			for i, arg := range args {
				argStrs[i] = fmt.Sprint(arg)
			}
			fullCmd = fmt.Sprintf("%s %s", command, strings.Join(argStrs, " "))
		}
	}

	message := fmt.Sprintf("Would execute command: %s", fullCmd)
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
			"_dryRun":  true,
			"_message": message,
		},
	}, nil
}
