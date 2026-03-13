// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fileprovider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
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
			Schema: schemahelper.ObjectSchema([]string{"operation"}, map[string]*jsonschema.Schema{
				"operation": schemahelper.StringProp("Operation to perform",
					schemahelper.WithExample("read"),
					schemahelper.WithEnum("read", "write", "exists", "delete", "write-tree")),
				"path": schemahelper.StringProp("File path (absolute or relative). Required for read, write, exists, delete operations.",
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
				"basePath": schemahelper.StringProp("Destination root directory (required for write-tree operation). Entries are written relative to this path.",
					schemahelper.WithExample("./output"),
					schemahelper.WithMaxLength(4096)),
				"entries": schemahelper.ArrayProp("Array of {path, content} objects to write (required for write-tree operation). Typically produced by the go-template provider's render-tree operation.",
					schemahelper.WithItems(schemahelper.ObjectProp(
						"A file entry with relative path and content to write",
						[]string{"path", "content"},
						map[string]*jsonschema.Schema{
							"path":    schemahelper.StringProp("Relative file path within basePath"),
							"content": schemahelper.StringProp("File content to write"),
						},
					))),
				"outputPath": schemahelper.StringProp("Go template for transforming each entry's output path before writing (write-tree only). "+
					"Available variables: __filePath (relative path), __fileName (base name), __fileStem (name without final extension), "+
					"__fileExtension (final extension with dot), __fileDir (parent directory). "+
					"Sprig functions are available. If omitted, the original entry path is used unchanged.",
					schemahelper.WithExample("{{ .__fileDir }}/{{ .__fileStem }}"),
					schemahelper.WithMaxLength(4096)),
				"permissions": schemahelper.StringProp("Unix file permissions as an octal string (write and write-tree operations). Defaults to 0600 (owner read/write only).",
					schemahelper.WithExample("0600"),
					schemahelper.WithPattern(`^0[0-7]{3}$`),
					schemahelper.WithMaxLength(4)),
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
				{
					Name:        "Write tree of rendered templates",
					Description: "Write an array of rendered file entries to a destination directory, preserving directory structure. Typically used with the go-template provider's render-tree output.",
					YAML: `name: write-rendered-templates
provider: file
inputs:
  operation: write-tree
  basePath: ./output
  entries:
    rslvr: rendered-templates`,
				},
				{
					Name:        "Write tree with path transformation",
					Description: "Write rendered templates while stripping the .tpl extension from output paths using outputPath Go template",
					YAML: `name: write-with-path-transform
provider: file
inputs:
  operation: write-tree
  basePath: ./output
  entries:
    rslvr: rendered-templates
  outputPath: "{{ .__fileDir }}/{{ .__fileStem }}"`,
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

	lgr.V(1).Info("executing provider", "provider", ProviderName, "operation", operation)

	// write-tree uses basePath instead of path
	if operation == "write-tree" {
		return p.executeWriteTreeDispatch(ctx, inputs)
	}

	path, ok := inputs["path"].(string)
	if !ok {
		return nil, fmt.Errorf("%s: path is required and must be a string", ProviderName)
	}

	lgr.V(1).Info("executing file operation", "provider", ProviderName, "operation", operation, "path", path)

	// Resolve path: in action mode with output-dir, resolves against output-dir; otherwise CWD
	absPath, err := provider.ResolvePath(ctx, path)
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

	// Parse permissions — default to 0600 (owner read/write only).
	fileMode := os.FileMode(0o600)
	if permStr, ok := inputs["permissions"].(string); ok && permStr != "" {
		parsed, err := strconv.ParseUint(permStr, 8, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid permissions %q: must be an octal string like \"0600\"", permStr)
		}
		fileMode = os.FileMode(parsed)
	}

	// Create parent directories if requested
	if createDirs {
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create directories: %w", err)
		}
	}

	// Write file
	if err := os.WriteFile(absPath, []byte(content), fileMode); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}
	if err := os.Chmod(absPath, fileMode); err != nil {
		return nil, fmt.Errorf("failed to set file permissions: %w", err)
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

// executeWriteTreeDispatch handles the write-tree operation routing (dry-run vs live).
func (p *FileProvider) executeWriteTreeDispatch(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	basePath, ok := inputs["basePath"].(string)
	if !ok || basePath == "" {
		return nil, fmt.Errorf("%s: basePath is required for write-tree operation", ProviderName)
	}

	absBasePath, err := provider.ResolvePath(ctx, basePath)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid basePath: %w", ProviderName, err)
	}

	if provider.DryRunFromContext(ctx) {
		return p.executeDryRunWriteTree(absBasePath, inputs)
	}

	result, err := p.executeWriteTree(absBasePath, inputs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName, "operation", "write-tree")
	return result, nil
}

// executeWriteTree writes an array of {path, content} entries to the filesystem under basePath.
func (p *FileProvider) executeWriteTree(absBasePath string, inputs map[string]any) (*provider.Output, error) {
	entries, err := p.parseWriteTreeEntries(inputs)
	if err != nil {
		return nil, err
	}

	outputPathTmpl, _ := inputs["outputPath"].(string)

	// Parse permissions — default to 0600 (owner read/write only).
	fileMode := os.FileMode(0o600)
	if permStr, ok := inputs["permissions"].(string); ok && permStr != "" {
		parsed, err := strconv.ParseUint(permStr, 8, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid permissions %q: must be an octal string like \"0600\"", permStr)
		}
		fileMode = os.FileMode(parsed)
	}

	var writtenPaths []string

	for i, entry := range entries {
		outputPath := entry.path
		if outputPathTmpl != "" {
			transformed, tmplErr := p.renderOutputPath(outputPathTmpl, entry.path)
			if tmplErr != nil {
				return nil, fmt.Errorf("outputPath template failed for entries[%d] (%s): %w", i, entry.path, tmplErr)
			}
			outputPath = transformed
		}

		// Resolve absolute destination
		absDest := filepath.Join(absBasePath, outputPath)

		// Path traversal safety: ensure the destination is under basePath
		if !isSubPath(absBasePath, absDest) {
			return nil, fmt.Errorf("path traversal detected: entries[%d] path %q resolves outside basePath %q", i, outputPath, absBasePath)
		}

		// Create parent directories
		dir := filepath.Dir(absDest)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create directories for %s: %w", outputPath, err)
		}

		// Write file
		if err := os.WriteFile(absDest, []byte(entry.content), fileMode); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", outputPath, err)
		}
		if err := os.Chmod(absDest, fileMode); err != nil {
			return nil, fmt.Errorf("failed to set permissions on %s: %w", outputPath, err)
		}

		writtenPaths = append(writtenPaths, outputPath)
	}

	return &provider.Output{
		Data: map[string]any{
			"success":      true,
			"operation":    "write-tree",
			"basePath":     absBasePath,
			"filesWritten": len(writtenPaths),
			"paths":        writtenPaths,
		},
	}, nil
}

// writeTreeEntry holds a parsed entry for write-tree.
type writeTreeEntry struct {
	path    string
	content string
}

// parseWriteTreeEntries parses and validates the entries input for write-tree.
func (p *FileProvider) parseWriteTreeEntries(inputs map[string]any) ([]writeTreeEntry, error) {
	entriesRaw, ok := inputs["entries"]
	if !ok || entriesRaw == nil {
		return nil, fmt.Errorf("entries is required for write-tree operation")
	}

	// Handle both []any and []map[string]any (the latter comes from JSON/resolver data)
	var entryMaps []map[string]any
	switch v := entriesRaw.(type) {
	case []any:
		entryMaps = make([]map[string]any, 0, len(v))
		for i, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("entries[%d] must be a map, got %T", i, item)
			}
			entryMaps = append(entryMaps, m)
		}
	case []map[string]any:
		entryMaps = v
	default:
		return nil, fmt.Errorf("entries must be an array, got %T", entriesRaw)
	}

	result := make([]writeTreeEntry, 0, len(entryMaps))
	for i, entry := range entryMaps {
		path, ok := entry["path"].(string)
		if !ok || path == "" {
			return nil, fmt.Errorf("entries[%d].path is required and must be a string", i)
		}

		content, ok := entry["content"].(string)
		if !ok {
			return nil, fmt.Errorf("entries[%d].content is required and must be a string", i)
		}

		result = append(result, writeTreeEntry{path: path, content: content})
	}

	return result, nil
}

// renderOutputPath renders the outputPath Go template for a given file path,
// injecting __file* variables computed from the path.
func (p *FileProvider) renderOutputPath(outputPathTmpl, filePath string) (string, error) {
	// Compute __file* variables
	fileName := filepath.Base(filePath)
	fileExt := filepath.Ext(fileName)
	fileStem := strings.TrimSuffix(fileName, fileExt)
	fileDir := filepath.ToSlash(filepath.Dir(filePath))
	if fileDir == "." {
		fileDir = ""
	}

	data := map[string]any{
		"__filePath":      filepath.ToSlash(filePath),
		"__fileName":      fileName,
		"__fileStem":      fileStem,
		"__fileExtension": fileExt,
		"__fileDir":       fileDir,
	}

	svc := gotmpl.NewService(nil)
	result, err := svc.Execute(context.Background(), gotmpl.TemplateOptions{
		Content: outputPathTmpl,
		Name:    "outputPath",
		Data:    data,
	})
	if err != nil {
		return "", err
	}

	// Clean up the result
	output := strings.TrimSpace(result.Output)
	// Normalize path separators
	output = filepath.ToSlash(output)
	// Remove leading slash if present (should be relative)
	output = strings.TrimPrefix(output, "/")

	return output, nil
}

// isSubPath checks that child is under parent (path traversal protection).
func isSubPath(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	// If the relative path starts with "..", it's outside the parent
	return !strings.HasPrefix(rel, "..") && rel != ".."
}

// executeDryRunWriteTree returns a dry-run preview for write-tree.
func (p *FileProvider) executeDryRunWriteTree(absBasePath string, inputs map[string]any) (*provider.Output, error) {
	entries, err := p.parseWriteTreeEntries(inputs)
	if err != nil {
		// In dry-run, still validate entries
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	outputPathTmpl, _ := inputs["outputPath"].(string)

	var outputPaths []string
	for i, entry := range entries {
		outputPath := entry.path
		if outputPathTmpl != "" {
			transformed, tmplErr := p.renderOutputPath(outputPathTmpl, entry.path)
			if tmplErr != nil {
				return nil, fmt.Errorf("%s: outputPath template failed for entries[%d] (%s): %w", ProviderName, i, entry.path, tmplErr)
			}
			outputPath = transformed
		}
		outputPaths = append(outputPaths, outputPath)
	}

	return &provider.Output{
		Data: map[string]any{
			"success":      true,
			"operation":    "write-tree",
			"basePath":     absBasePath,
			"filesWritten": len(outputPaths),
			"paths":        outputPaths,
			"_dryRun":      true,
			"_message":     fmt.Sprintf("Would write %d files under %s", len(outputPaths), absBasePath),
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
