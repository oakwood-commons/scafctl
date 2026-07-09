// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Snapshot normalization placeholders.
const (
	TimestampPlaceholder = "<TIMESTAMP>"
	UUIDPlaceholder      = "<UUID>"
	SandboxPlaceholder   = "<SANDBOX>"
)

// Preset mask names.
const (
	presetTimestamp = "timestamp"
	presetUUID      = "uuid"
	presetSandbox   = "sandbox"
	presetEmail     = "email"
	presetIPv4      = "ipv4"
	presetMAC       = "mac"
)

// Normalization patterns.
var (
	// ISO-8601 timestamps: 2024-01-15T10:30:00Z, 2024-01-15T10:30:00+05:00, etc.
	timestampRegex = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[Z+\-\d:]*`)
	// UUIDs: 8-4-4-4-12 hex pattern.
	uuidRegex = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	// Email addresses.
	emailRegex = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	// IPv4 addresses.
	ipv4Regex = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	// MAC addresses (colon or hyphen separated).
	macRegex = regexp.MustCompile(`\b(?:[0-9a-fA-F]{2}[:-]){5}[0-9a-fA-F]{2}\b`)
)

// presetMask is a named normalization rule. Built-in presets are enabled by
// default; catalog presets are opt-in via a mask with use: <name>. The sandbox
// preset is special-cased because it replaces a dynamic path rather than a
// static regex.
type presetMask struct {
	name        string
	builtin     bool
	regex       *regexp.Regexp
	placeholder string
}

// presetList is the deterministic order in which preset masks are applied.
// Built-ins run first (preserving historical behavior), then catalog presets.
var presetList = []presetMask{
	{name: presetTimestamp, builtin: true, regex: timestampRegex, placeholder: TimestampPlaceholder},
	{name: presetUUID, builtin: true, regex: uuidRegex, placeholder: UUIDPlaceholder},
	{name: presetSandbox, builtin: true, regex: nil, placeholder: SandboxPlaceholder},
	{name: presetEmail, builtin: false, regex: emailRegex, placeholder: "<EMAIL>"},
	{name: presetIPv4, builtin: false, regex: ipv4Regex, placeholder: "<IPV4>"},
	{name: presetMAC, builtin: false, regex: macRegex, placeholder: "<MAC>"},
}

// IsKnownPreset reports whether name is a valid preset mask name.
func IsKnownPreset(name string) bool {
	for _, p := range presetList {
		if p.name == name {
			return true
		}
	}
	return false
}

// PresetNames returns the list of valid preset mask names.
func PresetNames() []string {
	names := make([]string, len(presetList))
	for i, p := range presetList {
		names[i] = p.name
	}
	return names
}

// compiledMask is a user-supplied custom mask with its pattern compiled.
type compiledMask struct {
	key         string
	re          *regexp.Regexp
	placeholder string
	path        string
}

// maskSet holds the resolved masking configuration for a single test: which
// preset masks are enabled and the compiled custom masks in declared order.
type maskSet struct {
	enabled map[string]bool
	custom  []compiledMask
}

// buildMaskSet resolves the enabled presets and compiles custom masks. Built-in
// presets start enabled; a mask with use: <preset> toggles a preset on (or off
// when disabled: true). Custom masks (pattern + placeholder) are compiled in
// declared order. Invalid custom patterns are skipped defensively (they are
// rejected earlier by Mask.Validate).
func buildMaskSet(masks []Mask) maskSet {
	enabled := make(map[string]bool, len(presetList))
	for _, p := range presetList {
		enabled[p.name] = p.builtin
	}
	var custom []compiledMask
	for _, m := range masks {
		if m.Use != "" {
			enabled[m.Use] = !m.Disabled
			continue
		}
		re, err := regexp.Compile(m.Pattern)
		if err != nil {
			continue
		}
		custom = append(custom, compiledMask{
			key:         maskKey(m),
			re:          re,
			placeholder: m.Placeholder,
			path:        m.Path,
		})
	}
	return maskSet{enabled: enabled, custom: custom}
}

// apply normalizes input and applies the enabled preset masks followed by the
// custom masks, accumulating per-mask replacement counts. filePath scopes
// path-based custom masks: when empty (stdout source), path-scoped masks are
// skipped; when set (file source), a path-scoped mask applies only if its glob
// matches filePath.
func (ms maskSet) apply(input, sandboxPath, filePath string, counts map[string]int) string {
	result := normalizeJSON(input)

	for _, p := range presetList {
		if !ms.enabled[p.name] {
			continue
		}
		result = applyPreset(result, p, sandboxPath, counts)
	}

	for _, cm := range ms.custom {
		if cm.path != "" {
			if filePath == "" {
				continue
			}
			ok, err := doublestar.Match(cm.path, filePath)
			if err != nil || !ok {
				continue
			}
		}
		matches := cm.re.FindAllString(result, -1)
		if len(matches) > 0 {
			counts[cm.key] += len(matches)
			result = cm.re.ReplaceAllString(result, cm.placeholder)
		}
	}

	return result
}

// applyPreset applies a single preset mask, updating counts.
func applyPreset(input string, p presetMask, sandboxPath string, counts map[string]int) string {
	if p.name == presetSandbox {
		if sandboxPath == "" {
			return input
		}
		if n := strings.Count(input, sandboxPath); n > 0 {
			counts[p.name] += n
			input = strings.ReplaceAll(input, sandboxPath, p.placeholder)
		}
		return input
	}
	if matches := p.regex.FindAllString(input, -1); len(matches) > 0 {
		counts[p.name] += len(matches)
		input = p.regex.ReplaceAllString(input, p.placeholder)
	}
	return input
}

// maskKey returns the reporting key for a custom mask: its Name if set,
// otherwise its Pattern.
func maskKey(m Mask) string {
	if m.Name != "" {
		return m.Name
	}
	return m.Pattern
}

// CompareSnapshot normalizes actual (stdout source), reads the golden file at
// snapshotPath, and compares them. Returns (match, unifiedDiff, maskCounts,
// error).
func CompareSnapshot(actual, snapshotPath, sandboxPath string, masks []Mask) (bool, string, map[string]int, error) {
	counts := map[string]int{}
	normalized := buildMaskSet(masks).apply(actual, sandboxPath, "", counts)
	match, diff, err := compareNormalized(normalized, snapshotPath)
	return match, diff, counts, err
}

// UpdateSnapshot normalizes the actual output (stdout source) and writes it to
// snapshotPath. Returns the per-mask replacement counts.
func UpdateSnapshot(actual, snapshotPath, sandboxPath string, masks []Mask) (map[string]int, error) {
	counts := map[string]int{}
	normalized := buildMaskSet(masks).apply(actual, sandboxPath, "", counts)
	return counts, writeNormalized(normalized, snapshotPath)
}

// CompareFileSnapshot builds a deterministic manifest from the rendered files,
// applying masks (including path-scoped) per file, then compares it against the
// golden file. Returns (match, unifiedDiff, maskCounts, error).
func CompareFileSnapshot(files map[string]FileInfo, snapshotPath, sandboxPath string, masks []Mask) (bool, string, map[string]int, error) {
	manifest, counts := BuildFileManifest(files, sandboxPath, masks)
	match, diff, err := compareNormalized(manifest, snapshotPath)
	return match, diff, counts, err
}

// UpdateFileSnapshot builds the rendered-file manifest and writes it to
// snapshotPath. Returns the per-mask replacement counts.
func UpdateFileSnapshot(files map[string]FileInfo, snapshotPath, sandboxPath string, masks []Mask) (map[string]int, error) {
	manifest, counts := BuildFileManifest(files, sandboxPath, masks)
	return counts, writeNormalized(manifest, snapshotPath)
}

// BuildFileManifest serializes the rendered files into a deterministic text
// manifest. Files are sorted by path; each is emitted as a "=== path ==="
// header followed by its masked, normalized content. Path-scoped masks apply
// only to files whose path matches the mask glob.
func BuildFileManifest(files map[string]FileInfo, sandboxPath string, masks []Mask) (string, map[string]int) {
	ms := buildMaskSet(masks)
	counts := map[string]int{}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var b strings.Builder
	for _, p := range paths {
		normalized := ms.apply(files[p].Content, sandboxPath, p, counts)
		fmt.Fprintf(&b, "=== %s ===\n", p)
		b.WriteString(normalized)
		if !strings.HasSuffix(normalized, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String(), counts
}

// compareNormalized compares an already-normalized string against the golden
// file at snapshotPath. Returns (match, unifiedDiff, error).
func compareNormalized(normalized, snapshotPath string) (bool, string, error) {
	expected, err := os.ReadFile(snapshotPath)
	if err != nil {
		return false, "", fmt.Errorf("reading snapshot file %q: %w", snapshotPath, err)
	}

	expectedStr := string(expected)
	if normalized == expectedStr {
		return true, "", nil
	}

	return false, unifiedDiff(expectedStr, normalized, snapshotPath), nil
}

// writeNormalized writes a normalized string to snapshotPath, creating parent
// directories as needed.
func writeNormalized(normalized, snapshotPath string) error {
	dir := filepath.Dir(snapshotPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating snapshot directory: %w", err)
	}

	if err := os.WriteFile(snapshotPath, []byte(normalized), 0o600); err != nil {
		return fmt.Errorf("writing snapshot file %q: %w", snapshotPath, err)
	}

	return nil
}

// Normalize applies the default normalization pipeline (JSON key sorting plus
// the enabled built-in preset masks) to the input string. It is retained for
// callers that need normalization without custom masks.
func Normalize(input, sandboxPath string) string {
	return buildMaskSet(nil).apply(input, sandboxPath, "", map[string]int{})
}

// normalizeJSON attempts to parse the input as JSON and re-serialize it
// with sorted keys. If the input is not valid JSON, it is returned unchanged.
func normalizeJSON(input string) string {
	trimmed := strings.TrimSpace(input)
	if len(trimmed) == 0 {
		return input
	}

	// Try as JSON object.
	if trimmed[0] == '{' || trimmed[0] == '[' {
		var parsed any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return input
		}
		sorted, err := json.MarshalIndent(parsed, "", "  ")
		if err != nil {
			return input
		}
		// Preserve trailing newline if present in original.
		result := string(sorted)
		if strings.HasSuffix(input, "\n") {
			result += "\n"
		}
		return result
	}

	return input
}

// unifiedDiff produces a unified diff between expected and actual strings.
func unifiedDiff(expected, actual, snapshotPath string) string {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")

	var b strings.Builder
	fmt.Fprintf(&b, "--- expected (%s)\n", snapshotPath)
	b.WriteString("+++ actual\n")

	// Simple line-by-line diff with context.
	const contextLines = 3

	// Build a list of diff lines using a basic LCS-based approach.
	changes := computeDiffLines(expectedLines, actualLines)

	// Emit hunks with context.
	i := 0
	for i < len(changes) {
		// Find next changed line.
		start := i
		for start < len(changes) && changes[start].op == ' ' {
			start++
		}
		if start >= len(changes) {
			break
		}

		// Include context before.
		ctxStart := start - contextLines
		if ctxStart < 0 {
			ctxStart = 0
		}
		if ctxStart < i {
			ctxStart = i
		}

		// Find end of this change group (including context after).
		end := start
		for end < len(changes) {
			if changes[end].op != ' ' {
				// Extend past changed lines.
				end++
				continue
			}
			// Count following context lines.
			ctxCount := 0
			j := end
			for j < len(changes) && changes[j].op == ' ' {
				ctxCount++
				j++
			}
			if ctxCount <= contextLines*2 && j < len(changes) {
				// Not enough gap; merge with next hunk.
				end = j
				continue
			}
			// End of hunk; include trailing context.
			end += min(ctxCount, contextLines)
			break
		}
		if end > len(changes) {
			end = len(changes)
		}

		// Emit hunk.
		for k := ctxStart; k < end; k++ {
			b.WriteByte(changes[k].op)
			b.WriteString(changes[k].line)
			b.WriteByte('\n')
		}

		i = end
	}

	return b.String()
}

// computeDiffLines computes a simple diff between two sets of lines.
// Uses a basic algorithm: walk through both sides, emit matching lines
// as context, non-matching lines as removals/additions.
func computeDiffLines(expected, actual []string) []diffLine {
	// Use Myers-like approach with a simple LCS table for small inputs,
	// falling back to a line-by-line comparison.
	lcs := computeLCS(expected, actual)
	var result []diffLine

	ei, ai, li := 0, 0, 0
	for li < len(lcs) {
		// Emit deletions from expected until we reach the next LCS line.
		for ei < len(expected) && expected[ei] != lcs[li] {
			result = append(result, diffLine{op: '-', line: expected[ei]})
			ei++
		}
		// Emit additions from actual until we reach the next LCS line.
		for ai < len(actual) && actual[ai] != lcs[li] {
			result = append(result, diffLine{op: '+', line: actual[ai]})
			ai++
		}
		// Emit the common line.
		result = append(result, diffLine{op: ' ', line: lcs[li]})
		ei++
		ai++
		li++
	}
	// Emit remaining lines.
	for ei < len(expected) {
		result = append(result, diffLine{op: '-', line: expected[ei]})
		ei++
	}
	for ai < len(actual) {
		result = append(result, diffLine{op: '+', line: actual[ai]})
		ai++
	}

	return result
}

type diffLine struct {
	op   byte
	line string
}

// computeLCS computes the longest common subsequence of two string slices.
// Limited to reasonable sizes to avoid excessive memory usage.
func computeLCS(a, b []string) []string {
	m, n := len(a), len(b)

	// For very large inputs, fall back to empty LCS (full diff).
	// Guard against integer overflow in the m*n multiplication.
	if m > 0 && n > 1_000_000/m {
		return nil
	}

	// Standard DP approach.
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				dp[i][j] = max(dp[i-1][j], dp[i][j-1])
			}
		}
	}

	// Backtrack to find the LCS.
	lcs := make([]string, 0, dp[m][n])
	i, j := m, n
	for i > 0 && j > 0 {
		switch {
		case a[i-1] == b[j-1]:
			lcs = append(lcs, a[i-1])
			i--
			j--
		case dp[i-1][j] > dp[i][j-1]:
			i--
		default:
			j--
		}
	}

	// Reverse.
	for left, right := 0, len(lcs)-1; left < right; left, right = left+1, right-1 {
		lcs[left], lcs[right] = lcs[right], lcs[left]
	}

	return lcs
}
