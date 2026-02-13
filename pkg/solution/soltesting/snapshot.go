package soltesting

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Snapshot normalization placeholders.
const (
	TimestampPlaceholder = "<TIMESTAMP>"
	UUIDPlaceholder      = "<UUID>"
	SandboxPlaceholder   = "<SANDBOX>"
)

// Normalization patterns.
var (
	// ISO-8601 timestamps: 2024-01-15T10:30:00Z, 2024-01-15T10:30:00+05:00, etc.
	timestampRegex = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[Z+\-\d:]*`)
	// UUIDs: 8-4-4-4-12 hex pattern.
	uuidRegex = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
)

// CompareSnapshot normalizes actual, reads the golden file at snapshotPath,
// and compares them. Returns (match, unifiedDiff, error).
func CompareSnapshot(actual, snapshotPath, sandboxPath string) (bool, string, error) {
	normalized := Normalize(actual, sandboxPath)

	expected, err := os.ReadFile(snapshotPath)
	if err != nil {
		return false, "", fmt.Errorf("reading snapshot file %q: %w", snapshotPath, err)
	}

	expectedStr := string(expected)
	if normalized == expectedStr {
		return true, "", nil
	}

	diff := unifiedDiff(expectedStr, normalized, snapshotPath)
	return false, diff, nil
}

// UpdateSnapshot normalizes the actual output and writes it to the snapshotPath.
func UpdateSnapshot(actual, snapshotPath, sandboxPath string) error {
	normalized := Normalize(actual, sandboxPath)

	dir := filepath.Dir(snapshotPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating snapshot directory: %w", err)
	}

	if err := os.WriteFile(snapshotPath, []byte(normalized), 0o600); err != nil {
		return fmt.Errorf("writing snapshot file %q: %w", snapshotPath, err)
	}

	return nil
}

// Normalize applies the fixed normalization pipeline to the input string:
//  1. Sort JSON map keys deterministically (if valid JSON).
//  2. Replace ISO-8601 timestamps with <TIMESTAMP>.
//  3. Replace UUIDs with <UUID>.
//  4. Replace sandbox absolute paths with <SANDBOX>.
func Normalize(input, sandboxPath string) string {
	result := normalizeJSON(input)
	result = timestampRegex.ReplaceAllString(result, TimestampPlaceholder)
	result = uuidRegex.ReplaceAllString(result, UUIDPlaceholder)
	if sandboxPath != "" {
		result = strings.ReplaceAll(result, sandboxPath, SandboxPlaceholder)
	}
	return result
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
	b.WriteString(fmt.Sprintf("--- expected (%s)\n", snapshotPath))
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
	if m*n > 1_000_000 {
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
