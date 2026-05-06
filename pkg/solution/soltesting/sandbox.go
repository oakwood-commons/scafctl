// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

const (
	// MaxFileSize is the maximum file size (10MB) before content is replaced with a placeholder.
	MaxFileSize = 10 * 1024 * 1024
	// FileTooLargePlaceholder is the placeholder for files exceeding MaxFileSize.
	FileTooLargePlaceholder = "<file too large>"
	// BinaryFilePlaceholder is the placeholder for non-UTF-8 (binary) files.
	BinaryFilePlaceholder = "<binary file>"
)

// fileSnapshot records a file's state at a point in time.
type fileSnapshot struct {
	ModTime time.Time
	Size    int64
}

// Sandbox provides an isolated temporary directory for test execution.
// It supports file copying, pre/post snapshots for diff detection,
// and cleanup of temporary resources.
type Sandbox struct {
	root         string
	solutionPath string
	baseDir      string // subdirectory prefix for nesting files
	preSnapshot  map[string]fileSnapshot
}

// NewSandbox creates a new sandbox by copying the solution file, bundle files,
// and test-specific files into an isolated temporary directory.
// All paths are relative to solutionDir (the directory containing the solution file).
// Symlinks and path traversal above the solution root are rejected.
func NewSandbox(solutionPath string, bundleFiles, testFiles []string) (*Sandbox, error) {
	return newSandbox(solutionPath, "", bundleFiles, testFiles)
}

// NewSandboxWithBaseDir creates a sandbox where the solution and all related
// files are nested under baseDir within the sandbox root. This preserves
// directory structure for solutions that live in a subdirectory of a repository
// and whose resolvers reference paths relative to the repository root.
//
// For example, with baseDir="cldctl":
//   - solution.yaml  -> sandbox/cldctl/solution.yaml
//   - output/data.json -> sandbox/cldctl/output/data.json
//
// The process working directory (cmd.Dir) should be set to sandbox.Path()
// (the root), so repo-root-relative paths resolve correctly.
func NewSandboxWithBaseDir(solutionPath, baseDir string, bundleFiles, testFiles []string) (*Sandbox, error) {
	if err := validateBaseDir(baseDir); err != nil {
		return nil, err
	}
	return newSandbox(solutionPath, baseDir, bundleFiles, testFiles)
}

// validateBaseDir rejects absolute paths and path-traversal attempts to prevent
// files from being written outside the sandbox root.
func validateBaseDir(baseDir string) error {
	if filepath.IsAbs(baseDir) {
		return fmt.Errorf("baseDir must be a relative path, got %q", baseDir)
	}
	cleaned := filepath.Clean(baseDir)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("baseDir must not traverse above sandbox root, got %q", baseDir)
	}
	return nil
}

// newSandbox is the internal constructor shared by NewSandbox and NewSandboxWithBaseDir.
func newSandbox(solutionPath, baseDir string, bundleFiles, testFiles []string) (*Sandbox, error) {
	solutionDir := filepath.Dir(solutionPath)
	solutionBase := filepath.Base(solutionPath)

	tmpDir, err := os.MkdirTemp("", "scafctl-test-*")
	if err != nil {
		return nil, fmt.Errorf("creating sandbox temp dir: %w", err)
	}

	// When baseDir is set, nest all files under sandbox/<baseDir>/
	// so repo-root-relative paths resolve correctly.
	solutionRelPath := solutionBase
	if baseDir != "" {
		solutionRelPath = filepath.Join(baseDir, solutionBase)
	}

	s := &Sandbox{
		root:         tmpDir,
		solutionPath: filepath.Join(tmpDir, solutionRelPath),
		baseDir:      baseDir,
	}

	// Copy the solution file itself
	if err := s.copyFileWithPrefix(solutionDir, solutionBase, baseDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("copying solution file: %w", err)
	}

	// Detect and copy compose files referenced by the solution
	composeFiles, err := discoverComposeFiles(solutionPath, solutionDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("discovering compose files: %w", err)
	}
	for _, cf := range composeFiles {
		if err := s.copyFileWithPrefix(solutionDir, cf, baseDir); err != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, fmt.Errorf("copying compose file %q: %w", cf, err)
		}
	}

	// Copy bundle files
	for _, f := range bundleFiles {
		if err := s.copyFileWithPrefix(solutionDir, f, baseDir); err != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, fmt.Errorf("copying bundle file %q: %w", f, err)
		}
	}

	// Copy test files (with glob and directory expansion)
	expandedTestFiles, err := resolveFileEntries(solutionDir, testFiles)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("resolving test files: %w", err)
	}
	for _, f := range expandedTestFiles {
		if err := s.copyFileWithPrefix(solutionDir, f, baseDir); err != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, fmt.Errorf("copying test file %q: %w", f, err)
		}
	}

	return s, nil
}

// NewBaseSandbox creates a base sandbox for suite-level setup.
// The base sandbox can be copied per-test via CopyForTest.
func NewBaseSandbox(solutionPath string, bundleFiles []string) (*Sandbox, error) {
	return NewSandbox(solutionPath, bundleFiles, nil)
}

// CopyForTest creates a deep copy of the sandbox into a new temp directory
// and adds test-specific files from the original solution directory.
func (s *Sandbox) CopyForTest(solutionDir string, testFiles []string) (*Sandbox, error) {
	tmpDir, err := os.MkdirTemp("", "scafctl-test-*")
	if err != nil {
		return nil, fmt.Errorf("creating test sandbox temp dir: %w", err)
	}

	// Deep copy the entire base sandbox
	if err := copyDirRecursive(s.root, tmpDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("copying base sandbox: %w", err)
	}

	// Preserve the relative path from root to solution file
	solutionRel, err := filepath.Rel(s.root, s.solutionPath)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("computing relative solution path: %w", err)
	}
	child := &Sandbox{
		root:         tmpDir,
		solutionPath: filepath.Join(tmpDir, solutionRel),
		baseDir:      s.baseDir,
	}

	// Copy test-specific files from the original solution directory (with glob and directory expansion)
	expandedTestFiles, err := resolveFileEntries(solutionDir, testFiles)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("resolving test files: %w", err)
	}
	for _, f := range expandedTestFiles {
		if err := child.copyFileWithPrefix(solutionDir, f, s.baseDir); err != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, fmt.Errorf("copying test file %q: %w", f, err)
		}
	}

	return child, nil
}

// Path returns the sandbox root directory path.
func (s *Sandbox) Path() string {
	return s.root
}

// SolutionPath returns the path to the solution file within the sandbox.
func (s *Sandbox) SolutionPath() string {
	return s.solutionPath
}

// PreSnapshot records all file paths and modification times in the sandbox.
// Call this before executing the test command.
func (s *Sandbox) PreSnapshot() error {
	s.preSnapshot = make(map[string]fileSnapshot)
	return filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return fmt.Errorf("computing relative path for %q: %w", path, err)
		}
		s.preSnapshot[filepath.ToSlash(rel)] = fileSnapshot{
			ModTime: info.ModTime(),
			Size:    info.Size(),
		}
		return nil
	})
}

// PostSnapshot diffs the current sandbox against the pre-snapshot and returns
// only new or modified files. Applies the 10MB size guard and binary file guard.
func (s *Sandbox) PostSnapshot() (map[string]FileInfo, error) {
	if s.preSnapshot == nil {
		return nil, fmt.Errorf("PreSnapshot must be called before PostSnapshot")
	}

	result := make(map[string]FileInfo)
	err := filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return fmt.Errorf("computing relative path for %q: %w", path, err)
		}
		relSlash := filepath.ToSlash(rel)

		// Check if the file is new or modified
		pre, existed := s.preSnapshot[relSlash]
		if existed && pre.ModTime.Equal(info.ModTime()) && pre.Size == info.Size() {
			return nil // unchanged
		}

		// Read file content with guards
		content, err := readFileContent(path, info.Size())
		if err != nil {
			return fmt.Errorf("reading file %q: %w", relSlash, err)
		}

		result[relSlash] = FileInfo{
			Exists:  true,
			Content: content,
		}
		return nil
	})

	return result, err
}

// Cleanup removes the sandbox temporary directory.
func (s *Sandbox) Cleanup() {
	if s.root != "" {
		_ = os.RemoveAll(s.root)
	}
}

// copyFile copies a single file from sourceDir/relPath into the sandbox, preserving
// relative path structure. Rejects symlinks and path traversal.
func (s *Sandbox) copyFile(sourceDir, relPath string) error {
	// Reject path traversal
	cleaned := filepath.Clean(relPath)
	if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return fmt.Errorf(
			"path traversal rejected: %q — test files must be within the solution directory. "+
				"Move or copy the file alongside solution.yaml, or use 'init' steps instead",
			relPath,
		)
	}

	srcPath := filepath.Join(sourceDir, cleaned)

	// Reject symlinks
	info, err := os.Lstat(srcPath)
	if err != nil {
		return fmt.Errorf("stat %q: %w", srcPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlinks are not allowed: %q", relPath)
	}
	if info.IsDir() {
		return fmt.Errorf(
			"is a directory: %q — the 'files' property requires file paths or glob patterns "+
				"(e.g., %q to copy the entire directory tree)",
			relPath, relPath+"/**",
		)
	}

	dstPath := filepath.Join(s.root, cleaned)

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return fmt.Errorf("creating directory for %q: %w", relPath, err)
	}

	// Copy file contents
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("opening source %q: %w", srcPath, err)
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("creating destination %q: %w", dstPath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copying %q: %w", relPath, err)
	}

	return nil
}

// copyFileWithPrefix copies a file from sourceDir/relPath into the sandbox,
// nesting it under prefix/ when prefix is non-empty. This is used by
// NewSandboxWithBaseDir to preserve directory structure.
func (s *Sandbox) copyFileWithPrefix(sourceDir, relPath, prefix string) error {
	if prefix == "" {
		return s.copyFile(sourceDir, relPath)
	}
	// Reject path traversal
	cleaned := filepath.Clean(relPath)
	if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return fmt.Errorf(
			"path traversal rejected: %q — test files must be within the solution directory. "+
				"Move or copy the file alongside solution.yaml, or use 'init' steps instead",
			relPath,
		)
	}

	srcPath := filepath.Join(sourceDir, cleaned)

	// Reject symlinks
	info, err := os.Lstat(srcPath)
	if err != nil {
		return fmt.Errorf("stat %q: %w", srcPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlinks are not allowed: %q", relPath)
	}
	if info.IsDir() {
		return fmt.Errorf(
			"is a directory: %q — the 'files' property requires file paths or glob patterns "+
				"(e.g., %q to copy the entire directory tree)",
			relPath, relPath+"/**",
		)
	}

	// Nest under prefix/
	dstPath := filepath.Join(s.root, prefix, cleaned)

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return fmt.Errorf("creating directory for %q: %w", relPath, err)
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("opening source %q: %w", srcPath, err)
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("creating destination %q: %w", dstPath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copying %q: %w", relPath, err)
	}

	return nil
}

// readFileContent reads a file with size and binary guards.
func readFileContent(path string, size int64) (string, error) {
	if size > MaxFileSize {
		return FileTooLargePlaceholder, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	if !utf8.Valid(data) {
		return BinaryFilePlaceholder, nil
	}

	return string(data), nil
}

// copyDirRecursive copies a directory tree from src to dst.
func copyDirRecursive(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Copy file
		srcFile, err := os.Open(path) //nolint:gosec // G122: path comes from filepath.Walk on a test sandbox directory we own
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		if _, err := io.Copy(dstFile, srcFile); err != nil {
			return err
		}

		return os.Chmod(dstPath, info.Mode())
	})
}

// resolveFileEntries expands a list of file entries (which may include glob patterns
// and directory paths) into a flat list of individual relative file paths.
//
// Supported entry types:
//   - Plain file path: "config.yaml" — used as-is
//   - Glob pattern: "templates/**/*.yaml" — expanded via doublestar
//   - Directory path: "templates/" (trailing slash) or a path that resolves to a directory
//     — recursively expanded to include all files in the tree
//
// All returned paths are relative to sourceDir.
func resolveFileEntries(sourceDir string, entries []string) ([]string, error) {
	var result []string
	seen := make(map[string]bool)

	for _, entry := range entries {
		expanded, err := resolveOneFileEntry(sourceDir, entry)
		if err != nil {
			return nil, err
		}
		for _, p := range expanded {
			if !seen[p] {
				seen[p] = true
				result = append(result, p)
			}
		}
	}

	return result, nil
}

// resolveOneFileEntry resolves a single file entry to one or more relative paths.
func resolveOneFileEntry(sourceDir, entry string) ([]string, error) {
	cleaned := filepath.Clean(entry)

	// Reject path traversal early
	if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		// Let copyFile produce the detailed error
		return []string{entry}, nil
	}

	absPath := filepath.Join(sourceDir, cleaned)

	// Check if it's a directory (either by trailing slash or by stat)
	isDir := strings.HasSuffix(entry, "/") || strings.HasSuffix(entry, string(filepath.Separator))
	if !isDir {
		info, err := os.Stat(absPath)
		if err == nil && info.IsDir() {
			isDir = true
		}
	}

	if isDir {
		return expandDirectory(sourceDir, cleaned)
	}

	// Check if it contains glob characters
	if strings.ContainsAny(entry, "*?[{") {
		return expandGlob(sourceDir, entry)
	}

	// Plain file path — return as-is
	return []string{cleaned}, nil
}

// expandDirectory recursively walks a directory and returns all files as relative paths.
func expandDirectory(sourceDir, relDir string) ([]string, error) {
	absDir := filepath.Join(sourceDir, relDir)
	info, err := os.Stat(absDir)
	if err != nil {
		return nil, fmt.Errorf("directory %q: %w", relDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %q", relDir)
	}

	var result []string
	err = filepath.Walk(absDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			return nil
		}
		// Skip symlinks
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("computing relative path for %q: %w", path, err)
		}
		result = append(result, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory %q: %w", relDir, err)
	}

	return result, nil
}

// expandGlob expands a glob pattern relative to sourceDir into matching file paths.
func expandGlob(sourceDir, pattern string) ([]string, error) {
	absPattern := filepath.Join(sourceDir, pattern)

	matches, err := doublestar.FilepathGlob(absPattern)
	if err != nil {
		return nil, fmt.Errorf("glob pattern %q: %w", pattern, err)
	}

	var result []string
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue // skip unstat-able entries
		}
		if info.IsDir() {
			continue // skip directories, only include files
		}
		rel, err := filepath.Rel(sourceDir, match)
		if err != nil {
			return nil, fmt.Errorf("computing relative path for %q: %w", match, err)
		}
		result = append(result, rel)
	}

	return result, nil
}

// discoverComposeFiles reads a solution file and returns the relative paths
// of all files referenced by the top-level compose field (with glob expansion).
func discoverComposeFiles(solutionPath, solutionDir string) ([]string, error) {
	data, err := os.ReadFile(solutionPath)
	if err != nil {
		return nil, err
	}

	var doc struct {
		Compose []string `yaml:"compose"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	if len(doc.Compose) == 0 {
		return nil, nil
	}

	var result []string
	for _, pattern := range doc.Compose {
		absPattern := pattern
		if !filepath.IsAbs(pattern) {
			absPattern = filepath.Join(solutionDir, pattern)
		}

		matches, err := doublestar.FilepathGlob(absPattern)
		if err != nil {
			return nil, fmt.Errorf("compose glob %q: %w", pattern, err)
		}

		for _, match := range matches {
			rel, err := filepath.Rel(solutionDir, match)
			if err != nil {
				return nil, fmt.Errorf("computing relative path for %q: %w", match, err)
			}
			result = append(result, rel)
		}
	}

	return result, nil
}
