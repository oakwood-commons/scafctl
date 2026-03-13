// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package directoryprovider

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

// ProviderName is the name of this provider.
const ProviderName = "directory"

const (
	// DefaultMaxDepth is the default recursion depth for directory listing.
	DefaultMaxDepth = 10
	// MaxAllowedDepth is the absolute maximum recursion depth.
	MaxAllowedDepth = 50
	// DefaultMaxFileSize is the default max file size for content reading (1 MB).
	DefaultMaxFileSize = 1 << 20
	// binaryDetectSize is the number of bytes to sample for binary detection.
	binaryDetectSize = 8192
)

// DirectoryProvider provides directory operations.
type DirectoryProvider struct {
	descriptor *provider.Descriptor
}

// NewDirectoryProvider creates a new directory provider instance.
func NewDirectoryProvider() *DirectoryProvider {
	version := semver.MustParse("1.0.0")

	return &DirectoryProvider{
		descriptor: &provider.Descriptor{
			Name:        ProviderName,
			DisplayName: "Directory Provider",
			Description: "Provider for directory operations: listing contents with filtering, creating, removing, and copying directories",
			APIVersion:  "v1",
			Version:     version,
			Category:    "filesystem",
			Tags:        []string{"directory", "filesystem", "listing", "glob", "scan"},
			MockBehavior: "Returns mock directory listing without reading actual filesystem for list operations; " +
				"reports intended action without modifying filesystem for mkdir/rmdir/copy",
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,   // list operation
				provider.CapabilityAction, // mkdir, rmdir, copy operations
			},
			Schema: schemahelper.ObjectSchema([]string{"operation", "path"}, map[string]*jsonschema.Schema{
				"operation": schemahelper.StringProp("Operation to perform",
					schemahelper.WithExample("list"),
					schemahelper.WithEnum("list", "mkdir", "rmdir", "copy")),
				"path": schemahelper.StringProp("Target directory path (absolute or relative)",
					schemahelper.WithExample("./src"),
					schemahelper.WithMaxLength(4096)),
				"recursive": schemahelper.BoolProp("Enable recursive directory traversal",
					schemahelper.WithDefault(false)),
				"maxDepth": schemahelper.IntProp("Maximum recursion depth (1-50)",
					schemahelper.WithDefault(DefaultMaxDepth),
					schemahelper.WithMinimum(1),
					schemahelper.WithMaximum(MaxAllowedDepth)),
				"includeContent": schemahelper.BoolProp("Read and include file contents in output",
					schemahelper.WithDefault(false)),
				"maxFileSize": schemahelper.IntProp("Maximum file size in bytes for content reading; files exceeding this are skipped",
					schemahelper.WithDefault(DefaultMaxFileSize),
					schemahelper.WithMinimum(0)),
				"filterGlob": schemahelper.StringProp("Glob pattern to filter entries (e.g., '*.go', '**/*.yaml'). Mutually exclusive with filterRegex",
					schemahelper.WithExample("*.go"),
					schemahelper.WithMaxLength(500)),
				"filterRegex": schemahelper.StringProp("Regular expression to filter entry names. Mutually exclusive with filterGlob",
					schemahelper.WithExample("^test_.*\\.py$"),
					schemahelper.WithMaxLength(500)),
				"excludeHidden": schemahelper.BoolProp("Exclude hidden files and directories (names starting with '.')",
					schemahelper.WithDefault(false)),
				"checksum": schemahelper.StringProp("Compute checksum for files (requires includeContent). Supported: sha256, sha512",
					schemahelper.WithEnum("sha256", "sha512")),
				"createDirs": schemahelper.BoolProp("Create parent directories for mkdir (like mkdir -p)",
					schemahelper.WithDefault(false)),
				"destination": schemahelper.StringProp("Destination path for copy operation",
					schemahelper.WithMaxLength(4096)),
				"force": schemahelper.BoolProp("Force removal of non-empty directories for rmdir",
					schemahelper.WithDefault(false)),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"entries": schemahelper.ArrayProp("List of directory entries",
						schemahelper.WithItems(schemahelper.ObjectProp("A directory entry",
							nil,
							map[string]*jsonschema.Schema{
								"path":              schemahelper.StringProp("Relative path from the listed directory"),
								"absolutePath":      schemahelper.StringProp("Absolute filesystem path"),
								"name":              schemahelper.StringProp("File or directory name"),
								"extension":         schemahelper.StringProp("File extension including dot (e.g., '.go')"),
								"size":              schemahelper.IntProp("Size in bytes"),
								"isDir":             schemahelper.BoolProp("Whether this entry is a directory"),
								"type":              schemahelper.StringProp("Entry type: file or dir"),
								"mode":              schemahelper.StringProp("File permission mode (e.g., '0644')"),
								"modTime":           schemahelper.StringProp("Last modification time in RFC3339 format"),
								"mimeType":          schemahelper.StringProp("MIME type based on file extension"),
								"content":           schemahelper.StringProp("File content (only when includeContent is true)"),
								"contentEncoding":   schemahelper.StringProp("Content encoding: text or base64"),
								"checksum":          schemahelper.StringProp("File checksum (only when checksum algorithm is specified)"),
								"checksumAlgorithm": schemahelper.StringProp("Checksum algorithm used"),
							},
						))),
					"totalCount": schemahelper.IntProp("Total number of entries"),
					"dirCount":   schemahelper.IntProp("Number of directories"),
					"fileCount":  schemahelper.IntProp("Number of files"),
					"totalSize":  schemahelper.IntProp("Total size of all files in bytes"),
					"basePath":   schemahelper.StringProp("Absolute path of the listed directory"),
				}),
				provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"success":   schemahelper.BoolProp("Whether the operation succeeded"),
					"operation": schemahelper.StringProp("Operation that was performed"),
					"path":      schemahelper.StringProp("Absolute path of the target directory"),
				}),
			},
			Examples: []provider.Example{
				{
					Name:        "List directory contents",
					Description: "List files and directories in the current directory",
					YAML: `name: list-src
provider: directory
inputs:
  operation: list
  path: ./src`,
				},
				{
					Name:        "Recursive listing with glob filter",
					Description: "Recursively list all Go files in a directory",
					YAML: `name: find-go-files
provider: directory
inputs:
  operation: list
  path: ./pkg
  recursive: true
  filterGlob: "*.go"`,
				},
				{
					Name:        "List with file contents",
					Description: "List YAML files and include their contents",
					YAML: `name: read-configs
provider: directory
inputs:
  operation: list
  path: ./config
  recursive: true
  includeContent: true
  filterGlob: "*.yaml"
  maxFileSize: 524288`,
				},
				{
					Name:        "Create directory",
					Description: "Create a nested directory structure",
					YAML: `name: create-output-dir
provider: directory
inputs:
  operation: mkdir
  path: ./output/reports/2026
  createDirs: true`,
				},
				{
					Name:        "Remove directory",
					Description: "Force-remove a directory and all its contents",
					YAML: `name: cleanup-temp
provider: directory
inputs:
  operation: rmdir
  path: ./tmp/build-output
  force: true`,
				},
				{
					Name:        "Copy directory",
					Description: "Copy a directory tree to a new location",
					YAML: `name: backup-config
provider: directory
inputs:
  operation: copy
  path: ./config
  destination: ./config-backup`,
				},
			},
		},
	}
}

// Descriptor returns the provider's descriptor.
func (p *DirectoryProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute performs the directory operation.
func (p *DirectoryProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
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

	absPath, err := provider.ResolvePath(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid path: %w", ProviderName, err)
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName, "operation", operation, "path", path)

	if dryRun := provider.DryRunFromContext(ctx); dryRun {
		result, dryErr := p.executeDryRun(ctx, operation, absPath, inputs)
		if dryErr != nil {
			return nil, fmt.Errorf("%s: %w", ProviderName, dryErr)
		}
		lgr.V(1).Info("provider completed (dry-run)", "provider", ProviderName, "operation", operation)
		return result, nil
	}

	var result *provider.Output
	switch operation {
	case "list":
		result, err = p.executeList(absPath, inputs)
	case "mkdir":
		result, err = p.executeMkdir(absPath, inputs)
	case "rmdir":
		result, err = p.executeRmdir(absPath, inputs)
	case "copy":
		result, err = p.executeCopy(ctx, absPath, inputs)
	default:
		return nil, fmt.Errorf("%s: unsupported operation: %s", ProviderName, operation)
	}

	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName, "operation", operation)
	return result, nil
}

// listOptions holds parsed options for the list operation.
type listOptions struct {
	recursive      bool
	maxDepth       int
	includeContent bool
	maxFileSize    int64
	filterGlob     string
	filterRegex    *regexp.Regexp
	excludeHidden  bool
	checksum       string
}

// entryInfo holds information about a single directory entry.
type entryInfo struct {
	Path              string `json:"path"`
	AbsolutePath      string `json:"absolutePath"`
	Name              string `json:"name"`
	Extension         string `json:"extension"`
	Size              int64  `json:"size"`
	IsDir             bool   `json:"isDir"`
	Type              string `json:"type"`
	Mode              string `json:"mode"`
	ModTime           string `json:"modTime"`
	MimeType          string `json:"mimeType,omitempty"`
	Content           string `json:"content,omitempty"`
	ContentEncoding   string `json:"contentEncoding,omitempty"`
	Checksum          string `json:"checksum,omitempty"`
	ChecksumAlgorithm string `json:"checksumAlgorithm,omitempty"`
}

// parseListOptions parses and validates list operation inputs.
func parseListOptions(inputs map[string]any) (*listOptions, error) {
	opts := &listOptions{
		maxDepth:    DefaultMaxDepth,
		maxFileSize: DefaultMaxFileSize,
	}

	if v, ok := inputs["recursive"].(bool); ok {
		opts.recursive = v
	}

	if v, ok := inputs["maxDepth"]; ok {
		depth, err := toInt(v)
		if err != nil {
			return nil, fmt.Errorf("maxDepth must be an integer: %w", err)
		}
		if depth < 1 || depth > MaxAllowedDepth {
			return nil, fmt.Errorf("maxDepth must be between 1 and %d, got %d", MaxAllowedDepth, depth)
		}
		opts.maxDepth = depth
	}

	if v, ok := inputs["includeContent"].(bool); ok {
		opts.includeContent = v
	}

	if v, ok := inputs["maxFileSize"]; ok {
		size, err := toInt64(v)
		if err != nil {
			return nil, fmt.Errorf("maxFileSize must be an integer: %w", err)
		}
		if size < 0 {
			return nil, fmt.Errorf("maxFileSize must be non-negative, got %d", size)
		}
		opts.maxFileSize = size
	}

	if v, ok := inputs["filterGlob"].(string); ok && v != "" {
		opts.filterGlob = v
	}

	if v, ok := inputs["filterRegex"].(string); ok && v != "" {
		if opts.filterGlob != "" {
			return nil, fmt.Errorf("filterGlob and filterRegex are mutually exclusive")
		}
		re, err := regexp.Compile(v)
		if err != nil {
			return nil, fmt.Errorf("invalid filterRegex: %w", err)
		}
		opts.filterRegex = re
	}

	if v, ok := inputs["excludeHidden"].(bool); ok {
		opts.excludeHidden = v
	}

	if v, ok := inputs["checksum"].(string); ok && v != "" {
		switch v {
		case "sha256", "sha512":
			opts.checksum = v
		default:
			return nil, fmt.Errorf("unsupported checksum algorithm: %s (supported: sha256, sha512)", v)
		}
	}

	return opts, nil
}

// executeList performs the list operation.
func (p *DirectoryProvider) executeList(absPath string, inputs map[string]any) (*provider.Output, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("directory does not exist: %s", absPath)
		}
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", absPath)
	}

	opts, err := parseListOptions(inputs)
	if err != nil {
		return nil, err
	}

	var entries []map[string]any
	var warnings []string
	var dirCount, fileCount int
	var totalSize int64

	walkErr := p.walkDirectory(absPath, absPath, 0, opts, func(entry entryInfo) {
		m := entryToMap(entry)
		entries = append(entries, m)
		if entry.IsDir {
			dirCount++
		} else {
			fileCount++
			totalSize += entry.Size
		}
	}, &warnings)

	if walkErr != nil {
		return nil, walkErr
	}

	if entries == nil {
		entries = []map[string]any{}
	}

	output := &provider.Output{
		Data: map[string]any{
			"entries":    entries,
			"totalCount": dirCount + fileCount,
			"dirCount":   dirCount,
			"fileCount":  fileCount,
			"totalSize":  totalSize,
			"basePath":   absPath,
		},
	}

	if len(warnings) > 0 {
		output.Warnings = warnings
	}

	return output, nil
}

// walkDirectory recursively traverses a directory tree.
func (p *DirectoryProvider) walkDirectory(
	basePath, currentPath string,
	depth int,
	opts *listOptions,
	emit func(entryInfo),
	warnings *[]string,
) error {
	dirEntries, err := os.ReadDir(currentPath)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", currentPath, err)
	}

	for _, de := range dirEntries {
		name := de.Name()

		// Skip hidden files/dirs if requested
		if opts.excludeHidden && strings.HasPrefix(name, ".") {
			continue
		}

		// Skip symlinks
		if de.Type()&fs.ModeSymlink != 0 {
			continue
		}

		entryPath := filepath.Join(currentPath, name)
		relPath, _ := filepath.Rel(basePath, entryPath)

		fi, err := de.Info()
		if err != nil {
			*warnings = append(*warnings, fmt.Sprintf("failed to stat %s: %v", relPath, err))
			continue
		}

		// Skip symlinks detected via Lstat (ReadDir uses Lstat)
		if fi.Mode()&fs.ModeSymlink != 0 {
			continue
		}

		isDir := fi.IsDir()

		// Apply filters (to file/dir name)
		if !matchesFilter(name, opts) {
			// For directories in recursive mode, still recurse into them even if the dir name
			// doesn't match the filter — the filter applies to leaf names
			if isDir && opts.recursive && depth < opts.maxDepth {
				if walkErr := p.walkDirectory(basePath, entryPath, depth+1, opts, emit, warnings); walkErr != nil {
					*warnings = append(*warnings, fmt.Sprintf("error traversing %s: %v", relPath, walkErr))
				}
			}
			continue
		}

		entry := entryInfo{
			Path:         filepath.ToSlash(relPath),
			AbsolutePath: entryPath,
			Name:         name,
			Size:         fi.Size(),
			IsDir:        isDir,
			Mode:         fmt.Sprintf("%04o", fi.Mode().Perm()),
			ModTime:      fi.ModTime().UTC().Format(time.RFC3339),
		}

		if isDir {
			entry.Type = "dir"
		} else {
			entry.Extension = filepath.Ext(name)
			if entry.Extension != "" {
				entry.MimeType = mime.TypeByExtension(entry.Extension)
			}
			entry.Type = "file"

			// Read content if requested
			if opts.includeContent && fi.Size() <= opts.maxFileSize {
				content, encoding, readErr := readFileContent(entryPath)
				if readErr != nil {
					*warnings = append(*warnings, fmt.Sprintf("failed to read %s: %v", relPath, readErr))
				} else {
					entry.Content = content
					entry.ContentEncoding = encoding
				}
			} else if opts.includeContent && fi.Size() > opts.maxFileSize {
				*warnings = append(*warnings, fmt.Sprintf("skipped content for %s: size %d exceeds maxFileSize %d", relPath, fi.Size(), opts.maxFileSize))
			}

			// Compute checksum if requested and content was read
			if opts.checksum != "" && opts.includeContent && entry.Content != "" {
				cs, csErr := computeChecksum(entryPath, opts.checksum)
				if csErr != nil {
					*warnings = append(*warnings, fmt.Sprintf("failed to compute checksum for %s: %v", relPath, csErr))
				} else {
					entry.Checksum = cs
					entry.ChecksumAlgorithm = opts.checksum
				}
			}
		}

		emit(entry)

		// Recurse into subdirectories
		if isDir && opts.recursive && depth < opts.maxDepth {
			if walkErr := p.walkDirectory(basePath, entryPath, depth+1, opts, emit, warnings); walkErr != nil {
				*warnings = append(*warnings, fmt.Sprintf("error traversing %s: %v", relPath, walkErr))
			}
		}
	}

	return nil
}

// matchesFilter checks if a filename matches the configured filter.
func matchesFilter(name string, opts *listOptions) bool {
	if opts.filterGlob != "" {
		matched, err := filepath.Match(opts.filterGlob, name)
		if err != nil {
			return false
		}
		return matched
	}

	if opts.filterRegex != nil {
		return opts.filterRegex.MatchString(name)
	}

	return true
}

// entryToMap converts an entryInfo struct to a map[string]any.
func entryToMap(e entryInfo) map[string]any {
	m := map[string]any{
		"path":         e.Path,
		"absolutePath": e.AbsolutePath,
		"name":         e.Name,
		"extension":    e.Extension,
		"size":         e.Size,
		"isDir":        e.IsDir,
		"type":         e.Type,
		"mode":         e.Mode,
		"modTime":      e.ModTime,
	}

	if e.MimeType != "" {
		m["mimeType"] = e.MimeType
	}
	if e.Content != "" {
		m["content"] = e.Content
		m["contentEncoding"] = e.ContentEncoding
	}
	if e.Checksum != "" {
		m["checksum"] = e.Checksum
		m["checksumAlgorithm"] = e.ChecksumAlgorithm
	}

	return m
}

// readFileContent reads a file and returns its content as a string plus encoding type.
// Binary files are base64-encoded; text files are returned as-is.
func readFileContent(path string) (content, encoding string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}

	if isBinary(data) {
		return base64.StdEncoding.EncodeToString(data), "base64", nil
	}

	return string(data), "text", nil
}

// isBinary detects whether data is binary by checking for null bytes in the
// first binaryDetectSize bytes.
func isBinary(data []byte) bool {
	sample := data
	if len(sample) > binaryDetectSize {
		sample = sample[:binaryDetectSize]
	}

	for _, b := range sample {
		if b == 0 {
			return true
		}
	}

	return false
}

// computeChecksum computes a hash of the file at path using the given algorithm.
func computeChecksum(path, algorithm string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var h hash.Hash
	switch algorithm {
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	default:
		return "", fmt.Errorf("unsupported algorithm: %s", algorithm)
	}

	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// executeMkdir creates a directory.
func (p *DirectoryProvider) executeMkdir(absPath string, inputs map[string]any) (*provider.Output, error) {
	createDirs, _ := inputs["createDirs"].(bool)

	if createDirs {
		if err := os.MkdirAll(absPath, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create directories: %w", err)
		}
	} else {
		if err := os.Mkdir(absPath, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create directory: %w", err)
		}
	}

	return &provider.Output{
		Data: map[string]any{
			"success":   true,
			"operation": "mkdir",
			"path":      absPath,
		},
	}, nil
}

// executeRmdir removes a directory.
func (p *DirectoryProvider) executeRmdir(absPath string, inputs map[string]any) (*provider.Output, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("directory does not exist: %s", absPath)
		}
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", absPath)
	}

	force, _ := inputs["force"].(bool)

	if force {
		if err := os.RemoveAll(absPath); err != nil {
			return nil, fmt.Errorf("failed to remove directory: %w", err)
		}
	} else {
		if err := os.Remove(absPath); err != nil {
			return nil, fmt.Errorf("failed to remove directory (is it empty?): %w", err)
		}
	}

	return &provider.Output{
		Data: map[string]any{
			"success":   true,
			"operation": "rmdir",
			"path":      absPath,
		},
	}, nil
}

// executeCopy copies a directory tree to a destination.
func (p *DirectoryProvider) executeCopy(ctx context.Context, absPath string, inputs map[string]any) (*provider.Output, error) {
	destination, ok := inputs["destination"].(string)
	if !ok || destination == "" {
		return nil, fmt.Errorf("destination is required for copy operation")
	}

	absDest, err := provider.ResolvePath(ctx, destination)
	if err != nil {
		return nil, fmt.Errorf("invalid destination path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("source directory does not exist: %s", absPath)
		}
		return nil, fmt.Errorf("failed to stat source: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("source is not a directory: %s", absPath)
	}

	if err := copyDir(absPath, absDest); err != nil {
		return nil, fmt.Errorf("failed to copy directory: %w", err)
	}

	return &provider.Output{
		Data: map[string]any{
			"success":     true,
			"operation":   "copy",
			"path":        absPath,
			"destination": absDest,
		},
	}, nil
}

// copyDir recursively copies a directory tree from src to dst.
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode().Perm()); err != nil {
		return fmt.Errorf("creating destination directory: %w", err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		// Skip symlinks
		if entry.Type()&fs.ModeSymlink != 0 {
			continue
		}

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyFile copies a single file from src to dst, preserving permissions.
// It guards against symlink TOCTOU attacks by verifying the opened file
// descriptor matches the pre-open Lstat result.
func copyFile(src, dst string) error {
	// Lstat to reject symlinks before opening.
	lstatInfo, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !lstatInfo.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file: %s", src)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Stat the opened fd. If the path was swapped to a symlink between
	// the Lstat and Open, the fd will point to a different inode.
	openedInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(lstatInfo, openedInfo) {
		return fmt.Errorf("source file changed between check and open (possible symlink attack): %s", src)
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, lstatInfo.Mode().Perm()) //nolint:gosec // G703: dst is a resolved output path from provider config
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return nil
}

// executeDryRun handles dry-run mode for mutating operations.
// List is read-only and executes normally even in dry-run mode.
func (p *DirectoryProvider) executeDryRun(ctx context.Context, operation, absPath string, inputs map[string]any) (*provider.Output, error) {
	switch operation {
	case "list":
		// List is read-only, execute normally even in dry-run
		return p.executeList(absPath, inputs)

	case "mkdir":
		createDirs, _ := inputs["createDirs"].(bool)
		msg := fmt.Sprintf("Would create directory: %s", absPath)
		if createDirs {
			msg = fmt.Sprintf("Would create directory tree: %s", absPath)
		}
		return &provider.Output{
			Data: map[string]any{
				"success":   true,
				"operation": "mkdir",
				"path":      absPath,
				"_dryRun":   true,
				"_message":  msg,
			},
		}, nil

	case "rmdir":
		force, _ := inputs["force"].(bool)
		msg := fmt.Sprintf("Would remove directory: %s", absPath)
		if force {
			msg = fmt.Sprintf("Would force-remove directory and contents: %s", absPath)
		}
		return &provider.Output{
			Data: map[string]any{
				"success":   true,
				"operation": "rmdir",
				"path":      absPath,
				"_dryRun":   true,
				"_message":  msg,
			},
		}, nil

	case "copy":
		destination, ok := inputs["destination"].(string)
		if !ok || destination == "" {
			return nil, fmt.Errorf("destination is required for copy operation")
		}
		absDest, err := provider.ResolvePath(ctx, destination)
		if err != nil {
			return nil, fmt.Errorf("resolving copy destination: %w", err)
		}
		return &provider.Output{
			Data: map[string]any{
				"success":     true,
				"operation":   "copy",
				"path":        absPath,
				"destination": absDest,
				"_dryRun":     true,
				"_message":    fmt.Sprintf("Would copy %s to %s", absPath, absDest),
			},
		}, nil

	default:
		return nil, fmt.Errorf("unsupported operation: %s", operation)
	}
}

// toInt converts a numeric value to int, handling both int and float64 (from JSON).
func toInt(v any) (int, error) {
	switch val := v.(type) {
	case int:
		return val, nil
	case int64:
		return int(val), nil
	case float64:
		return int(val), nil
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return 0, err
		}
		return int(n), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to int", v)
	}
}

// toInt64 converts a numeric value to int64, handling both int and float64 (from JSON).
func toInt64(v any) (int64, error) {
	switch val := v.(type) {
	case int:
		return int64(val), nil
	case int64:
		return val, nil
	case float64:
		return int64(val), nil
	case json.Number:
		return val.Int64()
	default:
		return 0, fmt.Errorf("cannot convert %T to int64", v)
	}
}
