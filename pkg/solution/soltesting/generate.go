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

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"gopkg.in/yaml.v3"
)

// MaxGeneratedAssertions caps the number of assertions produced by the generator.
// The user is expected to curate the generated list before committing it.
const MaxGeneratedAssertions = 20

// maxGenerateDepth is the maximum object depth explored when deriving assertions.
// Depth 0 is the root (__output), depth 1 is top-level keys, depth 2 is their children.
const maxGenerateDepth = 2

// GenerateInput holds the inputs for test case generation.
type GenerateInput struct {
	// Command is the scafctl subcommand path, e.g. ["render", "solution"].
	Command []string `json:"command" yaml:"command" doc:"scafctl subcommand path" maxItems:"10"`

	// Args are the command arguments excluding the -f/--file and -o test flags,
	// e.g. ["-r", "env=prod"]. The generator appends "-o", "json" automatically
	// when no -o flag is already present, so the generated test will use structured
	// output and populate __output for CEL assertions.
	Args []string `json:"args,omitempty" yaml:"args,omitempty" doc:"Command arguments (without -f and -o)" maxItems:"50"`

	// TestName is the desired test name. When empty, one is derived from Command
	// and Args via DeriveTestName. Must match ^[a-zA-Z0-9][a-zA-Z0-9_-]*$.
	TestName string `json:"testName,omitempty" yaml:"testName,omitempty" doc:"Test name override" maxLength:"100"`

	// SnapshotDir is the directory where testdata/<name>.json is written.
	// Defaults to "testdata" relative to the working directory.
	SnapshotDir string `json:"snapshotDir,omitempty" yaml:"snapshotDir,omitempty" doc:"Directory for snapshot files" maxLength:"500"`

	// Data is the command output as a parsed Go value used for assertion derivation.
	// Typically produced by json.Unmarshal on the command's JSON output.
	// When nil, no CEL assertions are derived.
	Data any `json:"-" yaml:"-"`

	// RawJSON is the raw JSON bytes written as the snapshot golden file.
	// When empty, no snapshot file is written and the test case omits a snapshot field.
	RawJSON []byte `json:"-" yaml:"-"`
}

// GenerateResult holds the generated test case and snapshot metadata.
type GenerateResult struct {
	// TestName is the (possibly derived) name of the test.
	TestName string `json:"testName" yaml:"testName"`

	// TestCase is the generated test case, ready to paste into spec.testing.cases.
	TestCase *TestCase `json:"testCase" yaml:"testCase"`

	// SnapshotPath is the relative (or absolute) path where the snapshot file was
	// written. Empty when no snapshot was written.
	SnapshotPath string `json:"snapshotPath,omitempty" yaml:"snapshotPath,omitempty"`

	// SnapshotWritten is true when the snapshot file was created or updated on disk.
	SnapshotWritten bool `json:"snapshotWritten" yaml:"snapshotWritten"`
}

// Generate produces a TestCase definition from command output.
//
// It:
//  1. Derives a test name from Command + Args (unless TestName is set).
//  2. Walks Data up to depth 2 and generates CEL assertions.
//  3. Writes a normalized snapshot golden file to SnapshotDir/<name>.json.
//  4. Returns a GenerateResult with the test case and snapshot metadata.
func Generate(input *GenerateInput) (*GenerateResult, error) {
	if len(input.Command) == 0 {
		return nil, fmt.Errorf("generate: command is required")
	}

	// Derive or validate test name.
	testName := input.TestName
	if testName == "" {
		testName = DeriveTestName(input.Command, input.Args)
	}

	// Derive CEL assertions from the parsed output value (only when data is present).
	var assertions []Assertion
	if input.Data != nil {
		assertions = deriveAssertions(input.Data, "__output", 0)
		if len(assertions) > MaxGeneratedAssertions {
			assertions = assertions[:MaxGeneratedAssertions]
		}
	}

	// Resolve snapshot directory and file path.
	snapshotDir := input.SnapshotDir
	if snapshotDir == "" {
		snapshotDir = "testdata"
	}
	snapshotFile := filepath.Join(snapshotDir, testName+".json")

	// Write the snapshot golden file when raw JSON is provided.
	var snapshotWritten bool
	if len(input.RawJSON) > 0 {
		if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
			return nil, fmt.Errorf("generate: creating snapshot directory %q: %w", snapshotDir, err)
		}
		normalized := Normalize(string(input.RawJSON), "")
		if err := os.WriteFile(snapshotFile, []byte(normalized), 0o600); err != nil {
			return nil, fmt.Errorf("generate: writing snapshot %q: %w", snapshotFile, err)
		}
		snapshotWritten = true
	}

	// Build the description from command + args (human-readable summary).
	descArgs := append(append([]string{}, input.Command...), input.Args...)
	desc := fmt.Sprintf("Auto-generated test for: %s", strings.Join(descArgs, " "))

	// Ensure the generated test args include -o json so __output is populated.
	testArgs := ensureOutputJSON(input.Args)

	tc := &TestCase{
		Description: desc,
		Command:     input.Command,
		Args:        testArgs,
		Assertions:  assertions,
		Tags:        []string{"generated"},
	}
	if snapshotWritten {
		tc.Snapshot = snapshotFile
	}

	return &GenerateResult{
		TestName:        testName,
		TestCase:        tc,
		SnapshotPath:    snapshotFile,
		SnapshotWritten: snapshotWritten,
	}, nil
}

// DeriveTestName produces a kebab-case test name from a command path and its args.
//
// The algorithm:
//  1. Starts with the command words (e.g. "render", "solution").
//  2. For each flag that takes a value (-r env=prod), splits "key=value" pairs and
//     appends both parts; plain values are appended as-is.
//  3. Positional args are appended directly.
//  4. The result is lowercased, non-alphanumeric chars replaced with dashes,
//     consecutive dashes collapsed, and leading/trailing dashes stripped.
//
// Examples:
//
//	DeriveTestName(["render","solution"], ["-r","env=prod"])  → "render-solution-env-prod"
//	DeriveTestName(["run","resolver"], ["db"])                → "run-resolver-db"
func DeriveTestName(command, args []string) string {
	parts := make([]string, 0, len(command)+4)
	parts = append(parts, command...)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			// Positional arg — include directly.
			parts = append(parts, arg)
			continue
		}

		// Flag: check whether the next token is its value.
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			val := args[i+1]
			i++ // consume the value token
			// For key=value pairs (e.g. "env=prod"), include both sides.
			if idx := strings.IndexByte(val, '='); idx >= 0 {
				parts = append(parts, val[:idx], val[idx+1:])
			} else {
				parts = append(parts, val)
			}
		}
		// Boolean flags (no value) contribute nothing to the slug.
	}

	// Slugify.
	slug := strings.ToLower(strings.Join(parts, "-"))
	slug = generateNonAlphanumRe.ReplaceAllString(slug, "-")
	slug = generateMultiDashRe.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")

	if slug == "" {
		return "generated-test"
	}
	return slug
}

// GenerateToYAML marshals a GenerateResult to YAML ready for pasting into the
// spec.testing.cases section of a solution file. The outer key is the test name.
func GenerateToYAML(result *GenerateResult) ([]byte, error) {
	wrapper := map[string]*TestCase{
		result.TestName: result.TestCase,
	}
	data, err := yaml.Marshal(wrapper)
	if err != nil {
		return nil, fmt.Errorf("generate: marshaling test YAML: %w", err)
	}
	return data, nil
}

var (
	generateNonAlphanumRe = regexp.MustCompile(`[^a-z0-9-]+`)
	generateMultiDashRe   = regexp.MustCompile(`-{2,}`)
)

// ensureOutputJSON returns a copy of args with "-o", "json" appended when no
// -o / --output flag is already present.
func ensureOutputJSON(args []string) []string {
	for _, arg := range args {
		if arg == "-o" || arg == "--output" ||
			strings.HasPrefix(arg, "-o=") || strings.HasPrefix(arg, "--output=") {
			return args
		}
	}
	result := make([]string, len(args), len(args)+2)
	copy(result, args)
	return append(result, "-o", "json")
}

// deriveAssertions walks data up to maxGenerateDepth, emitting one CEL assertion
// per node. Each assertion uses the __output path prefix so it is ready for the
// test runner's CEL evaluation context.
func deriveAssertions(data any, path string, depth int) []Assertion {
	if depth > maxGenerateDepth {
		return nil
	}

	var out []Assertion

	switch v := data.(type) {
	case map[string]any:
		// Size assertion for the map.
		out = append(out, Assertion{
			Expression: celexp.Expression(fmt.Sprintf("size(%s) == %d", path, len(v))),
			Message:    fmt.Sprintf("%s should have %d keys", path, len(v)),
		})
		// Recurse into children (sorted for determinism).
		for _, key := range sortedStringKeys(v) {
			childPath := fmt.Sprintf(`%s["%s"]`, path, key)
			out = append(out, deriveAssertions(v[key], childPath, depth+1)...)
		}

	case []any:
		out = append(out, Assertion{
			Expression: celexp.Expression(fmt.Sprintf("size(%s) == %d", path, len(v))),
			Message:    fmt.Sprintf("%s should have %d elements", path, len(v)),
		})

	case string:
		out = append(out, Assertion{
			Expression: celexp.Expression(fmt.Sprintf(`%s == "%s"`, path, generateJSONEscape(v))),
		})

	case float64:
		out = append(out, Assertion{
			Expression: celexp.Expression(fmt.Sprintf(`%s == %s`, path, generateFormatNumber(v))),
		})

	case bool:
		out = append(out, Assertion{
			Expression: celexp.Expression(fmt.Sprintf(`%s == %v`, path, v)),
		})

	case nil:
		out = append(out, Assertion{
			Expression: celexp.Expression(fmt.Sprintf(`%s == null`, path)),
		})
	}

	return out
}

// sortedStringKeys returns the keys of m in alphabetical order.
func sortedStringKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// generateJSONEscape escapes a string value for embedding in a CEL string literal.
// It uses json.Marshal to handle all necessary escape sequences, then strips the
// surrounding quotes that json.Marshal adds.
func generateJSONEscape(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		// Fallback: manual escaping.
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		return s
	}
	// b is `"escaped-value"` — strip the outer double quotes.
	return string(b[1 : len(b)-1])
}

// generateFormatNumber formats a float64 as an integer when it has no fractional
// part (JSON numbers without decimals unmarshal as float64 in Go).
func generateFormatNumber(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}
