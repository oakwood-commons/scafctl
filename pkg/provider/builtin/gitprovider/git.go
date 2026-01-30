package gitprovider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
)

// ProviderName is the name of this provider.
const ProviderName = "git"

// GitProvider provides Git repository operations.
type GitProvider struct {
	descriptor *provider.Descriptor
}

// NewGitProvider creates a new git provider instance.
func NewGitProvider() *GitProvider {
	version, _ := semver.NewVersion("1.0.0")
	return &GitProvider{
		descriptor: &provider.Descriptor{
			Name:         "git",
			DisplayName:  "Git Provider",
			APIVersion:   "v1",
			Version:      version,
			Description:  "Performs Git version control operations on local and remote repositories using the local git executable",
			MockBehavior: "Returns mock git information without accessing actual git repository",
			Capabilities: []provider.Capability{
				provider.CapabilityAction,
				provider.CapabilityFrom,
			},
			Schema: provider.SchemaDefinition{
				Properties: map[string]provider.PropertyDefinition{
					"operation": {
						Type:        provider.PropertyTypeString,
						Required:    true,
						Description: "Git operation to perform",
						Example:     "clone",
						Enum:        []any{"clone", "pull", "status", "add", "commit", "push", "checkout", "branch", "log", "tag"},
						MaxLength:   ptrs.IntPtr(50),
					},
					"repository": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "Repository URL for clone operation",
						Example:     "https://github.com/user/repo.git",
						MaxLength:   ptrs.IntPtr(1000),
					},
					"path": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "Local path for repository",
						Example:     "/tmp/repo",
						MaxLength:   ptrs.IntPtr(500),
					},
					"branch": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "Branch name",
						Example:     "main",
						MaxLength:   ptrs.IntPtr(200),
					},
					"message": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "Commit message",
						Example:     "Update configuration",
						MaxLength:   ptrs.IntPtr(1000),
					},
					"files": {
						Type:        provider.PropertyTypeArray,
						Required:    false,
						Description: "Files to add",
						MaxItems:    ptrs.IntPtr(100),
					},
					"tag": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "Tag name",
						Example:     "v1.0.0",
						MaxLength:   ptrs.IntPtr(200),
					},
					"remote": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "Remote name",
						Example:     "origin",
						Default:     "origin",
						MaxLength:   ptrs.IntPtr(100),
					},
					"depth": {
						Type:        provider.PropertyTypeInt,
						Required:    false,
						Description: "Clone depth for shallow clone",
						Example:     "1",
						Maximum:     ptrs.Float64Ptr(10000.0),
					},
					"username": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "Username for authentication",
						Example:     "user",
						MaxLength:   ptrs.IntPtr(200),
					},
					"password": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "Password or token for authentication",
						Example:     "ghp_token",
						IsSecret:    true,
						MaxLength:   ptrs.IntPtr(500),
					},
					"force": {
						Type:        provider.PropertyTypeBool,
						Required:    false,
						Description: "Force the operation",
						Example:     "false",
						Default:     false,
					},
				},
			},
			OutputSchemas: map[provider.Capability]provider.SchemaDefinition{
				provider.CapabilityFrom: {
					Properties: map[string]provider.PropertyDefinition{
						"output": {
							Type:        provider.PropertyTypeString,
							Description: "Command output",
						},
						"operation": {
							Type:        provider.PropertyTypeString,
							Description: "The operation that was performed",
						},
						"path": {
							Type:        provider.PropertyTypeString,
							Description: "Repository path used",
						},
					},
				},
				provider.CapabilityAction: {
					Properties: map[string]provider.PropertyDefinition{
						"success": {
							Type:        provider.PropertyTypeBool,
							Description: "Whether the operation succeeded",
						},
						"output": {
							Type:        provider.PropertyTypeString,
							Description: "Command output",
						},
						"error": {
							Type:        provider.PropertyTypeString,
							Description: "Error message if operation failed",
						},
						"operation": {
							Type:        provider.PropertyTypeString,
							Description: "The operation that was performed",
						},
						"path": {
							Type:        provider.PropertyTypeString,
							Description: "Repository path used",
						},
					},
				},
			},
			Examples: []provider.Example{
				{
					Name:        "Clone repository",
					Description: "Clone a Git repository to a local path",
					YAML: `name: clone-repo
provider: git
inputs:
  operation: clone
  repository: "https://github.com/user/repo.git"
  path: /tmp/repo`,
				},
				{
					Name:        "Shallow clone",
					Description: "Clone only the latest commit for faster downloads",
					YAML: `name: shallow-clone
provider: git
inputs:
  operation: clone
  repository: "https://github.com/user/repo.git"
  path: /tmp/repo
  depth: 1`,
				},
				{
					Name:        "Commit changes",
					Description: "Add files and commit changes to the repository",
					YAML: `name: commit-changes
provider: git
inputs:
  operation: commit
  path: /tmp/repo
  message: "Update configuration files"
  files:
    - config.yaml
    - settings.json`,
				},
				{
					Name:        "Checkout branch",
					Description: "Switch to a different branch in the repository",
					YAML: `name: checkout-feature
provider: git
inputs:
  operation: checkout
  path: /tmp/repo
  branch: feature-branch`,
				},
				{
					Name:        "Push with authentication",
					Description: "Push changes to a remote repository with token authentication",
					YAML: `name: push-changes
provider: git
inputs:
  operation: push
  path: /tmp/repo
  remote: origin
  branch: main
  username: user
  password: ghp_secrettoken123`,
				},
			},
		},
	}
}

// Descriptor returns the provider's descriptor.
func (p *GitProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute performs the Git operation.
func (p *GitProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}
	operation, ok := inputs["operation"].(string)
	if !ok || operation == "" {
		return nil, fmt.Errorf("%s: operation is required and must be a non-empty string", ProviderName)
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName, "operation", operation)

	if dryRun := provider.DryRunFromContext(ctx); dryRun {
		result, err := p.executeDryRun(operation, inputs)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ProviderName, err)
		}
		lgr.V(1).Info("provider completed", "provider", ProviderName)
		return result, nil
	}

	result, err := p.executeGitOperation(ctx, operation, inputs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName)
	return result, nil
}

func (p *GitProvider) executeGitOperation(ctx context.Context, operation string, inputs map[string]any) (*provider.Output, error) {
	switch operation {
	case "clone":
		return p.executeClone(ctx, inputs)
	case "pull":
		return p.executePull(ctx, inputs)
	case "status":
		return p.executeStatus(ctx, inputs)
	case "add":
		return p.executeAdd(ctx, inputs)
	case "commit":
		return p.executeCommit(ctx, inputs)
	case "push":
		return p.executePush(ctx, inputs)
	case "checkout":
		return p.executeCheckout(ctx, inputs)
	case "branch":
		return p.executeBranch(ctx, inputs)
	case "log":
		return p.executeLog(ctx, inputs)
	case "tag":
		return p.executeTag(ctx, inputs)
	default:
		return nil, fmt.Errorf("unsupported operation: %s", operation)
	}
}

func (p *GitProvider) executeClone(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	repository, ok := inputs["repository"].(string)
	if !ok || repository == "" {
		return nil, fmt.Errorf("repository URL is required for clone operation")
	}

	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path is required for clone operation")
	}

	args := []string{"clone"}

	if depthRaw, ok := inputs["depth"]; ok {
		var depth int
		switch v := depthRaw.(type) {
		case int:
			depth = v
		case float64:
			depth = int(v)
		}
		if depth > 0 {
			args = append(args, "--depth", fmt.Sprint(depth))
		}
	}

	if branch, ok := inputs["branch"].(string); ok && branch != "" {
		args = append(args, "--branch", branch)
	}

	repoURL := repository
	if username, ok := inputs["username"].(string); ok && username != "" {
		if password, ok := inputs["password"].(string); ok && password != "" {
			repoURL = injectCredentials(repository, username, password)
		}
	}

	args = append(args, repoURL, path)

	return p.runGitCommand(ctx, "", args, "clone")
}

func (p *GitProvider) executePull(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path is required for pull operation")
	}

	remote, _ := inputs["remote"].(string)
	if remote == "" {
		remote = "origin"
	}

	args := []string{"pull", remote}

	if branch, ok := inputs["branch"].(string); ok && branch != "" {
		args = append(args, branch)
	}

	return p.runGitCommand(ctx, path, args, "pull")
}

func (p *GitProvider) executeStatus(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path is required for status operation")
	}

	args := []string{"status", "--porcelain"}

	return p.runGitCommand(ctx, path, args, "status")
}

func (p *GitProvider) executeAdd(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path is required for add operation")
	}

	args := []string{"add"}

	filesRaw, ok := inputs["files"]
	if !ok || filesRaw == nil {
		return nil, fmt.Errorf("files is required for add operation")
	}

	switch v := filesRaw.(type) {
	case []any:
		for _, file := range v {
			args = append(args, fmt.Sprint(file))
		}
	case []string:
		args = append(args, v...)
	default:
		return nil, fmt.Errorf("files must be an array")
	}

	return p.runGitCommand(ctx, path, args, "add")
}

func (p *GitProvider) executeCommit(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path is required for commit operation")
	}

	message, ok := inputs["message"].(string)
	if !ok || message == "" {
		return nil, fmt.Errorf("message is required for commit operation")
	}

	args := []string{"commit", "-m", message}

	return p.runGitCommand(ctx, path, args, "commit")
}

func (p *GitProvider) executePush(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path is required for push operation")
	}

	remote, _ := inputs["remote"].(string)
	if remote == "" {
		remote = "origin"
	}

	args := []string{"push", remote}

	if branch, ok := inputs["branch"].(string); ok && branch != "" {
		args = append(args, branch)
	}

	if force, ok := inputs["force"].(bool); ok && force {
		args = append(args, "--force")
	}

	return p.runGitCommand(ctx, path, args, "push")
}

func (p *GitProvider) executeCheckout(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path is required for checkout operation")
	}

	branch, ok := inputs["branch"].(string)
	if !ok || branch == "" {
		return nil, fmt.Errorf("branch is required for checkout operation")
	}

	args := []string{"checkout", branch}

	return p.runGitCommand(ctx, path, args, "checkout")
}

func (p *GitProvider) executeBranch(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path is required for branch operation")
	}

	args := []string{"branch"}

	if branch, ok := inputs["branch"].(string); ok && branch != "" {
		args = append(args, branch)
	} else {
		args = append(args, "-a")
	}

	return p.runGitCommand(ctx, path, args, "branch")
}

func (p *GitProvider) executeLog(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path is required for log operation")
	}

	args := []string{"log", "--oneline", "-n", "10"}

	return p.runGitCommand(ctx, path, args, "log")
}

func (p *GitProvider) executeTag(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	path, _ := inputs["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path is required for tag operation")
	}

	tag, ok := inputs["tag"].(string)
	if !ok || tag == "" {
		return p.runGitCommand(ctx, path, []string{"tag"}, "tag")
	}

	args := []string{"tag", tag}

	if message, ok := inputs["message"].(string); ok && message != "" {
		args = append(args, "-m", message)
	}

	return p.runGitCommand(ctx, path, args, "tag")
}

func (p *GitProvider) runGitCommand(ctx context.Context, workDir string, args []string, operation string) (*provider.Output, error) {
	cmd := exec.CommandContext(ctx, "git", args...)

	if workDir != "" {
		if operation != "clone" {
			if _, err := os.Stat(workDir); os.IsNotExist(err) {
				return nil, fmt.Errorf("directory does not exist: %s", workDir)
			}
		}
		cmd.Dir = workDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	success := true
	errorMsg := ""

	if err != nil {
		success = false
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			errorMsg = stderr.String()
			if errorMsg == "" {
				errorMsg = fmt.Sprintf("git command failed with exit code %d", exitErr.ExitCode())
			}
		} else {
			return nil, fmt.Errorf("failed to execute git command: %w", err)
		}
	}

	output := stdout.String()
	if output == "" && stderr.String() != "" {
		output = stderr.String()
	}

	return &provider.Output{
		Data: map[string]any{
			"success":   success,
			"output":    strings.TrimSpace(output),
			"error":     errorMsg,
			"operation": operation,
			"path":      workDir,
		},
	}, nil
}

//nolint:unparam // Error return kept for consistent interface - may return errors in future
func (p *GitProvider) executeDryRun(operation string, inputs map[string]any) (*provider.Output, error) {
	message := fmt.Sprintf("Would execute git %s", operation)

	if repository, ok := inputs["repository"].(string); ok && repository != "" {
		message += fmt.Sprintf(" on repository: %s", repository)
	}

	if path, ok := inputs["path"].(string); ok && path != "" {
		message += fmt.Sprintf(" at path: %s", path)
	}

	if branch, ok := inputs["branch"].(string); ok && branch != "" {
		message += fmt.Sprintf(" for branch: %s", branch)
	}

	return &provider.Output{
		Data: map[string]any{
			"success":   true,
			"output":    "",
			"error":     "",
			"operation": operation,
			"path":      inputs["path"],
			"_dryRun":   true,
			"_message":  message,
		},
	}, nil
}

func injectCredentials(repoURL, username, password string) string {
	if strings.HasPrefix(repoURL, "https://") {
		return strings.Replace(repoURL, "https://", fmt.Sprintf("https://%s:%s@", username, password), 1)
	}
	if strings.HasPrefix(repoURL, "http://") {
		return strings.Replace(repoURL, "http://", fmt.Sprintf("http://%s:%s@", username, password), 1)
	}
	return repoURL
}
