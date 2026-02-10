// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fileprovider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

// ProviderName is the name of this provider.
const ProviderName = "file"

// FileProvider provides filesystem operations.
type FileProvider struct {
	descriptor *provider.Descriptor
}

// NewFileProvider creates a new file provider instance.
func NewFileProvider() *FileProvider {
	version := semver.MustParse("1.0.0")

	return &FileProvider{
		descriptor: &provider.Descriptor{
			Name:         "file",
			DisplayName:  "File Provider",
			Description:  "Provider for filesystem operations (read, write, exists, delete)",
			APIVersion:   "v1",
			Version:      version,
			Category:     "filesystem",
			MockBehavior: "Returns mock file content without reading actual filesystem",
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,      // read, exists operations
				provider.CapabilityAction,    // write, delete operations
				provider.CapabilityTransform, // transform operations on file content
			},
			Schema: schemahelper.ObjectSchema([]string{"operation", "path"}, map[string]*jsonschema.Schema{
				"operation": schemahelper.StringProp("Operation to perform",
					schemahelper.WithExample("read"),
					schemahelper.WithEnum("read", "write", "exists", "delete")),
				"path": schemahelper.StringProp("File path (absolute or relative)",
					schemahelper.WithExample("./config.yaml"),
					schemahelper.WithMaxLength(4096)),
				"content": schemahelper.StringProp("Content to write (required for write operation)",
					schemahelper.WithExample("data: value"),
					schemahelper.WithMaxLength(10485760)),
				"createDirs": schemahelper.BoolProp("Create parent directories if they don't exist (for write operation)",
					schemahelper.WithExample(true),
					schemahelper.WithDefault(false)),
				"encoding": schemahelper.StringProp("File encoding for read/write operations",
					schemahelper.WithExample("utf-8"),
					schemahelper.WithDefault("utf-8"),
					schemahelper.WithEnum("utf-8", "binary")),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"content": schemahelper.StringProp("File content (for read operation)"),
					"exists":  schemahelper.BoolProp("Whether the file exists (for exists operation)"),
					"path":    schemahelper.StringProp("Absolute path to the file"),
					"size":    schemahelper.IntProp("File size in bytes (for read operation)"),
				}),
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"content": schemahelper.StringProp("File content (for read operation)"),
					"path":    schemahelper.StringProp("Absolute path to the file"),
					"size":    schemahelper.IntProp("File size in bytes"),
				}),
				provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"success": schemahelper.BoolProp("Whether the operation succeeded (for write/delete operations)"),
					"path":    schemahelper.StringProp("Absolute path to the file"),
				}),
			},
			Examples: []provider.Example{
				{
					Name:        "Read file contents",
					Description: "Read the contents of a file from the filesystem",
					YAML: `name: read-config
provider: file
inputs:
  operation: read
  path: ./config.yaml`,
				},
				{
					Name:        "Write file with directory creation",
					Description: "Write content to a file, creating parent directories if needed",
					YAML: `name: write-output
provider: file
inputs:
  operation: write
  path: ./output/data/result.txt
  content: "Generated content"
  createDirs: true`,
				},
				{
					Name:        "Check file existence",
					Description: "Check whether a file exists without reading its contents",
					YAML: `name: check-file
provider: file
inputs:
  operation: exists
  path: /etc/hosts`,
				},
				{
					Name:        "Delete file",
					Description: "Remove a file from the filesystem",
					YAML: `name: cleanup-temp
provider: file
inputs:
  operation: delete
  path: /tmp/temporary-file.txt`,
				},
			},
		},
	}
}

// Descriptor returns the provider's descriptor.
func (p *FileProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute performs the filesystem operation.
func (p *FileProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}
	operation, ok := inputs["operation"].(string)
	if !ok {
		return nil, fmt.Errorf("%s: operation is required and must be a string", ProviderName)
	}

	path, ok := inputs["path"].(string)
	if !ok {
		return nil, fmt.Errorf("%s: path is required and must be a string", ProviderName)
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName, "operation", operation, "path", path)

	// Convert to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid path: %w", ProviderName, err)
	}

	// Check for dry-run mode
	if dryRun := provider.DryRunFromContext(ctx); dryRun {
		result, err := p.executeDryRun(operation, absPath, inputs)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ProviderName, err)
		}
		lgr.V(1).Info("provider completed (dry-run)", "provider", ProviderName, "operation", operation)
		return result, nil
	}

	var result *provider.Output
	switch operation {
	case "read":
		result, err = p.executeRead(absPath)
	case "write":
		result, err = p.executeWrite(absPath, inputs)
	case "exists":
		result, err = p.executeExists(absPath)
	case "delete":
		result, err = p.executeDelete(absPath)
	default:
		return nil, fmt.Errorf("%s: unsupported operation: %s", ProviderName, operation)
	}

	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName, "operation", operation)
	return result, nil
}

func (p *FileProvider) executeRead(absPath string) (*provider.Output, error) {
	// Check if file exists
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file does not exist: %s", absPath)
		}
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file: %s", absPath)
	}

	// Read file content
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return &provider.Output{
		Data: map[string]any{
			"content": string(content),
			"path":    absPath,
			"size":    info.Size(),
		},
	}, nil
}

func (p *FileProvider) executeWrite(absPath string, inputs map[string]any) (*provider.Output, error) {
	content, ok := inputs["content"].(string)
	if !ok {
		return nil, fmt.Errorf("content is required for write operation")
	}

	createDirs, _ := inputs["createDirs"].(bool)

	// Create parent directories if requested
	if createDirs {
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create directories: %w", err)
		}
	}

	// Write file
	//nolint:gosec // 0644 is intentional for user-created files
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	return &provider.Output{
		Data: map[string]any{
			"success": true,
			"path":    absPath,
		},
	}, nil
}

//nolint:unparam // Error return kept for consistent interface - may return errors in future
func (p *FileProvider) executeExists(absPath string) (*provider.Output, error) {
	_, err := os.Stat(absPath)
	exists := err == nil

	return &provider.Output{
		Data: map[string]any{
			"exists": exists,
			"path":   absPath,
		},
	}, nil
}

func (p *FileProvider) executeDelete(absPath string) (*provider.Output, error) {
	// Check if file exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("file does not exist: %s", absPath)
	}

	// Delete file
	if err := os.Remove(absPath); err != nil {
		return nil, fmt.Errorf("failed to delete file: %w", err)
	}

	return &provider.Output{
		Data: map[string]any{
			"success": true,
			"path":    absPath,
		},
	}, nil
}

func (p *FileProvider) executeDryRun(operation, absPath string, inputs map[string]any) (*provider.Output, error) {
	switch operation {
	case "read":
		return &provider.Output{
			Data: map[string]any{
				"content":  "[DRY RUN] Would read file content",
				"path":     absPath,
				"size":     0,
				"_dryRun":  true,
				"_message": fmt.Sprintf("Would read file: %s", absPath),
			},
		}, nil

	case "write":
		content, _ := inputs["content"].(string)
		return &provider.Output{
			Data: map[string]any{
				"success":  true,
				"path":     absPath,
				"_dryRun":  true,
				"_message": fmt.Sprintf("Would write %d bytes to: %s", len(content), absPath),
			},
		}, nil

	case "exists":
		// Exists operation is read-only, so actually check
		_, err := os.Stat(absPath)
		exists := err == nil
		return &provider.Output{
			Data: map[string]any{
				"exists": exists,
				"path":   absPath,
			},
		}, nil

	case "delete":
		return &provider.Output{
			Data: map[string]any{
				"success":  true,
				"path":     absPath,
				"_dryRun":  true,
				"_message": fmt.Sprintf("Would delete file: %s", absPath),
			},
		}, nil

	default:
		return nil, fmt.Errorf("unsupported operation: %s", operation)
	}
}
