// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fileprovider

import (
	"context"
	"errors"
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
			Name:        "file",
			DisplayName: "File Provider",
			Description: "Provider for filesystem operations (read, write, exists, delete)",
			APIVersion:  "v1",
			Version:     version,
			Category:    "filesystem",
			WhatIf: func(_ context.Context, input any) (string, error) {
				inputs, ok := input.(map[string]any)
				if !ok {
					return "", nil
				}
				operation, _ := inputs["operation"].(string)
				path, _ := inputs["path"].(string)
				switch operation {
				case "write":
					return fmt.Sprintf("Would write file %s", path), nil
				case "delete":
					return fmt.Sprintf("Would delete file %s", path), nil
				case "read":
					return fmt.Sprintf("Would read file %s", path), nil
				case "exists":
					return fmt.Sprintf("Would check if file exists: %s", path), nil
				case "write-tree":
					basePath, _ := inputs["basePath"].(string)
					return fmt.Sprintf("Would write file tree to %s", basePath), nil
				default:
					return fmt.Sprintf("Would perform file %s on %s", operation, path), nil
				}
			},
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
				"entries": schemahelper.ArrayProp("Array of {path, content} objects to write (required for write-tree operation). "+
					"Entries without a content field (e.g. directory entries) are silently skipped. "+
					"Typically produced by the go-template provider's render-tree operation.",
					schemahelper.WithItems(schemahelper.ObjectProp(
						"A file entry with relative path and optional content to write",
						[]string{"path"},
						map[string]*jsonschema.Schema{
							"path":    schemahelper.StringProp("Relative file path within basePath"),
							"content": schemahelper.StringProp("File content to write"),
							"onConflict": schemahelper.StringProp("Per-entry conflict resolution strategy override",
								schemahelper.WithEnum("error", "overwrite", "skip", "skip-unchanged", "append")),
							"dedupe": schemahelper.BoolProp("Per-entry deduplication override (only valid with append strategy)"),
							"backup": schemahelper.BoolProp("Per-entry backup override"),
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
				"onConflict": schemahelper.StringProp(
					"Conflict resolution strategy when the target file already exists. "+
						"Defaults to skip-unchanged when not overridden by the --on-conflict CLI flag.",
					schemahelper.WithExample("skip-unchanged"),
					schemahelper.WithEnum("error", "overwrite", "skip", "skip-unchanged", "append")),
				"backup": schemahelper.BoolProp(
					"Create a .bak backup of existing files before mutating them. " +
						"Applies to overwrite, skip-unchanged (when content differs), and append (when content is appended)."),
				"dedupe": schemahelper.BoolProp(
					"When onConflict is append, perform line-level deduplication. "+
						"Only lines not already present in the existing file are appended. "+
						"Useful for files like .gitignore. "+
						"Returns a validation error if set to true with any strategy other than append.",
					schemahelper.WithExample(true),
					schemahelper.WithDefault(false)),
				"failFast": schemahelper.BoolProp(
					"When onConflict is error and operation is write-tree, stop at the first "+
						"conflicting file instead of collecting all conflicts into a single error. "+
						"Has no effect on other strategies or the write operation.",
					schemahelper.WithExample(false),
					schemahelper.WithDefault(false)),
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
					"success":      schemahelper.BoolProp("Whether the operation succeeded (for write/delete operations)"),
					"path":         schemahelper.StringProp("Absolute path to the file (write operation)"),
					"status":       schemahelper.StringProp("Outcome of the write operation (created, overwritten, skipped, unchanged, appended). The error strategy returns a Go error, not a status value."),
					"backupPath":   schemahelper.StringProp("Path to the backup file, if one was created"),
					"operation":    schemahelper.StringProp("Operation performed (write-tree)"),
					"basePath":     schemahelper.StringProp("Absolute destination root directory (write-tree operation)"),
					"paths":        schemahelper.ArrayProp("Relative paths of files that were written (write-tree operation)", schemahelper.WithItems(schemahelper.StringProp("Relative file path"))),
					"filesWritten": schemahelper.IntProp("Total number of files written (created + overwritten + appended) for write-tree operation"),
					"filesStatus": schemahelper.ArrayProp("Per-file status details (for write-tree operation)",
						schemahelper.WithItems(schemahelper.ObjectProp(
							"Status of a single file write within a write-tree operation",
							nil,
							map[string]*jsonschema.Schema{
								"path":       schemahelper.StringProp("Relative path of the file"),
								"status":     schemahelper.StringProp("Outcome of the write (created, overwritten, skipped, unchanged, appended). The error strategy returns a Go error, not a status value."),
								"backupPath": schemahelper.StringProp("Path to the backup file, if one was created"),
							},
						))),
					"created":     schemahelper.IntProp("Number of newly created files (write-tree summary)"),
					"overwritten": schemahelper.IntProp("Number of overwritten files (write-tree summary)"),
					"skipped":     schemahelper.IntProp("Number of skipped files (write-tree summary)"),
					"unchanged":   schemahelper.IntProp("Number of unchanged files (write-tree summary)"),
					"appended":    schemahelper.IntProp("Number of files with appended content (write-tree summary)"),
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
		result, err := p.executeDryRun(ctx, operation, absPath, inputs)
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
		result, err = p.executeWrite(ctx, absPath, inputs)
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

func (p *FileProvider) executeWrite(ctx context.Context, absPath string, inputs map[string]any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

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

	// Resolve conflict strategy and flags.
	onConflictStr, _ := inputs["onConflict"].(string)
	strategy := resolveConflictStrategy(ctx, "", onConflictStr)
	backupFlag := resolveBackup(ctx, nil, boolPtrFromInputs(inputs, "backup"))
	dedupeFlag := resolveDedupe(nil, boolPtrFromInputs(inputs, "dedupe"))

	// Validate: dedupe only valid with append strategy.
	if dedupeFlag && strategy != ConflictAppend {
		return nil, fmt.Errorf("dedupe can only be used with append strategy, got %q", strategy)
	}

	lgr.V(1).Info("file write", "path", absPath, "strategy", string(strategy), "backup", backupFlag)

	// Create parent directories if requested.
	if createDirs {
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create directories: %w", err)
		}
	}

	contentBytes := []byte(content)
	outputData := map[string]any{
		"success": true,
		"path":    absPath,
	}

	// Check if target file exists.
	statInfo, statErr := os.Stat(absPath)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to stat file %s: %w", absPath, statErr)
	}
	fileExists := statErr == nil

	if fileExists && statInfo.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file: %s", absPath)
	}

	if !fileExists {
		// File does not exist — handle append empty content as no-op.
		if strategy == ConflictAppend && len(contentBytes) == 0 {
			outputData["status"] = string(StatusUnchanged)
			return &provider.Output{Data: outputData}, nil
		}
		if err := writeFileWithMode(absPath, contentBytes, fileMode); err != nil {
			return nil, err
		}
		outputData["status"] = string(StatusCreated)
		return &provider.Output{Data: outputData}, nil
	}

	// File exists — apply conflict strategy.
	switch strategy {
	case ConflictError:
		match, err := contentMatchesFile(absPath, contentBytes)
		if err != nil {
			return nil, fmt.Errorf("content comparison failed: %w", err)
		}
		if match {
			outputData["status"] = string(StatusUnchanged)
			return &provider.Output{Data: outputData}, nil
		}
		userPath, _ := inputs["path"].(string)
		if userPath == "" {
			userPath = absPath
		}
		return nil, &FileConflictError{Changed: []string{userPath}}

	case ConflictSkip:
		outputData["status"] = string(StatusSkipped)
		return &provider.Output{Data: outputData}, nil

	case ConflictSkipUnchanged:
		match, err := contentMatchesFile(absPath, contentBytes)
		if err != nil {
			return nil, fmt.Errorf("content comparison failed: %w", err)
		}
		if match {
			outputData["status"] = string(StatusUnchanged)
			return &provider.Output{Data: outputData}, nil
		}
		if backupFlag {
			bp, err := backupFile(absPath)
			if err != nil {
				return nil, fmt.Errorf("backup failed: %w", err)
			}
			outputData["backupPath"] = bp
		}
		if err := writeFileWithMode(absPath, contentBytes, fileMode); err != nil {
			return nil, err
		}
		outputData["status"] = string(StatusOverwritten)
		return &provider.Output{Data: outputData}, nil

	case ConflictOverwrite:
		if backupFlag {
			bp, err := backupFile(absPath)
			if err != nil {
				return nil, fmt.Errorf("backup failed: %w", err)
			}
			outputData["backupPath"] = bp
		}
		if err := writeFileWithMode(absPath, contentBytes, fileMode); err != nil {
			return nil, err
		}
		outputData["status"] = string(StatusOverwritten)
		return &provider.Output{Data: outputData}, nil

	case ConflictAppend:
		if len(contentBytes) == 0 {
			outputData["status"] = string(StatusUnchanged)
			return &provider.Output{Data: outputData}, nil
		}
		if backupFlag {
			bp, err := backupFile(absPath)
			if err != nil {
				return nil, fmt.Errorf("backup failed: %w", err)
			}
			outputData["backupPath"] = bp
		}
		status, err := appendToFile(absPath, contentBytes, fileMode, dedupeFlag)
		if err != nil {
			return nil, fmt.Errorf("append failed: %w", err)
		}
		outputData["status"] = string(status)
		return &provider.Output{Data: outputData}, nil

	default:
		return nil, fmt.Errorf("unsupported conflict strategy: %s", strategy)
	}
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
		return p.executeDryRunWriteTree(ctx, absBasePath, inputs)
	}

	result, err := p.executeWriteTree(ctx, absBasePath, inputs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName, "operation", "write-tree")
	return result, nil
}

// executeWriteTree writes an array of {path, content} entries to the filesystem under basePath.
func (p *FileProvider) executeWriteTree(ctx context.Context, absBasePath string, inputs map[string]any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

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

	// Invocation-level conflict inputs.
	invOnConflict, _ := inputs["onConflict"].(string)
	invBackup := boolPtrFromInputs(inputs, "backup")
	invDedupe := boolPtrFromInputs(inputs, "dedupe")
	failFast, _ := inputs["failFast"].(bool)

	// Phase 1: Resolve output paths, strategies, and validate.
	type resolvedEntry struct {
		outputPath string
		absDest    string
		content    string
		strategy   ConflictStrategy
		backup     bool
		dedupe     bool
	}

	resolved := make([]resolvedEntry, 0, len(entries))
	for i, entry := range entries {
		outputPath := entry.path
		if outputPathTmpl != "" {
			transformed, tmplErr := p.renderOutputPath(outputPathTmpl, entry.path)
			if tmplErr != nil {
				return nil, fmt.Errorf("outputPath template failed for entries[%d] (%s): %w", i, entry.path, tmplErr)
			}
			outputPath = transformed
		}

		absDest := filepath.Join(absBasePath, outputPath)
		if !isSubPath(absBasePath, absDest) {
			return nil, fmt.Errorf("path traversal detected: entries[%d] path %q resolves outside basePath %q", i, outputPath, absBasePath)
		}

		strategy := resolveConflictStrategy(ctx, entry.onConflict, invOnConflict)
		backup := resolveBackup(ctx, entry.backup, invBackup)
		dedupe := resolveDedupe(entry.dedupe, invDedupe)

		if dedupe && strategy != ConflictAppend {
			return nil, fmt.Errorf("entries[%d] (%s): dedupe can only be used with append strategy, got %q", i, outputPath, strategy)
		}

		resolved = append(resolved, resolvedEntry{
			outputPath: outputPath,
			absDest:    absDest,
			content:    entry.content,
			strategy:   strategy,
			backup:     backup,
			dedupe:     dedupe,
		})
	}

	// Phase 2: Pre-scan for error strategy — runs unconditionally before any writes.
	// Checksum-aware: files with identical content are silently skipped (unchanged),
	// only files with actual content differences trigger the error.
	// In failFast mode: stop on the first changed conflict to prevent partial writes.
	{
		conflictErr := &FileConflictError{}
		for _, re := range resolved {
			if re.strategy != ConflictError {
				continue
			}
			_, statErr := os.Stat(re.absDest)
			if statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) {
					continue
				}
				return nil, fmt.Errorf("failed to stat file %s: %w", re.absDest, statErr)
			}
			// File exists — check if content matches.
			match, matchErr := contentMatchesFile(re.absDest, []byte(re.content))
			if matchErr != nil {
				return nil, fmt.Errorf("content comparison failed for %s: %w", re.absDest, matchErr)
			}
			if match {
				conflictErr.Unchanged = append(conflictErr.Unchanged, re.outputPath)
			} else {
				if failFast {
					return nil, &FileConflictError{Changed: []string{re.outputPath}}
				}
				conflictErr.Changed = append(conflictErr.Changed, re.outputPath)
			}
		}
		if len(conflictErr.Changed) > 0 {
			return nil, conflictErr
		}
	}

	// Phase 3: Write loop.
	results := make([]FileWriteResult, 0, len(resolved))
	var writtenPaths []string

	for i, re := range resolved {
		lgr.V(1).Info("file write", "path", re.absDest, "strategy", string(re.strategy), "backup", re.backup)

		contentBytes := []byte(re.content)
		result := FileWriteResult{Path: re.outputPath}

		statInfo, statErr := os.Stat(re.absDest)
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return nil, fmt.Errorf("entries[%d] (%s): failed to stat file: %w", i, re.outputPath, statErr)
		}
		fileExists := statErr == nil

		if fileExists && statInfo.IsDir() {
			return nil, fmt.Errorf("entries[%d] (%s): path is a directory, not a file: %s", i, re.outputPath, re.absDest)
		}

		if !fileExists {
			if re.strategy == ConflictAppend && len(contentBytes) == 0 {
				result.Status = StatusUnchanged
			} else {
				if err := ensureParentDir(re.absDest, re.outputPath); err != nil {
					return nil, err
				}
				if err := writeFileWithMode(re.absDest, contentBytes, fileMode); err != nil {
					return nil, fmt.Errorf("entries[%d] (%s): %w", i, re.outputPath, err)
				}
				result.Status = StatusCreated
				writtenPaths = append(writtenPaths, re.outputPath)
			}
		} else {
			switch re.strategy {
			case ConflictError:
				// Safety net: Phase 2 pre-scan should have already caught changed files.
				// If we reach here, the file has identical content (unchanged).
				result.Status = StatusUnchanged

			case ConflictSkip:
				result.Status = StatusSkipped

			case ConflictSkipUnchanged:
				match, err := contentMatchesFile(re.absDest, contentBytes)
				if err != nil {
					return nil, fmt.Errorf("entries[%d] (%s): content comparison failed: %w", i, re.outputPath, err)
				}
				if match {
					result.Status = StatusUnchanged
				} else {
					if re.backup {
						bp, err := backupFile(re.absDest)
						if err != nil {
							return nil, fmt.Errorf("entries[%d] (%s): backup failed: %w", i, re.outputPath, err)
						}
						result.BackupPath = bp
					}
					if err := writeFileWithMode(re.absDest, contentBytes, fileMode); err != nil {
						return nil, fmt.Errorf("entries[%d] (%s): %w", i, re.outputPath, err)
					}
					result.Status = StatusOverwritten
					writtenPaths = append(writtenPaths, re.outputPath)
				}

			case ConflictOverwrite:
				if re.backup {
					bp, err := backupFile(re.absDest)
					if err != nil {
						return nil, fmt.Errorf("entries[%d] (%s): backup failed: %w", i, re.outputPath, err)
					}
					result.BackupPath = bp
				}
				if err := writeFileWithMode(re.absDest, contentBytes, fileMode); err != nil {
					return nil, fmt.Errorf("entries[%d] (%s): %w", i, re.outputPath, err)
				}
				result.Status = StatusOverwritten
				writtenPaths = append(writtenPaths, re.outputPath)

			case ConflictAppend:
				if len(contentBytes) == 0 {
					result.Status = StatusUnchanged
				} else {
					if re.backup {
						bp, err := backupFile(re.absDest)
						if err != nil {
							return nil, fmt.Errorf("entries[%d] (%s): backup failed: %w", i, re.outputPath, err)
						}
						result.BackupPath = bp
					}
					status, err := appendToFile(re.absDest, contentBytes, fileMode, re.dedupe)
					if err != nil {
						return nil, fmt.Errorf("entries[%d] (%s): append failed: %w", i, re.outputPath, err)
					}
					result.Status = status
					if status == StatusAppended || status == StatusCreated {
						writtenPaths = append(writtenPaths, re.outputPath)
					}
				}

			default:
				return nil, fmt.Errorf("entries[%d] (%s): unsupported conflict strategy: %s", i, re.outputPath, re.strategy)
			}
		}

		results = append(results, result)
	}

	// Phase 4: Build output with summary counts.
	counts := map[FileWriteStatus]int{}
	filesStatus := make([]map[string]any, 0, len(results))
	for _, r := range results {
		counts[r.Status]++
		entry := map[string]any{
			"path":   r.Path,
			"status": string(r.Status),
		}
		if r.BackupPath != "" {
			entry["backupPath"] = r.BackupPath
		}
		filesStatus = append(filesStatus, entry)
	}

	filesWritten := counts[StatusCreated] + counts[StatusOverwritten] + counts[StatusAppended]

	return &provider.Output{
		Data: map[string]any{
			"success":      true,
			"operation":    "write-tree",
			"basePath":     absBasePath,
			"filesWritten": filesWritten,
			"paths":        writtenPaths,
			"filesStatus":  filesStatus,
			"created":      counts[StatusCreated],
			"overwritten":  counts[StatusOverwritten],
			"skipped":      counts[StatusSkipped],
			"unchanged":    counts[StatusUnchanged],
			"appended":     counts[StatusAppended],
		},
	}, nil
}

// writeTreeEntry holds a parsed entry for write-tree.
type writeTreeEntry struct {
	path       string
	content    string
	onConflict string // optional per-entry override
	dedupe     *bool  // optional per-entry override (pointer to distinguish unset from false)
	backup     *bool  // optional per-entry override (pointer to distinguish unset from false)
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

		contentRaw, hasContent := entry["content"]
		if !hasContent || contentRaw == nil {
			// Skip directory entries (no content key) silently.
			continue
		}
		content, ok := contentRaw.(string)
		if !ok {
			return nil, fmt.Errorf("entries[%d].content must be a string, got %T", i, contentRaw)
		}

		entryOnConflict, _ := entry["onConflict"].(string)

		// Validate per-entry onConflict if provided.
		if entryOnConflict != "" && !ConflictStrategy(entryOnConflict).IsValid() {
			return nil, fmt.Errorf("entries[%d].onConflict: invalid strategy %q (valid: error, overwrite, skip, skip-unchanged, append)", i, entryOnConflict)
		}

		result = append(result, writeTreeEntry{
			path:       path,
			content:    content,
			onConflict: entryOnConflict,
			dedupe:     boolPtrFromInputs(entry, "dedupe"),
			backup:     boolPtrFromInputs(entry, "backup"),
		})
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

// executeDryRunWriteTree returns a dry-run preview for write-tree with planned
// conflict resolution statuses per entry.
func (p *FileProvider) executeDryRunWriteTree(ctx context.Context, absBasePath string, inputs map[string]any) (*provider.Output, error) {
	entries, err := p.parseWriteTreeEntries(inputs)
	if err != nil {
		// In dry-run, still validate entries
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	outputPathTmpl, _ := inputs["outputPath"].(string)

	// Invocation-level conflict inputs.
	invOnConflict, _ := inputs["onConflict"].(string)
	invBackup := boolPtrFromInputs(inputs, "backup")
	invDedupe := boolPtrFromInputs(inputs, "dedupe")

	var outputPaths []string
	filesStatus := make([]map[string]any, 0, len(entries))
	counts := map[FileWriteStatus]int{}

	for i, entry := range entries {
		outputPath := entry.path
		if outputPathTmpl != "" {
			transformed, tmplErr := p.renderOutputPath(outputPathTmpl, entry.path)
			if tmplErr != nil {
				return nil, fmt.Errorf("%s: outputPath template failed for entries[%d] (%s): %w", ProviderName, i, entry.path, tmplErr)
			}
			outputPath = transformed
		}

		absDest := filepath.Join(absBasePath, outputPath)
		if !isSubPath(absBasePath, absDest) {
			return nil, fmt.Errorf("%s: path traversal detected: entries[%d] path %q resolves outside basePath %q", ProviderName, i, outputPath, absBasePath)
		}
		strategy := resolveConflictStrategy(ctx, entry.onConflict, invOnConflict)
		backup := resolveBackup(ctx, entry.backup, invBackup)
		dedupe := resolveDedupe(entry.dedupe, invDedupe)

		if dedupe && strategy != ConflictAppend {
			return nil, fmt.Errorf("%s: entries[%d] (%s): dedupe can only be used with append strategy, got %q", ProviderName, i, outputPath, strategy)
		}

		planned, err := computePlannedStatus(absDest, []byte(entry.content), strategy, dedupe)
		if err != nil {
			return nil, fmt.Errorf("%s: entries[%d] (%s): %w", ProviderName, i, outputPath, err)
		}

		statusEntry := map[string]any{
			"path":           outputPath,
			"_plannedStatus": string(planned),
			"_strategy":      string(strategy),
		}
		if backup {
			statusEntry["_backup"] = true
		}
		counts[planned]++
		filesStatus = append(filesStatus, statusEntry)
		if planned == StatusCreated || planned == StatusOverwritten || planned == StatusAppended {
			outputPaths = append(outputPaths, outputPath)
		}
	}

	filesWritten := counts[StatusCreated] + counts[StatusOverwritten] + counts[StatusAppended]

	return &provider.Output{
		Data: map[string]any{
			"success":      true,
			"operation":    "write-tree",
			"basePath":     absBasePath,
			"filesWritten": filesWritten,
			"paths":        outputPaths,
			"filesStatus":  filesStatus,
			"created":      counts[StatusCreated],
			"overwritten":  counts[StatusOverwritten],
			"skipped":      counts[StatusSkipped],
			"unchanged":    counts[StatusUnchanged],
			"appended":     counts[StatusAppended],
			"errored":      counts[StatusError],
			"_dryRun":      true,
			"_message":     fmt.Sprintf("Would write %d files under %s", filesWritten, absBasePath),
		},
	}, nil
}

func (p *FileProvider) executeDryRun(ctx context.Context, operation, absPath string, inputs map[string]any) (*provider.Output, error) {
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
		content, ok := inputs["content"].(string)
		if !ok {
			return nil, fmt.Errorf("content is required for write operation")
		}

		// Resolve conflict strategy and flags.
		onConflictStr, _ := inputs["onConflict"].(string)
		strategy := resolveConflictStrategy(ctx, "", onConflictStr)
		backupFlag := resolveBackup(ctx, nil, boolPtrFromInputs(inputs, "backup"))
		dedupeFlag := resolveDedupe(nil, boolPtrFromInputs(inputs, "dedupe"))

		if dedupeFlag && strategy != ConflictAppend {
			return nil, fmt.Errorf("dedupe can only be used with append strategy, got %q", strategy)
		}

		planned, err := computePlannedStatus(absPath, []byte(content), strategy, dedupeFlag)
		if err != nil {
			return nil, err
		}

		data := map[string]any{
			"success":        true,
			"path":           absPath,
			"_dryRun":        true,
			"_plannedStatus": string(planned),
			"_strategy":      string(strategy),
			"_message":       dryRunWriteMessage(planned, len(content), absPath),
		}
		if backupFlag {
			data["_backup"] = true
		}
		return &provider.Output{Data: data}, nil

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

// dryRunWriteMessage returns a human-readable dry-run message that accurately
// reflects what the write operation would do based on the planned status.
func dryRunWriteMessage(planned FileWriteStatus, contentLen int, absPath string) string {
	switch planned {
	case StatusCreated:
		return fmt.Sprintf("Would create %s (%d bytes)", absPath, contentLen)
	case StatusOverwritten:
		return fmt.Sprintf("Would overwrite %s (%d bytes)", absPath, contentLen)
	case StatusSkipped:
		return fmt.Sprintf("Would skip %s (file exists, strategy=skip)", absPath)
	case StatusUnchanged:
		return fmt.Sprintf("Would skip %s (content unchanged)", absPath)
	case StatusAppended:
		return fmt.Sprintf("Would append %d bytes to %s", contentLen, absPath)
	case StatusError:
		return fmt.Sprintf("Would error: file already exists: %s", absPath)
	default:
		return fmt.Sprintf("Would write %d bytes to: %s (planned: %s)", contentLen, absPath, planned)
	}
}

// computePlannedStatus determines what status a file write would produce without
// actually performing the write. Used by dry-run to preview conflict resolution.
// Returns an error if os.Stat fails for reasons other than the file not existing.
//
// Performance note: for skip-unchanged and append+dedupe strategies this reads
// the target file to compare content. When called in a write-tree loop the
// cost is O(entries × file_size) for those strategies.
func computePlannedStatus(absPath string, content []byte, strategy ConflictStrategy, dedupe bool) (FileWriteStatus, error) {
	statInfo, statErr := os.Stat(absPath)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("failed to stat file %s: %w", absPath, statErr)
	}
	fileExists := statErr == nil

	if fileExists && statInfo.IsDir() {
		return "", fmt.Errorf("path is a directory, not a file: %s", absPath)
	}

	if !fileExists {
		if strategy == ConflictAppend && len(content) == 0 {
			return StatusUnchanged, nil
		}
		return StatusCreated, nil
	}

	// File exists — compute planned status based on strategy.
	switch strategy {
	case ConflictError:
		// Would error at runtime, report accurately so dry-run output is not misleading.
		return StatusError, nil
	case ConflictSkip:
		return StatusSkipped, nil
	case ConflictSkipUnchanged:
		match, matchErr := contentMatchesFile(absPath, content)
		if matchErr != nil {
			return "", fmt.Errorf("content comparison failed for %s: %w", absPath, matchErr)
		}
		if match {
			return StatusUnchanged, nil
		}
		return StatusOverwritten, nil
	case ConflictOverwrite:
		return StatusOverwritten, nil
	case ConflictAppend:
		if len(content) == 0 {
			return StatusUnchanged, nil
		}
		if dedupe {
			// Check if all lines already exist.
			existing, readErr := os.ReadFile(absPath)
			if readErr != nil {
				return "", fmt.Errorf("read file for dedupe check %s: %w", absPath, readErr)
			}
			existingLines := strings.Split(string(existing), "\n")
			seen := make(map[string]bool, len(existingLines))
			for _, line := range existingLines {
				seen[strings.TrimRight(line, "\r")] = true
			}
			newLines := strings.Split(string(content), "\n")
			for _, line := range newLines {
				if !seen[strings.TrimRight(line, "\r")] {
					return StatusAppended, nil
				}
			}
			return StatusUnchanged, nil
		}
		return StatusAppended, nil
	default:
		return "", fmt.Errorf("unsupported conflict strategy: %q", strategy)
	}
}
