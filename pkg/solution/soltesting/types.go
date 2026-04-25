// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package soltesting provides types and utilities for functional testing of solutions.
// It defines the test case specification, assertion types, and result structures
// used by the `scafctl test functional` and `scafctl test list` commands.
//
// Test definitions live under `spec.testing` in solution YAML and support
// compose-based splitting into separate files.
package soltesting

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/duration"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting/mockexec"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting/mockserver"
	"gopkg.in/yaml.v3"
)

// Max limits enforced by Validate().
const (
	// MaxAssertionsPerTest is the maximum number of assertions allowed per test case.
	MaxAssertionsPerTest = 100
	// MaxFilesPerTest is the maximum number of file entries allowed per test case.
	MaxFilesPerTest = 50
	// MaxTagsPerTest is the maximum number of tags allowed per test case.
	MaxTagsPerTest = 20
	// MaxExtendsDepth is the maximum depth of extends inheritance chains.
	MaxExtendsDepth = 10
	// MaxTestsPerSolution is the maximum number of tests per solution.
	MaxTestsPerSolution = 500
	// MaxRetries is the maximum number of retry attempts for a failing test.
	MaxRetries = 10
)

// Test name validation patterns.
var (
	testNameRegex     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
	templateNameRegex = regexp.MustCompile(`^_[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
)

// TestNamePattern returns the compiled regex for valid test names.
func TestNamePattern() *regexp.Regexp {
	return testNameRegex
}

// TemplateNamePattern returns the compiled regex for valid template names.
func TemplateNamePattern() *regexp.Regexp {
	return templateNameRegex
}

// Valid target values for assertions.
var validTargets = map[string]bool{
	"":         true,
	"stdout":   true,
	"stderr":   true,
	"combined": true,
}

// Status represents the outcome of a test.
type Status string

const (
	// StatusPass indicates the test passed.
	StatusPass Status = "pass"
	// StatusFail indicates the test failed (assertion failure).
	StatusFail Status = "fail"
	// StatusSkip indicates the test was skipped.
	StatusSkip Status = "skip"
	// StatusError indicates the test encountered an infrastructure/setup error.
	StatusError Status = "error"
)

// SkipBuiltinsValue supports both bool and []string via custom UnmarshalYAML.
// When bool: true skips all builtins, false skips none.
// When []string: skips only the named builtins (without "builtin:" prefix).
// Both UnmarshalYAML and MarshalYAML are required to survive
// the deepCopySolution YAML round-trip used in compose.
type SkipBuiltinsValue struct {
	// All when true means skip all builtins.
	All bool `json:"-" yaml:"-"`
	// Names lists specific builtin names to skip.
	Names []string `json:"-" yaml:"-"`
}

// UnmarshalYAML implements yaml.Unmarshaler for SkipBuiltinsValue.
func (s *SkipBuiltinsValue) UnmarshalYAML(value *yaml.Node) error {
	// Try bool first
	var b bool
	if err := value.Decode(&b); err == nil {
		s.All = b
		s.Names = nil
		return nil
	}

	// Try []string
	var names []string
	if err := value.Decode(&names); err == nil {
		s.All = false
		s.Names = names
		return nil
	}

	return fmt.Errorf("skipBuiltins must be a bool or list of strings")
}

// MarshalYAML implements yaml.Marshaler for SkipBuiltinsValue.
func (s SkipBuiltinsValue) MarshalYAML() (any, error) {
	if s.All {
		return true, nil
	}
	if len(s.Names) > 0 {
		return s.Names, nil
	}
	return false, nil
}

// UnmarshalJSON implements json.Unmarshaler for SkipBuiltinsValue.
func (s *SkipBuiltinsValue) UnmarshalJSON(data []byte) error {
	// Try bool first
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		s.All = b
		s.Names = nil
		return nil
	}

	// Try []string
	var names []string
	if err := json.Unmarshal(data, &names); err == nil {
		s.All = false
		s.Names = names
		return nil
	}

	return fmt.Errorf("skipBuiltins must be a bool or list of strings")
}

// MarshalJSON implements json.Marshaler for SkipBuiltinsValue.
func (s SkipBuiltinsValue) MarshalJSON() ([]byte, error) {
	if s.All {
		return json.Marshal(true)
	}
	if len(s.Names) > 0 {
		return json.Marshal(s.Names)
	}
	return json.Marshal(false)
}

// IsSkipped returns true if the SkipBuiltinsValue indicates all builtins are skipped
// or if the value has specific builtin names listed.
func (s SkipBuiltinsValue) IsSkipped() bool {
	return s.All || len(s.Names) > 0
}

// SkipValue supports both bool and CEL expression string via custom UnmarshalYAML.
// When bool: true skips unconditionally, false means no skip.
// When string: the string is evaluated as a CEL expression at discovery time.
// YAML usage:
//
//	skip: true              # unconditional skip
//	skip: 'os == "windows"'  # conditional skip via CEL
type SkipValue struct {
	// Static when true means unconditionally skip.
	Static bool `json:"-" yaml:"-"`
	// Expression is a CEL expression for conditional skipping.
	Expression celexp.Expression `json:"-" yaml:"-"`
}

// UnmarshalYAML implements yaml.Unmarshaler for SkipValue.
func (s *SkipValue) UnmarshalYAML(value *yaml.Node) error {
	// Try bool first
	var b bool
	if err := value.Decode(&b); err == nil {
		s.Static = b
		s.Expression = ""
		return nil
	}

	// Try string (CEL expression)
	var expr string
	if err := value.Decode(&expr); err == nil {
		s.Static = false
		s.Expression = celexp.Expression(expr)
		return nil
	}

	return fmt.Errorf("skip must be a bool or a CEL expression string")
}

// MarshalYAML implements yaml.Marshaler for SkipValue.
func (s SkipValue) MarshalYAML() (any, error) {
	if s.Expression != "" {
		return string(s.Expression), nil
	}
	if s.Static {
		return true, nil
	}
	return false, nil
}

// UnmarshalJSON implements json.Unmarshaler for SkipValue.
func (s *SkipValue) UnmarshalJSON(data []byte) error {
	// Try bool first
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		s.Static = b
		s.Expression = ""
		return nil
	}

	// Try string (CEL expression)
	var expr string
	if err := json.Unmarshal(data, &expr); err == nil {
		s.Static = false
		s.Expression = celexp.Expression(expr)
		return nil
	}

	return fmt.Errorf("skip must be a bool or a CEL expression string")
}

// MarshalJSON implements json.Marshaler for SkipValue.
func (s SkipValue) MarshalJSON() ([]byte, error) {
	if s.Expression != "" {
		return json.Marshal(string(s.Expression))
	}
	if s.Static {
		return json.Marshal(true)
	}
	return json.Marshal(false)
}

// IsZero returns true if the SkipValue is the zero value (no skip configured).
func (s SkipValue) IsZero() bool {
	return !s.Static && s.Expression == ""
}

// ShouldSkip returns true if the SkipValue indicates unconditional skip.
// For expression-based skip, use the runner's evaluation logic.
func (s SkipValue) ShouldSkip() bool {
	return s.Static
}

// HasExpression returns true if the SkipValue has a CEL expression.
func (s SkipValue) HasExpression() bool {
	return s.Expression != ""
}

// String returns a human-readable string representation.
func (s SkipValue) String() string {
	if s.Expression != "" {
		return string(s.Expression)
	}
	if s.Static {
		return "true"
	}
	return "false"
}

// TestSuite groups all test-related configuration under a single top-level property.
type TestSuite struct {
	// Config holds solution-level test configuration.
	Config *TestConfig `json:"config,omitempty" yaml:"config,omitempty" doc:"Test configuration"`

	// Cases is a map of functional test definitions keyed by test name.
	// Test names must be unique and must match ^[a-zA-Z0-9][a-zA-Z0-9_-]*$.
	// Names starting with _ are templates that are not executed directly.
	Cases map[string]*TestCase `json:"cases,omitempty" yaml:"cases,omitempty" doc:"Test case definitions keyed by name"`
}

// HasCases returns true if the suite contains any test case definitions.
func (ts *TestSuite) HasCases() bool {
	return ts != nil && len(ts.Cases) > 0
}

// HasConfig returns true if the suite has test configuration.
func (ts *TestSuite) HasConfig() bool {
	return ts != nil && ts.Config != nil
}

// TestConfig holds solution-level test configuration.
type TestConfig struct {
	// SkipBuiltins disables builtin tests. true for all, or list of specific names.
	SkipBuiltins SkipBuiltinsValue `json:"skipBuiltins,omitempty" yaml:"skipBuiltins,omitempty" doc:"Disable builtins: true for all, or list of specific names"`

	// Env provides suite-level environment variables applied to all tests.
	Env map[string]string `json:"env,omitempty" yaml:"env,omitempty" doc:"Suite-level environment variables applied to all tests"`

	// Setup contains suite-level setup steps run once, then the resulting sandbox is copied per-test.
	Setup []InitStep `json:"setup,omitempty" yaml:"setup,omitempty" doc:"Suite-level setup steps. Run once, then sandbox copied per-test" maxItems:"50"`

	// Cleanup contains suite-level teardown steps run once after all tests complete, even on failure.
	Cleanup []InitStep `json:"cleanup,omitempty" yaml:"cleanup,omitempty" doc:"Suite-level teardown steps. Run once after all tests complete, even on failure" maxItems:"50"`

	// Files lists shared file paths, globs, or directories copied into every test sandbox.
	// These are merged with per-test files; per-test entries are appended after config files.
	// Duplicates are deduplicated during sandbox creation (first-seen-wins).
	Files []string `json:"files,omitempty" yaml:"files,omitempty" doc:"Shared files copied into every test sandbox" maxItems:"50"`

	// Services defines background services started before tests and stopped after.
	// Currently supports "http" type which starts a mock HTTP server.
	Services []ServiceConfig `json:"services,omitempty" yaml:"services,omitempty" doc:"Background services started before tests" maxItems:"10"`
}

// ServiceConfig defines a background service for test infrastructure.
type ServiceConfig struct {
	// Name is a unique identifier for the service within this solution's tests.
	Name string `json:"name" yaml:"name" doc:"Unique service identifier" maxLength:"100" pattern:"^[a-zA-Z0-9][a-zA-Z0-9_-]*$"`

	// Type is the service type. Supported: "http" (mock HTTP server), "exec" (mock shell commands).
	Type string `json:"type" yaml:"type" doc:"Service type" pattern:"^(http|exec)$" patternDescription:"Must be: http or exec"`

	// PortEnv is the environment variable name where the assigned port is exposed.
	// Tests can reference this via testConfig.env or in resolver inputs (e.g., $MOCK_HTTP_PORT).
	// Only used when Type is "http".
	PortEnv string `json:"portEnv,omitempty" yaml:"portEnv,omitempty" doc:"Env var name for assigned port (http only)" maxLength:"100" pattern:"^[A-Z][A-Z0-9_]*$"`

	// BaseURLEnv is the environment variable name where the base URL is exposed (e.g., http://127.0.0.1:PORT).
	// Optional — if empty, only PortEnv is set. Only used when Type is "http".
	BaseURLEnv string `json:"baseUrlEnv,omitempty" yaml:"baseUrlEnv,omitempty" doc:"Env var name for base URL (http only)" maxLength:"100"`

	// Routes defines the mock HTTP routes (only used when Type is "http").
	Routes []mockserver.Route `json:"routes,omitempty" yaml:"routes,omitempty" doc:"Mock HTTP routes" maxItems:"100"`

	// ExecRules defines mock command rules (only used when Type is "exec").
	// Rules are matched in order — the first matching rule wins.
	ExecRules []mockexec.Rule `json:"execRules,omitempty" yaml:"execRules,omitempty" doc:"Mock command rules" maxItems:"100"`

	// Passthrough allows unmatched exec commands to run normally (only used when Type is "exec").
	// When false (default), unmatched commands fail with an error.
	Passthrough bool `json:"passthrough,omitempty" yaml:"passthrough,omitempty" doc:"Allow unmatched exec commands to run (exec only)"`
}

// InitStep is a setup/cleanup command.
// Uses the same input schema as the exec provider.
type InitStep struct {
	// Command is the command to execute. Supports POSIX shell syntax (pipes, redirections, variables).
	Command string `json:"command" yaml:"command" doc:"Command to execute" maxLength:"1000"`

	// Args contains additional arguments, automatically shell-quoted.
	Args []string `json:"args,omitempty" yaml:"args,omitempty" doc:"Additional arguments" maxItems:"50"`

	// Stdin provides standard input to the command.
	Stdin string `json:"stdin,omitempty" yaml:"stdin,omitempty" doc:"Standard input to provide to the command" maxLength:"10000"`

	// WorkingDir is the working directory relative to sandbox root.
	WorkingDir string `json:"workingDir,omitempty" yaml:"workingDir,omitempty" doc:"Working directory (relative to sandbox root)" maxLength:"500"`

	// Env contains environment variables merged with the parent process.
	Env map[string]string `json:"env,omitempty" yaml:"env,omitempty" doc:"Environment variables merged with the parent process"`

	// Timeout is the timeout in seconds (default: 30).
	Timeout int `json:"timeout,omitempty" yaml:"timeout,omitempty" doc:"Timeout in seconds" maximum:"3600"`

	// Shell specifies the shell interpreter: auto (default), sh, bash, pwsh, cmd.
	Shell string `json:"shell,omitempty" yaml:"shell,omitempty" doc:"Shell interpreter" pattern:"^(auto|sh|bash|pwsh|cmd)$" patternDescription:"Must be one of: auto, sh, bash, pwsh, cmd"`
}

// TestCase defines a single functional test for a solution.
type TestCase struct {
	// Name is the test name (auto-set from map key).
	Name string `json:"name" yaml:"name" doc:"Test name (auto-set from map key)" maxLength:"100" pattern:"^[a-zA-Z0-9_][a-zA-Z0-9_-]*$" patternDescription:"Must start with a letter, digit, or underscore and contain only letters, digits, hyphens, and underscores"`

	// Description is a human-readable test description.
	Description string `json:"description" yaml:"description" doc:"Human-readable test description" maxLength:"500"`

	// Command is the scafctl subcommand as an array (e.g., [render, solution]).
	Command []string `json:"command,omitempty" yaml:"command,omitempty" doc:"scafctl subcommand as an array" maxItems:"10"`

	// Args contains additional CLI flags appended after the command.
	Args []string `json:"args,omitempty" yaml:"args,omitempty" doc:"Additional CLI flags appended after the command" maxItems:"50"`

	// Extends lists names of test templates to inherit from. Applied left-to-right.
	Extends []string `json:"extends,omitempty" yaml:"extends,omitempty" doc:"Names of test templates to inherit from" maxItems:"10"`

	// Tags are labels for categorization and filtering.
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty" doc:"Tags for categorization and filtering" maxItems:"20"`

	// Env contains per-test environment variables.
	Env map[string]string `json:"env,omitempty" yaml:"env,omitempty" doc:"Per-test environment variables"`

	// Files lists relative paths, glob patterns, or directory paths for files required by this test.
	// Globs (e.g., 'templates/**/*.yaml') are expanded using doublestar matching.
	// Directories (e.g., 'templates/') are recursively expanded to include all files.
	Files []string `json:"files,omitempty" yaml:"files,omitempty" doc:"Relative paths, glob patterns (e.g., 'templates/**'), or directory paths for files required by this test. Directories are recursively copied." maxItems:"50"`

	// Init contains setup steps executed sequentially before the command.
	Init []InitStep `json:"init,omitempty" yaml:"init,omitempty" doc:"Setup steps executed sequentially before the command" maxItems:"50"`

	// Cleanup contains teardown steps executed after the command, even on failure.
	Cleanup []InitStep `json:"cleanup,omitempty" yaml:"cleanup,omitempty" doc:"Teardown steps executed after the command, even on failure" maxItems:"50"`

	// Assertions validates command output. Required unless snapshot is set.
	Assertions []Assertion `json:"assertions,omitempty" yaml:"assertions,omitempty" doc:"Output assertions" maxItems:"100"`

	// Snapshot is a relative path to a golden file for normalized comparison.
	Snapshot string `json:"snapshot,omitempty" yaml:"snapshot,omitempty" doc:"Relative path to a golden file for normalized comparison" maxLength:"500"`

	// InjectFile controls whether the runner auto-injects -f <sandbox-solution-path>.
	// Default is true. Set to false for commands that don't accept -f.
	InjectFile *bool `json:"injectFile,omitempty" yaml:"injectFile,omitempty" doc:"When true, auto-inject -f <sandbox-solution-path>"`

	// ExpectFailure when true, the test passes if the command exits non-zero.
	ExpectFailure bool `json:"expectFailure,omitempty" yaml:"expectFailure,omitempty" doc:"When true, test passes if command exits non-zero"`

	// ExitCode is the exact expected exit code. Mutually exclusive with ExpectFailure.
	ExitCode *int `json:"exitCode,omitempty" yaml:"exitCode,omitempty" doc:"Exact expected exit code"`

	// Timeout is the per-test timeout as a Go duration string.
	Timeout *duration.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty" doc:"Per-test timeout as a Go duration string"`

	// Skip controls test skipping. Accepts true (unconditional) or a CEL expression string (conditional).
	Skip SkipValue `json:"skip,omitempty" yaml:"skip,omitempty" doc:"Skip this test: true for unconditional, or a CEL expression string for conditional skip"`

	// SkipReason is a human-readable reason for skipping.
	SkipReason string `json:"skipReason,omitempty" yaml:"skipReason,omitempty" doc:"Human-readable reason for skipping" maxLength:"500"`

	// Retries is the number of retry attempts for a failing test.
	Retries int `json:"retries,omitempty" yaml:"retries,omitempty" doc:"Number of retry attempts for failing tests" maximum:"10"`
}

// IsTemplate returns true if this test is a template (name starts with _).
// Template tests are not executed directly but can be inherited via extends.
func (tc *TestCase) IsTemplate() bool {
	return strings.HasPrefix(tc.Name, "_")
}

// GetInjectFile returns the value of InjectFile, defaulting to true if not set.
func (tc *TestCase) GetInjectFile() bool {
	if tc.InjectFile == nil {
		return true
	}
	return *tc.InjectFile
}

// Validate performs comprehensive validation of a TestCase.
func (tc *TestCase) Validate() error {
	var errs []string

	// Name validation
	switch {
	case tc.Name == "":
		errs = append(errs, "test name is required")
	case tc.IsTemplate():
		if !templateNameRegex.MatchString(tc.Name) {
			errs = append(errs, fmt.Sprintf("template name %q must match %s", tc.Name, templateNameRegex.String()))
		}
	default:
		if !testNameRegex.MatchString(tc.Name) {
			errs = append(errs, fmt.Sprintf("test name %q must match %s", tc.Name, testNameRegex.String()))
		}
	}

	// Mutual exclusion: exitCode and expectFailure
	if tc.ExitCode != nil && tc.ExpectFailure {
		errs = append(errs, "exitCode and expectFailure are mutually exclusive")
	}

	// Non-template tests must have command and at least one of assertions or snapshot
	// (after extends resolution, so we only enforce if extends is empty)
	if !tc.IsTemplate() && len(tc.Extends) == 0 {
		if len(tc.Command) == 0 {
			errs = append(errs, "command is required for non-template tests without extends")
		}
		if len(tc.Assertions) == 0 && tc.Snapshot == "" {
			errs = append(errs, "at least one of assertions or snapshot is required for non-template tests")
		}
	}

	// Args must not contain -f or --file
	for _, arg := range tc.Args {
		if arg == "-f" || arg == "--file" {
			errs = append(errs, "args must not contain -f or --file; use injectFile to control file injection")
			break
		}
	}

	// Retries validation
	if tc.Retries < 0 || tc.Retries > MaxRetries {
		errs = append(errs, fmt.Sprintf("retries must be between 0 and %d", MaxRetries))
	}

	// Field count limits
	if len(tc.Assertions) > MaxAssertionsPerTest {
		errs = append(errs, fmt.Sprintf("assertions count %d exceeds maximum of %d", len(tc.Assertions), MaxAssertionsPerTest))
	}
	if len(tc.Files) > MaxFilesPerTest {
		errs = append(errs, fmt.Sprintf("files count %d exceeds maximum of %d", len(tc.Files), MaxFilesPerTest))
	}
	if len(tc.Tags) > MaxTagsPerTest {
		errs = append(errs, fmt.Sprintf("tags count %d exceeds maximum of %d", len(tc.Tags), MaxTagsPerTest))
	}

	// Timeout must be positive if set
	if tc.Timeout != nil && tc.Timeout.Duration <= 0 {
		errs = append(errs, "timeout must be positive")
	}

	// Validate each assertion
	for i, a := range tc.Assertions {
		if err := a.Validate(); err != nil {
			errs = append(errs, fmt.Sprintf("assertion[%d]: %s", i, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("test %q validation failed:\n  - %s", tc.Name, strings.Join(errs, "\n  - "))
	}
	return nil
}

// Assertion validates command output.
// Exactly one of Expression, Regex, Contains, NotRegex, or NotContains must be set.
type Assertion struct {
	// Expression is a CEL expression evaluating to bool.
	Expression celexp.Expression `json:"expression,omitempty" yaml:"expression,omitempty" doc:"CEL expression evaluating to bool"`

	// Regex is a regex pattern that must match somewhere in the target text.
	Regex string `json:"regex,omitempty" yaml:"regex,omitempty" doc:"Regex pattern that must match" maxLength:"1000"`

	// Contains is a substring that must appear in the target text.
	Contains string `json:"contains,omitempty" yaml:"contains,omitempty" doc:"Substring that must appear" maxLength:"5000"`

	// NotRegex is a regex pattern that must NOT match anywhere in the target text.
	NotRegex string `json:"notRegex,omitempty" yaml:"notRegex,omitempty" doc:"Regex pattern that must NOT match" maxLength:"1000"`

	// NotContains is a substring that must NOT appear in the target text.
	NotContains string `json:"notContains,omitempty" yaml:"notContains,omitempty" doc:"Substring that must NOT appear" maxLength:"5000"`

	// Target specifies which output stream to match against: stdout (default), stderr, or combined.
	// Only applies to regex, contains, notRegex, notContains.
	// CEL expressions access both via context variables.
	Target string `json:"target,omitempty" yaml:"target,omitempty" doc:"Output stream to match: stdout, stderr, or combined" pattern:"^(stdout|stderr|combined)?$" patternDescription:"Must be one of: stdout, stderr, combined (or empty for stdout)"`

	// Message is a custom failure message (optional).
	Message string `json:"message,omitempty" yaml:"message,omitempty" doc:"Custom failure message" maxLength:"1000"`
}

// Validate checks that exactly one assertion type is set and target is valid.
func (a *Assertion) Validate() error {
	count := 0
	if a.Expression != "" {
		count++
	}
	if a.Regex != "" {
		count++
	}
	if a.Contains != "" {
		count++
	}
	if a.NotRegex != "" {
		count++
	}
	if a.NotContains != "" {
		count++
	}

	if count == 0 {
		return fmt.Errorf("exactly one of expression, regex, contains, notRegex, or notContains must be set")
	}
	if count > 1 {
		return fmt.Errorf("exactly one of expression, regex, contains, notRegex, or notContains must be set, got %d", count)
	}

	if !validTargets[a.Target] {
		return fmt.Errorf("target must be one of: stdout, stderr, combined (or empty); got %q", a.Target)
	}

	// Validate regex patterns compile
	if a.Regex != "" {
		if _, err := regexp.Compile(a.Regex); err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
	}
	if a.NotRegex != "" {
		if _, err := regexp.Compile(a.NotRegex); err != nil {
			return fmt.Errorf("invalid notRegex pattern: %w", err)
		}
	}

	return nil
}

// FileInfo represents a file created or modified in the sandbox.
type FileInfo struct {
	// Exists is always true for entries in the map (present for consistency).
	Exists bool `json:"exists"`
	// Content is the full file content as a string.
	Content string `json:"content"`
}

// CommandOutput is the assertion context passed to CEL expressions.
type CommandOutput struct {
	// Stdout is the raw stdout text.
	Stdout string `json:"stdout"`
	// Stderr is the raw stderr text.
	Stderr string `json:"stderr"`
	// ExitCode is the process exit code.
	ExitCode int `json:"exitCode"`
	// Output is the parsed JSON output. nil when the command doesn't support -o json.
	Output map[string]any `json:"output,omitempty"`
	// Files contains files created or modified in the sandbox during command execution.
	Files map[string]FileInfo `json:"files"`
}

// TestResult captures the outcome of a single test.
type TestResult struct {
	// Solution is the solution name.
	Solution string `json:"solution"`
	// Test is the test name.
	Test string `json:"test"`
	// Status is the test outcome.
	Status Status `json:"status"`
	// Duration is how long the test took.
	Duration time.Duration `json:"duration"`
	// Message provides details about skip, error, or failure.
	Message string `json:"message,omitempty"`
	// Assertions contains the results of each assertion evaluation.
	Assertions []AssertionResult `json:"assertions,omitempty"`
	// RetryAttempt indicates which retry attempt passed (0 = first attempt).
	RetryAttempt int `json:"retryAttempt,omitempty"`
	// SandboxPath is the sandbox directory path (when --keep-sandbox is set).
	SandboxPath string `json:"sandboxPath,omitempty"`
	// Stdout is the captured stdout from the test command (included on failure).
	Stdout string `json:"stdout,omitempty"`
	// Stderr is the captured stderr from the test command (included on failure).
	Stderr string `json:"stderr,omitempty"`
}

// AssertionResult captures the outcome of a single assertion.
type AssertionResult struct {
	// Type is the assertion type (expression, regex, contains, notRegex, notContains).
	Type string `json:"type"`
	// Input is the assertion input (expression string, regex pattern, or substring).
	Input string `json:"input"`
	// Passed indicates whether the assertion passed.
	Passed bool `json:"passed"`
	// Message is the failure diagnostic message.
	Message string `json:"message,omitempty"`
	// Actual is the actual value encountered (for diagnostics).
	Actual any `json:"actual,omitempty"`
}
