// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package settings

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewCliParams(t *testing.T) {
	tests := []struct {
		name string
		want *Run
	}{
		{
			name: "default CLI params",
			want: &Run{
				MinLogLevel: "none",
				EntryPointSettings: EntryPointSettings{
					FromAPI: false,
					FromCli: true,
					Path:    "",
				},
				IsQuiet:     false,
				NoColor:     false,
				ExitOnError: true,
				BinaryName:  CliBinaryName,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewCliParams()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultHTTPCacheDir(t *testing.T) {
	dir := DefaultHTTPCacheDir()
	assert.Contains(t, dir, "scafctl")
	assert.Contains(t, dir, "http-cache")
}

func TestDefaultBuildCacheDir(t *testing.T) {
	dir := DefaultBuildCacheDir()
	assert.Contains(t, dir, "scafctl")
	assert.Contains(t, dir, "build-cache")
}

func TestDefaultPluginCacheDir(t *testing.T) {
	dir := DefaultPluginCacheDir()
	assert.Contains(t, dir, "scafctl")
	assert.Contains(t, dir, "plugins")
}

func TestSolutionFoldersFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		binaryName string
		want       []string
	}{
		{
			name:       "default binary name",
			binaryName: "scafctl",
			want:       []string{"scafctl", ".scafctl", ""},
		},
		{
			name:       "custom binary name",
			binaryName: "mycli",
			want:       []string{"mycli", ".mycli", ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SolutionFoldersFor(tt.binaryName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSolutionFileNamesFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		binaryName  string
		wantLen     int
		mustContain []string
	}{
		{
			name:        "default binary name",
			binaryName:  "scafctl",
			wantLen:     8,
			mustContain: []string{"solution.yaml", "scafctl.yaml", "scafctl.json", "actions.yaml", "actions.yml"},
		},
		{
			name:        "custom binary name",
			binaryName:  "mycli",
			wantLen:     8,
			mustContain: []string{"solution.yaml", "mycli.yaml", "mycli.json", "actions.yaml", "actions.yml"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SolutionFileNamesFor(tt.binaryName)
			assert.Len(t, got, tt.wantLen)
			for _, want := range tt.mustContain {
				assert.Contains(t, got, want)
			}
		})
	}
}

func TestHTTPCacheKeyPrefixFor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "scafctl:", HTTPCacheKeyPrefixFor("scafctl"))
	assert.Equal(t, "mycli:", HTTPCacheKeyPrefixFor("mycli"))
}

func TestHTTPCacheDirFor(t *testing.T) {
	t.Parallel()
	dir := HTTPCacheDirFor("mycli")
	assert.Contains(t, dir, "mycli")
	assert.Contains(t, dir, "http-cache")
	// Verify the app directory segment is "mycli", not "scafctl".
	parts := strings.Split(dir, string(filepath.Separator))
	assert.NotContains(t, parts, "scafctl")
}

func TestBuildCacheDirFor(t *testing.T) {
	t.Parallel()
	dir := BuildCacheDirFor("mycli")
	assert.Contains(t, dir, "mycli")
	assert.Contains(t, dir, "build-cache")
	// Verify the app directory segment is "mycli", not "scafctl".
	parts := strings.Split(dir, string(filepath.Separator))
	assert.NotContains(t, parts, "scafctl")
}

func TestPluginCacheDirFor(t *testing.T) {
	t.Parallel()
	dir := PluginCacheDirFor("mycli")
	assert.Contains(t, dir, "mycli")
	assert.Contains(t, dir, "plugins")
	// Verify the app directory segment is "mycli", not "scafctl".
	parts := strings.Split(dir, string(filepath.Separator))
	assert.NotContains(t, parts, "scafctl")
}

func TestRootSolutionFolders_MatchesDefault(t *testing.T) {
	t.Parallel()
	assert.Equal(t, SolutionFoldersFor(CliBinaryName), RootSolutionFolders)
}

func TestSolutionFileNames_MatchesDefault(t *testing.T) {
	t.Parallel()
	assert.Equal(t, SolutionFileNamesFor(CliBinaryName), SolutionFileNames)
}

func TestSanitizeBinaryName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "simple name", raw: "mycli", want: "mycli"},
		{name: "with hyphen", raw: "my-cli", want: "my-cli"},
		{name: "with path", raw: "/usr/bin/mycli", want: "mycli"},
		{name: "with extension", raw: "mycli.exe", want: "mycli"},
		{name: "path and extension", raw: "/usr/local/bin/my-tool.exe", want: "my-tool"},
		{name: "spaces replaced", raw: "my cli", want: "my_cli"},
		{name: "empty string", raw: "", want: CliBinaryName},
		{name: "dot only", raw: ".", want: CliBinaryName},
		{name: "double dot", raw: "..", want: CliBinaryName},
		{name: "path separators only", raw: "///", want: CliBinaryName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, SanitizeBinaryName(tt.raw))
		})
	}
}

func TestSafeEnvPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		binaryName string
		want       string
	}{
		{name: "simple", binaryName: "scafctl", want: "SCAFCTL"},
		{name: "with hyphen", binaryName: "my-cli", want: "MY_CLI"},
		{name: "with dot", binaryName: "my.cli", want: "MY_CLI"},
		{name: "already upper", binaryName: "MYCLI", want: "MYCLI"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, SafeEnvPrefix(tt.binaryName))
		})
	}
}

func TestActionFileNamesFor(t *testing.T) {
	t.Parallel()
	names := ActionFileNamesFor("mycli")
	// Action files must come first.
	assert.Equal(t, "actions.yaml", names[0])
	assert.Equal(t, "actions.yml", names[1])
	// Solution files follow as fallback.
	assert.Contains(t, names, "solution.yaml")
	assert.Contains(t, names, "mycli.yaml")
}

func TestSolutionOnlyFileNamesFor(t *testing.T) {
	t.Parallel()
	names := SolutionOnlyFileNamesFor("mycli")
	for _, n := range names {
		assert.False(t, IsActionFile(n), "should not contain action files: %s", n)
	}
	assert.Contains(t, names, "solution.yaml")
	assert.Contains(t, names, "mycli.yaml")
}

func TestIsActionFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want bool
	}{
		{"actions.yaml", true},
		{"actions.yml", true},
		{"solution.yaml", false},
		{"actions.json", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsActionFile(tt.name))
		})
	}
}

func TestFileNamesForMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		mode              DiscoveryMode
		binaryName        string
		customActionFiles []string
		wantFirst         string
		wantContains      string
		wantNotContains   string
	}{
		{
			name:         "default mode returns all",
			mode:         DiscoveryModeDefault,
			binaryName:   "scafctl",
			wantFirst:    "solution.yaml",
			wantContains: "actions.yaml",
		},
		{
			name:         "action mode prefers actions",
			mode:         DiscoveryModeAction,
			binaryName:   "scafctl",
			wantFirst:    "actions.yaml",
			wantContains: "solution.yaml",
		},
		{
			name:            "solution mode excludes actions",
			mode:            DiscoveryModeSolution,
			binaryName:      "scafctl",
			wantFirst:       "solution.yaml",
			wantNotContains: "actions.yaml",
		},
		{
			name:              "action mode with custom files",
			mode:              DiscoveryModeAction,
			binaryName:        "mycli",
			customActionFiles: []string{"custom.yaml"},
			wantFirst:         "custom.yaml",
			wantContains:      "solution.yaml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := FileNamesForMode(tt.mode, tt.binaryName, tt.customActionFiles)
			assert.NotEmpty(t, result)
			assert.Equal(t, tt.wantFirst, result[0])
			if tt.wantContains != "" {
				assert.Contains(t, result, tt.wantContains)
			}
			if tt.wantNotContains != "" {
				assert.NotContains(t, result, tt.wantNotContains)
			}
		})
	}
}

func TestDiscoveryMode_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode DiscoveryMode
		want string
	}{
		{DiscoveryModeDefault, "default"},
		{DiscoveryModeAction, "action"},
		{DiscoveryModeSolution, "solution"},
		{DiscoveryMode(99), "unknown(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.mode.String())
		})
	}
}

func TestFileNamesForMode_CustomActionFiles_NoMutation(t *testing.T) {
	t.Parallel()
	custom := make([]string, 1, 10) // extra capacity to detect append mutation
	custom[0] = "custom.yaml"
	original := make([]string, len(custom))
	copy(original, custom)

	_ = FileNamesForMode(DiscoveryModeAction, "scafctl", custom)

	assert.Equal(t, original, custom, "FileNamesForMode must not mutate the input slice")
}
