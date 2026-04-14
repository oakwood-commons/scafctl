// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fileprovider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// ConflictStrategy defines how file writes behave when the target already exists.
type ConflictStrategy string

const (
	// ConflictError causes the write to fail when the target file exists.
	ConflictError ConflictStrategy = "error"

	// ConflictOverwrite always replaces the existing file.
	ConflictOverwrite ConflictStrategy = "overwrite"

	// ConflictSkip leaves the existing file untouched.
	ConflictSkip ConflictStrategy = "skip"

	// ConflictSkipUnchanged overwrites only when content differs (default).
	ConflictSkipUnchanged ConflictStrategy = "skip-unchanged"

	// ConflictAppend adds content to the end of the existing file.
	ConflictAppend ConflictStrategy = "append"
)

// IsValid reports whether s is a recognised conflict strategy.
func (s ConflictStrategy) IsValid() bool {
	switch s {
	case ConflictError, ConflictOverwrite, ConflictSkip, ConflictSkipUnchanged, ConflictAppend:
		return true
	}
	return false
}

// OrDefault returns s if it is non-empty, otherwise the default strategy.
func (s ConflictStrategy) OrDefault() ConflictStrategy {
	if s == "" {
		return ConflictStrategy(settings.DefaultConflictStrategy)
	}
	return s
}

// FileWriteStatus describes the outcome of a single file write.
type FileWriteStatus string

const (
	// StatusCreated means a new file was written (target did not exist).
	StatusCreated FileWriteStatus = "created"

	// StatusOverwritten means an existing file was replaced.
	StatusOverwritten FileWriteStatus = "overwritten"

	// StatusSkipped means the existing file was left untouched.
	StatusSkipped FileWriteStatus = "skipped"

	// StatusUnchanged means the existing content already matched the new content.
	StatusUnchanged FileWriteStatus = "unchanged"

	// StatusAppended means new content was appended to the existing file.
	StatusAppended FileWriteStatus = "appended"

	// StatusError means the write would fail (error strategy, file exists).
	// Used only in dry-run mode to accurately represent the planned outcome.
	StatusError FileWriteStatus = "error"
)

// FileWriteResult summarises the outcome of a single file write.
type FileWriteResult struct {
	Path       string          `json:"path" yaml:"path" doc:"Path of the written file" maxLength:"4096" example:"./output/config.yaml"`
	Status     FileWriteStatus `json:"status" yaml:"status" doc:"Outcome of the write operation" maxLength:"16" example:"created"`
	BackupPath string          `json:"backupPath,omitempty" yaml:"backupPath,omitempty" doc:"Path to the backup file, if one was created" maxLength:"4096"`
}

// FileConflictError is a structured error returned when the error conflict strategy
// detects files that already exist. It groups files into changed (different checksum)
// and unchanged (same checksum, silently skipped) for clear error output.
type FileConflictError struct {
	// Changed lists files with different checksums that would be overwritten.
	Changed []string `json:"changed" yaml:"changed" doc:"Files with different content than the source" maxItems:"10000"`
	// Unchanged lists files with identical checksums that were silently skipped.
	Unchanged []string `json:"unchanged" yaml:"unchanged" doc:"Files with identical content (skipped)" maxItems:"10000"`
}

// Error returns a structured, human-readable conflict report.
func (e *FileConflictError) Error() string {
	total := len(e.Changed) + len(e.Unchanged)
	var b strings.Builder

	if total == 1 && len(e.Changed) == 1 {
		fmt.Fprintf(&b, "file already exists (content differs): %s", e.Changed[0])
		b.WriteString("\n\nUse --force to overwrite, or --on-conflict skip-unchanged to skip identical files.")
		return b.String()
	}

	fmt.Fprintf(&b, "%d file(s) already exist", total)
	b.WriteString("\n")

	if len(e.Changed) > 0 {
		b.WriteString("\n  Changed (would overwrite):\n")
		for _, f := range e.Changed {
			fmt.Fprintf(&b, "    %s\n", f)
		}
	}

	if len(e.Unchanged) > 0 {
		b.WriteString("\n  Unchanged (same checksum, skipped):\n")
		for _, f := range e.Unchanged {
			fmt.Fprintf(&b, "    %s\n", f)
		}
	}

	b.WriteString("\nUse --force to overwrite, or --on-conflict skip-unchanged to skip identical files.")
	return b.String()
}

// resolveConflictStrategy resolves the effective conflict strategy from per-entry,
// invocation-level, context-level, and default fallbacks (in priority order).
func resolveConflictStrategy(ctx context.Context, entryLevel, invocationLevel string) ConflictStrategy {
	if entryLevel != "" {
		return ConflictStrategy(entryLevel)
	}
	if invocationLevel != "" {
		return ConflictStrategy(invocationLevel)
	}
	if ctxStrategy, ok := provider.ConflictStrategyFromContext(ctx); ok && ctxStrategy != "" {
		return ConflictStrategy(ctxStrategy)
	}
	return ConflictStrategy(settings.DefaultConflictStrategy)
}

// resolveBackup resolves the effective backup flag from per-entry, invocation-level,
// and context-level fallbacks.
func resolveBackup(ctx context.Context, entryLevel, invocationLevel *bool) bool {
	if entryLevel != nil {
		return *entryLevel
	}
	if invocationLevel != nil {
		return *invocationLevel
	}
	if ctxBackup, ok := provider.BackupFromContext(ctx); ok {
		return ctxBackup
	}
	return false
}

// resolveDedupe resolves the effective dedupe flag from per-entry and invocation-level.
func resolveDedupe(entryLevel, invocationLevel *bool) bool {
	if entryLevel != nil {
		return *entryLevel
	}
	if invocationLevel != nil {
		return *invocationLevel
	}
	return false
}

// boolPtrFromInputs extracts an optional bool from an inputs map.
// Returns nil if the key is absent or not a bool.
func boolPtrFromInputs(inputs map[string]any, key string) *bool {
	v, ok := inputs[key]
	if !ok || v == nil {
		return nil
	}
	b, ok := v.(bool)
	if !ok {
		return nil
	}
	return &b
}

// ensureParentDir creates the parent directory for absPath if it doesn't exist.
func ensureParentDir(absPath, outputPath string) error {
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directories for %s: %w", outputPath, err)
	}
	return nil
}

// fileNeedsNewlineSeparator reports whether the file at absPath is non-empty
// and does not end with a newline. It reads only the last byte to avoid loading
// the entire file into memory.
func fileNeedsNewlineSeparator(absPath string) (bool, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return false, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() == 0 {
		return false, nil
	}

	buf := make([]byte, 1)
	if _, err := f.ReadAt(buf, info.Size()-1); err != nil {
		return false, err
	}
	return buf[0] != '\n', nil
}

// writeFileWithMode writes content to absPath and applies the given file mode.
func writeFileWithMode(absPath string, content []byte, fileMode os.FileMode) error {
	if err := os.WriteFile(absPath, content, fileMode); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	if err := os.Chmod(absPath, fileMode); err != nil {
		return fmt.Errorf("failed to set file permissions: %w", err)
	}
	return nil
}

// contentMatchesFile reports whether newContent is byte-for-byte identical to
// the file at absPath by comparing SHA-256 digests. If absPath does not exist,
// it returns (false, nil) so callers need not check existence first.
func contentMatchesFile(absPath string, newContent []byte) (bool, error) {
	f, err := os.Open(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("open file for comparison: %w", err)
	}
	defer f.Close()

	// Hash the new content in memory.
	newHash := sha256.Sum256(newContent)

	// Stream the existing file through SHA-256.
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, fmt.Errorf("hash existing file: %w", err)
	}

	return bytes.Equal(newHash[:], h.Sum(nil)), nil
}

// backupFile creates a backup copy of absPath. It tries <path>.bak first, then
// .bak.1, .bak.2, … up to settings.DefaultMaxBackups. File permissions are
// preserved on the backup copy. Returns the chosen backup path.
func backupFile(absPath string) (string, error) {
	// Read source file mode before attempting backup.
	srcInfo, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("stat source for backup: %w", err)
	}

	src, err := os.Open(absPath)
	if err != nil {
		return "", fmt.Errorf("open source for backup: %w", err)
	}
	defer src.Close()

	// Build ordered list of candidate backup paths: .bak, .bak.1, .bak.2, …
	candidates := make([]string, 0, settings.DefaultMaxBackups)
	candidates = append(candidates, absPath+".bak")
	for i := 1; i < settings.DefaultMaxBackups; i++ {
		candidates = append(candidates, fmt.Sprintf("%s.bak.%d", absPath, i))
	}

	// Try each candidate with O_CREATE|O_EXCL. This is race-safe: if another
	// process claims a slot between our Stat probe and OpenFile, or without
	// any probe at all, O_EXCL returns EEXIST and we simply try the next slot.
	var dst *os.File
	var backupPath string
	for _, candidate := range candidates {
		f, openErr := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, srcInfo.Mode().Perm())
		if openErr != nil {
			if errors.Is(openErr, os.ErrExist) {
				continue
			}
			return "", fmt.Errorf("create backup file %s: %w", candidate, openErr)
		}
		dst = f
		backupPath = candidate
		break
	}
	if dst == nil {
		return "", fmt.Errorf("backup limit reached for %s: maximum %d backups", absPath, settings.DefaultMaxBackups)
	}

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return "", fmt.Errorf("copy to backup file: %w", err)
	}

	// Flush to disk before closing to ensure backup integrity.
	if err := dst.Sync(); err != nil {
		dst.Close()
		return "", fmt.Errorf("sync backup file: %w", err)
	}
	if err := dst.Close(); err != nil {
		return "", fmt.Errorf("close backup file: %w", err)
	}

	// Explicitly set permissions to match the source file. The os.OpenFile
	// create mode is subject to the process umask, which can strip bits
	// (e.g. 0755 -> 0711). Chmod after close ensures an exact match.
	if err := os.Chmod(backupPath, srcInfo.Mode().Perm()); err != nil {
		return "", fmt.Errorf("set backup permissions: %w", err)
	}

	return backupPath, nil
}

// appendToFile appends content to absPath, optionally deduplicating lines.
// If the file does not exist it is created. Empty content is a no-op.
func appendToFile(absPath string, content []byte, fileMode os.FileMode, dedupe bool) (FileWriteStatus, error) {
	if len(content) == 0 {
		return StatusUnchanged, nil
	}

	// If the file does not exist, just write it.
	if _, err := os.Stat(absPath); errors.Is(err, os.ErrNotExist) {
		if err := writeFileWithMode(absPath, content, fileMode); err != nil {
			return "", fmt.Errorf("create file for append: %w", err)
		}
		return StatusCreated, nil
	}

	if !dedupe {
		return appendRaw(absPath, content, fileMode)
	}
	return appendDedupe(absPath, content, fileMode)
}

// appendRaw appends content directly, inserting a newline separator if the
// existing file does not end with one.
func appendRaw(absPath string, content []byte, fileMode os.FileMode) (FileWriteStatus, error) {
	needsSep, err := fileNeedsNewlineSeparator(absPath)
	if err != nil {
		return "", fmt.Errorf("check trailing newline for append: %w", err)
	}

	var buf []byte
	if needsSep {
		buf = append(buf, '\n')
	}
	buf = append(buf, content...)

	f, err := os.OpenFile(absPath, os.O_APPEND|os.O_WRONLY, fileMode)
	if err != nil {
		return "", fmt.Errorf("open file for append: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(buf); err != nil {
		return "", fmt.Errorf("write append content: %w", err)
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("sync file after append: %w", err)
	}
	if err := os.Chmod(absPath, fileMode); err != nil {
		return "", fmt.Errorf("set file permissions after append: %w", err)
	}
	return StatusAppended, nil
}

// appendDedupe appends only lines from content that are not already present in
// the file. Comparison is case-sensitive after stripping trailing \r.
func appendDedupe(absPath string, content []byte, fileMode os.FileMode) (FileWriteStatus, error) {
	existing, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("read file for dedupe append: %w", err)
	}

	existingLines := strings.Split(string(existing), "\n")
	seen := make(map[string]bool, len(existingLines))
	for _, line := range existingLines {
		seen[strings.TrimRight(line, "\r")] = true
	}

	newLines := strings.Split(string(content), "\n")
	var unique []string
	for _, line := range newLines {
		cleaned := strings.TrimRight(line, "\r")
		if !seen[cleaned] {
			unique = append(unique, line)
			seen[cleaned] = true // prevent duplicates within new content too
		}
	}

	if len(unique) == 0 {
		return StatusUnchanged, nil
	}

	var buf []byte
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		buf = append(buf, '\n')
	}
	buf = append(buf, []byte(strings.Join(unique, "\n"))...)

	f, err := os.OpenFile(absPath, os.O_APPEND|os.O_WRONLY, fileMode)
	if err != nil {
		return "", fmt.Errorf("open file for dedupe append: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(buf); err != nil {
		return "", fmt.Errorf("write dedupe append content: %w", err)
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("sync file after dedupe append: %w", err)
	}
	if err := os.Chmod(absPath, fileMode); err != nil {
		return "", fmt.Errorf("set file permissions after dedupe append: %w", err)
	}
	return StatusAppended, nil
}
