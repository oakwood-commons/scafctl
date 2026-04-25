// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package metadataprovider

import (
	"context"
	"os"
	"runtime"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescriptor(t *testing.T) {
	p := New()
	d := p.Descriptor()
	assert.Equal(t, ProviderName, d.Name)
	assert.Equal(t, "Metadata Provider", d.DisplayName)
	assert.Equal(t, "v1", d.APIVersion)
	assert.NotNil(t, d.Schema)
	assert.Len(t, d.Capabilities, 1)
	assert.Equal(t, provider.CapabilityFrom, d.Capabilities[0])
	assert.Len(t, d.Examples, 1)
	assert.NotNil(t, d.OutputSchemas[provider.CapabilityFrom])
}

func TestExecute_FullContext(t *testing.T) {
	p := New()

	// Set up context with settings and solution metadata.
	ctx := context.Background()
	ctx = settings.IntoContext(ctx, &settings.Run{
		EntryPointSettings: settings.EntryPointSettings{
			FromCli: true,
			Path:    "scafctl/run/solution",
		},
	})
	ctx = provider.WithSolutionMetadata(ctx, &provider.SolutionMeta{
		Name:        "my-solution",
		Version:     "1.2.3",
		DisplayName: "My Solution",
		Description: "A test solution",
		Category:    "testing",
		Tags:        []string{"test", "example"},
	})

	out, err := p.Execute(ctx, nil)
	require.NoError(t, err)

	result, ok := out.Data.(map[string]any)
	require.True(t, ok, "expected map[string]any output")

	// Verify version info.
	versionMap, ok := result["version"].(map[string]any)
	require.True(t, ok, "version should be a map")
	assert.Equal(t, settings.VersionInformation.BuildVersion, versionMap["buildVersion"])
	assert.Equal(t, settings.VersionInformation.Commit, versionMap["commit"])
	assert.Equal(t, settings.VersionInformation.BuildTime, versionMap["buildTime"])

	// Verify args — should be os.Args.
	args, ok := result["args"].([]string)
	require.True(t, ok, "args should be []string")
	assert.Equal(t, os.Args, args)

	// Verify cwd.
	expectedCwd, _ := os.Getwd()
	assert.Equal(t, expectedCwd, result["cwd"])

	// Verify entrypoint.
	assert.Equal(t, "cli", result["entrypoint"])

	// Verify command.
	assert.Equal(t, "scafctl/run/solution", result["command"])

	// Verify platform info.
	assert.Equal(t, runtime.GOOS, result["os"])
	assert.Equal(t, runtime.GOARCH, result["arch"])

	// Verify solution metadata.
	solMap, ok := result["solution"].(map[string]any)
	require.True(t, ok, "solution should be a map")
	assert.Equal(t, "my-solution", solMap["name"])
	assert.Equal(t, "1.2.3", solMap["version"])
	assert.Equal(t, "My Solution", solMap["displayName"])
	assert.Equal(t, "A test solution", solMap["description"])
	assert.Equal(t, "testing", solMap["category"])
	assert.Equal(t, []string{"test", "example"}, solMap["tags"])
}

func TestExecute_APIEntrypoint(t *testing.T) {
	p := New()

	ctx := context.Background()
	ctx = settings.IntoContext(ctx, &settings.Run{
		EntryPointSettings: settings.EntryPointSettings{
			FromAPI: true,
			Path:    "api/v1/solutions/run",
		},
	})

	out, err := p.Execute(ctx, nil)
	require.NoError(t, err)

	result := out.Data.(map[string]any)
	assert.Equal(t, "api", result["entrypoint"])
	assert.Equal(t, "api/v1/solutions/run", result["command"])
}

func TestExecute_NoContext(t *testing.T) {
	p := New()

	// No settings or solution metadata in context — should still succeed with defaults.
	out, err := p.Execute(context.Background(), nil)
	require.NoError(t, err)

	result, ok := out.Data.(map[string]any)
	require.True(t, ok)

	// entrypoint should be "unknown" when no settings are in context.
	assert.Equal(t, "unknown", result["entrypoint"])
	assert.Equal(t, "", result["command"])

	// solution should be an empty map.
	solMap, ok := result["solution"].(map[string]any)
	require.True(t, ok)
	assert.Empty(t, solMap)

	// version, args, cwd should still be populated from process state.
	assert.NotNil(t, result["version"])
	assert.NotNil(t, result["args"])
	assert.NotEmpty(t, result["cwd"])

	// os and arch should always be populated.
	assert.Equal(t, runtime.GOOS, result["os"])
	assert.Equal(t, runtime.GOARCH, result["arch"])
}

func TestExecute_NoSolutionMetadata(t *testing.T) {
	p := New()

	ctx := context.Background()
	ctx = settings.IntoContext(ctx, &settings.Run{
		EntryPointSettings: settings.EntryPointSettings{
			FromCli: true,
			Path:    "scafctl/run/resolver",
		},
	})

	out, err := p.Execute(ctx, nil)
	require.NoError(t, err)

	result := out.Data.(map[string]any)
	assert.Equal(t, "cli", result["entrypoint"])

	// solution should be an empty map when not set.
	solMap, ok := result["solution"].(map[string]any)
	require.True(t, ok)
	assert.Empty(t, solMap)
}

func TestDetectShell_FromSHELL(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	assert.Equal(t, "zsh", detectShell())
}

func TestDetectShell_FromComSpec(t *testing.T) {
	t.Setenv("SHELL", "")
	t.Setenv("PSModulePath", "")
	t.Setenv("ComSpec", "/c/Windows/system32/cmd.exe")
	assert.Equal(t, "cmd.exe", detectShell())
}

func TestDetectShell_Empty(t *testing.T) {
	t.Setenv("SHELL", "")
	t.Setenv("PSModulePath", "")
	t.Setenv("ComSpec", "")
	assert.Equal(t, "", detectShell())
}

func TestDetectShell_SHELLTakesPrecedence(t *testing.T) {
	// $SHELL should win even if PSModulePath and ComSpec are set.
	t.Setenv("SHELL", "/usr/bin/bash")
	t.Setenv("PSModulePath", "C:\\modules")
	t.Setenv("ComSpec", "C:\\Windows\\system32\\cmd.exe")
	assert.Equal(t, "bash", detectShell())
}

func TestDetectShell_PSModulePath_Pwsh(t *testing.T) {
	origGoos := goosFunc
	origParent := parentProcessNameFunc
	t.Cleanup(func() {
		goosFunc = origGoos
		parentProcessNameFunc = origParent
	})

	goosFunc = func() string { return "windows" }
	parentProcessNameFunc = func() string { return "pwsh.exe" }

	t.Setenv("SHELL", "")
	t.Setenv("PSModulePath", `C:\Users\test\Documents\PowerShell\Modules`)
	t.Setenv("ComSpec", `C:\Windows\system32\cmd.exe`)

	assert.Equal(t, "pwsh", detectShell())
}

func TestDetectShell_PSModulePath_WindowsPowerShell(t *testing.T) {
	origGoos := goosFunc
	origParent := parentProcessNameFunc
	t.Cleanup(func() {
		goosFunc = origGoos
		parentProcessNameFunc = origParent
	})

	goosFunc = func() string { return "windows" }
	parentProcessNameFunc = func() string { return "powershell.exe" }

	t.Setenv("SHELL", "")
	t.Setenv("PSModulePath", `C:\Users\test\Documents\PowerShell\Modules`)
	t.Setenv("ComSpec", `C:\Windows\system32\cmd.exe`)

	assert.Equal(t, "powershell", detectShell())
}

func TestDetectShell_GitBashOnWindows(t *testing.T) {
	// Git Bash sets $SHELL, so it takes precedence.
	t.Setenv("SHELL", "/usr/bin/bash")
	t.Setenv("PSModulePath", "")
	t.Setenv("ComSpec", `C:\Windows\system32\cmd.exe`)

	assert.Equal(t, "bash", detectShell())
}

func TestDetectShell_CmdExeFallback(t *testing.T) {
	origGoos := goosFunc
	t.Cleanup(func() { goosFunc = origGoos })

	goosFunc = func() string { return "windows" }

	t.Setenv("SHELL", "")
	t.Setenv("PSModulePath", "")
	// Use forward slashes so filepath.Base works correctly on all platforms.
	t.Setenv("ComSpec", "C:/Windows/system32/cmd.exe")

	assert.Equal(t, "cmd.exe", detectShell())
}

func TestDetectPowerShellVariant_Pwsh(t *testing.T) {
	orig := parentProcessNameFunc
	t.Cleanup(func() { parentProcessNameFunc = orig })

	parentProcessNameFunc = func() string { return "pwsh.exe" }
	assert.Equal(t, "pwsh", detectPowerShellVariant())
}

func TestDetectPowerShellVariant_WindowsPowerShell(t *testing.T) {
	orig := parentProcessNameFunc
	t.Cleanup(func() { parentProcessNameFunc = orig })

	parentProcessNameFunc = func() string { return "powershell.exe" }
	assert.Equal(t, "powershell", detectPowerShellVariant())
}

func TestDetectPowerShellVariant_Unknown(t *testing.T) {
	orig := parentProcessNameFunc
	t.Cleanup(func() { parentProcessNameFunc = orig })

	parentProcessNameFunc = func() string { return "" }
	assert.Equal(t, "pwsh", detectPowerShellVariant())
}

func TestDetectPowerShellVariant_UnexpectedParent(t *testing.T) {
	orig := parentProcessNameFunc
	t.Cleanup(func() { parentProcessNameFunc = orig })

	parentProcessNameFunc = func() string { return "explorer.exe" }
	assert.Equal(t, "pwsh", detectPowerShellVariant())
}

func TestExecute_ShellField(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/bash")

	p := New()
	out, err := p.Execute(context.Background(), nil)
	require.NoError(t, err)

	result := out.Data.(map[string]any)
	assert.Equal(t, "bash", result["shell"])
}
