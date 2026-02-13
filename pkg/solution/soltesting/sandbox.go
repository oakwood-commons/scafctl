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
	preSnapshot  map[string]fileSnapshot
}

// NewSandbox creates a new sandbox by copying the solution file, bundle files,
// and test-specific files into an isolated temporary directory.
// All paths are relative to solutionDir (the directory containing the solution file).
// Symlinks and path traversal above the solution root are rejected.
func NewSandbox(solutionPath string, bundleFiles, testFiles []string) (*Sandbox, error) {
	solutionDir := filepath.Dir(solutionPath)
	solutionBase := filepath.Base(solutionPath)

	tmpDir, err := os.MkdirTemp("", "scafctl-test-*")
	if err != nil {
		return nil, fmt.Errorf("creating sandbox temp dir: %w", err)
	}

	s := &Sandbox{
		root:         tmpDir,
		solutionPath: filepath.Join(tmpDir, solutionBase),
	}

	// Copy the solution file itself
	if err := s.copyFile(solutionDir, solutionBase); err != nil {
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
		if err := s.copyFile(solutionDir, cf); err != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, fmt.Errorf("copying compose file %q: %w", cf, err)
		}
	}

	// Copy bundle files
	for _, f := range bundleFiles {
		if err := s.copyFile(solutionDir, f); err != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, fmt.Errorf("copying bundle file %q: %w", f, err)
		}
	}

	// Copy test files
	for _, f := range testFiles {
		if err := s.copyFile(solutionDir, f); err != nil {
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

	solutionBase := filepath.Base(s.solutionPath)
	child := &Sandbox{
		root:         tmpDir,
		solutionPath: filepath.Join(tmpDir, solutionBase),
	}

	// Copy test-specific files from the original solution directory
	for _, f := range testFiles {
		if err := child.copyFile(solutionDir, f); err != nil {
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
		return fmt.Errorf("path traversal rejected: %q", relPath)
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
		srcFile, err := os.Open(path)
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
