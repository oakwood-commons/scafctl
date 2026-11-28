// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"text/template"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoot_CommandProperties(t *testing.T) {
	t.Parallel()
	cmd, _ := Root(nil)
	require.NotNil(t, cmd)
	if cmd.Use != "scafctl" {
		t.Errorf("Root().Use = %q, want %q", cmd.Use, "scafctl")
	}
	wantShort := "Declaratively discover data, shape configuration, and run portable workflows for humans and AI agents"
	if cmd.Short != wantShort {
		t.Errorf("Root().Short = %q, want %q", cmd.Short, wantShort)
	}
	assert.Contains(t, cmd.Long, wantShort)
	assert.Contains(t, cmd.Long, "MCP")
	if cmd.Annotations["commandType"] != "main" {
		t.Errorf("Root().Annotations[\"commandType\"] = %q, want %q", cmd.Annotations["commandType"], "main")
	}
}

func TestRoot_PersistentFlags(t *testing.T) {
	t.Parallel()
	cmd, _ := Root(nil)
	flags := cmd.PersistentFlags()
	if flags.Lookup("log-level") == nil {
		t.Error("Expected 'log-level' persistent flag to be defined")
	}
	if flags.Lookup("quiet") == nil {
		t.Error("Expected 'quiet' persistent flag to be defined")
	}
	if flags.Lookup("no-color") == nil {
		t.Error("Expected 'no-color' persistent flag to be defined")
	}
	if flags.Lookup("pprof") == nil {
		t.Error("Expected 'pprof' persistent flag to be defined")
	}
	if flags.Lookup("pprof-output-dir") == nil {
		t.Error("Expected 'pprof-output-dir' persistent flag to be defined")
	}
	if flags.Lookup("cwd") == nil {
		t.Error("Expected 'cwd' persistent flag to be defined")
	}
}

func TestRoot_HiddenFlags(t *testing.T) {
	t.Parallel()
	cmd, _ := Root(nil)
	pprofFlag := cmd.PersistentFlags().Lookup("pprof")
	require.NotNil(t, pprofFlag, "Expected 'pprof' flag to exist")
	if !pprofFlag.Hidden {
		t.Error("Expected 'pprof' flag to be hidden")
	}
	pprofOutFlag := cmd.PersistentFlags().Lookup("pprof-output-dir")
	require.NotNil(t, pprofOutFlag, "Expected 'pprof-output-dir' flag to exist")
	if !pprofOutFlag.Hidden {
		t.Error("Expected 'pprof-output-dir' flag to be hidden")
	}
}

func TestRoot_HasVersionSubcommand(t *testing.T) {
	t.Parallel()
	cmd, _ := Root(nil)
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "version" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'version' subcommand to be added")
	}
}

func TestRoot_HasOptionsSubcommand(t *testing.T) {
	t.Parallel()
	cmd, _ := Root(nil)
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "options" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'options' subcommand to be added")
	}
}

func TestRoot_CommandGroups(t *testing.T) {
	t.Parallel()
	cmd, _ := Root(nil)

	groups := cmd.Groups()
	if len(groups) == 0 {
		t.Fatal("Expected command groups to be defined")
	}

	wantGroups := []string{"core", "inspect", "artifact", "config", "plugin"}
	gotIDs := make(map[string]bool)
	for _, g := range groups {
		gotIDs[g.ID] = true
	}
	for _, id := range wantGroups {
		if !gotIDs[id] {
			t.Errorf("Expected group %q to be defined", id)
		}
	}

	// Verify key commands have group assignments.
	subCmds := make(map[string]string)
	for _, sub := range cmd.Commands() {
		subCmds[sub.Name()] = sub.GroupID
	}
	wantAssignments := map[string]string{
		"run":     "core",
		"lint":    "core",
		"get":     "inspect",
		"explain": "inspect",
		"diff":    "inspect",
		"new":     "core",
		"package": "artifact",
		"config":  "config",
		"auth":    "config",
		"plugins": "plugin",
		"mcp":     "plugin",
	}
	for name, wantGroup := range wantAssignments {
		if got := subCmds[name]; got != wantGroup {
			t.Errorf("command %q: GroupID = %q, want %q", name, got, wantGroup)
		}
	}
}

func TestRoot_UsageTemplateHidesGlobalFlags(t *testing.T) {
	t.Parallel()
	cmd, _ := Root(nil)

	// Verify the rendered root help omits flags and references "options".
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	require.NoError(t, err)
	output := buf.String()

	if strings.Contains(output, "Global Flags:") {
		t.Error("Root help output should not contain 'Global Flags:' section")
	}
	if !strings.Contains(output, `Use "scafctl options"`) {
		t.Error("Root help output should reference 'scafctl options' command")
	}
}

func TestRoot_ParallelConstruction(t *testing.T) {
	t.Parallel()
	// Verify that constructing multiple Root() commands concurrently
	// does not cause data races (run with -race to validate).
	const n = 10
	cmds := make([]*cobra.Command, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			cmds[idx], _ = Root(nil)
		}(i)
	}
	wg.Wait()
	for i, cmd := range cmds {
		if cmd == nil {
			t.Errorf("Root() call %d returned nil", i)
		}
	}
}

func TestRoot_WithCustomIOStreams(t *testing.T) {
	t.Parallel()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd, _ := Root(&RootOptions{IOStreams: ioStreams})
	if cmd == nil {
		t.Fatal("Root() with custom IOStreams returned nil")
	}
	// Verify the command was constructed successfully with custom streams—
	// subcommands are added and the command is usable.
	if len(cmd.Commands()) == 0 {
		t.Error("Expected subcommands to be registered")
	}
}

func TestRoot_WithExitFunc(t *testing.T) {
	t.Parallel()
	var captured int
	exitCalled := false
	cmd, _ := Root(&RootOptions{
		ExitFunc: func(code int) {
			exitCalled = true
			captured = code
		},
	})
	if cmd == nil {
		t.Fatal("Root() with ExitFunc returned nil")
	}
	// The exit func is wired through writer options, which are applied
	// during PersistentPreRun. We verify it was accepted without error.
	_ = exitCalled
	_ = captured
}

// stubAuthHandler is a minimal auth.Handler for testing BuiltinAuthHandlers.
type stubAuthHandler struct {
	name        string
	displayName string
	loginCalls  []auth.LoginOptions
}

func (s *stubAuthHandler) Name() string        { return s.name }
func (s *stubAuthHandler) DisplayName() string { return s.displayName }
func (s *stubAuthHandler) SupportedFlows() []auth.Flow {
	return []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode}
}
func (s *stubAuthHandler) Capabilities() []auth.Capability { return nil }
func (s *stubAuthHandler) Login(_ context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	s.loginCalls = append(s.loginCalls, opts)
	return &auth.Result{Claims: &auth.Claims{Email: "user@example.com"}}, nil
}
func (s *stubAuthHandler) Logout(_ context.Context) error                 { return nil }
func (s *stubAuthHandler) Status(_ context.Context) (*auth.Status, error) { return &auth.Status{}, nil }

func (s *stubAuthHandler) GetToken(_ context.Context, _ auth.TokenOptions) (*auth.Token, error) {
	return &auth.Token{AccessToken: "tok"}, nil
}

func (s *stubAuthHandler) InjectAuth(_ context.Context, _ *http.Request, _ auth.TokenOptions) error {
	return nil
}

func TestRoot_WithBuiltinAuthHandlers(t *testing.T) {
	t.Parallel()
	handler := &stubAuthHandler{name: "internal-idp", displayName: "Internal IdP"}
	cmd, cleanup := Root(&RootOptions{
		BinaryName:          "mycli",
		BuiltinAuthHandlers: []auth.Handler{handler},
	})
	defer cleanup()
	require.NotNil(t, cmd)

	// Verify the command tree was constructed with the option accepted.
	assert.Equal(t, "mycli", cmd.Use)
}

func TestRoot_WithBuiltinAuthHandlers_Execution(t *testing.T) {
	t.Parallel()
	handler := &stubAuthHandler{name: "internal-idp", displayName: "Internal IdP"}
	ioStreams, stdout, _ := terminal.NewTestIOStreams()

	cmd, cleanup := Root(&RootOptions{
		IOStreams:           ioStreams,
		BinaryName:          "mycli",
		BuiltinAuthHandlers: []auth.Handler{handler},
	})
	defer cleanup()
	cmd.SetArgs([]string{"auth", "handlers", "-o", "json"})
	err := cmd.Execute()
	require.NoError(t, err)

	output := stdout.String()

	// Parse the JSON output and verify our builtin handler appears.
	var handlers []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &handlers))

	var found bool
	for _, h := range handlers {
		if h["name"] == "internal-idp" {
			found = true
			assert.Equal(t, "Internal IdP", h["displayName"])
			assert.Equal(t, "built-in", h["source"])
			break
		}
	}
	assert.True(t, found, "builtin handler 'internal-idp' should appear in auth handlers output")
}

func TestRoot_WithClusterResolver(t *testing.T) {
	t.Parallel()
	resolver := &kube.MockResolver{}
	cmd, cleanup := Root(&RootOptions{
		BinaryName:      "mycli",
		ClusterResolver: resolver,
	})
	defer cleanup()
	require.NotNil(t, cmd)

	// Verify the command tree was constructed with the option accepted.
	assert.Equal(t, "mycli", cmd.Use)
}

func TestRoot_WithClusterResolver_Execution(t *testing.T) {
	t.Parallel()
	resolver := &kube.MockResolver{}
	ioStreams, _, _ := terminal.NewTestIOStreams()

	var captured kube.ClusterResolver
	cmd, cleanup := Root(&RootOptions{
		IOStreams:       ioStreams,
		BinaryName:      "mycli",
		ClusterResolver: resolver,
		PreRunHook: func(c *cobra.Command, _ []string) error {
			captured = kube.ResolverFromContext(c.Context())
			return nil
		},
	})
	defer cleanup()
	cmd.SetArgs([]string{"auth", "handlers", "-o", "json"})
	require.NoError(t, cmd.Execute())

	// The embedder-provided resolver must be attached to the command context.
	assert.Same(t, resolver, captured)
}

func TestRoot_WithoutClusterResolver_Execution(t *testing.T) {
	t.Parallel()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	// Isolate from the developer's real config (which may declare clusters) by
	// pointing at an empty config file. This keeps the test deterministic
	// regardless of the machine's ~/.config/scafctl/config.yaml.
	emptyCfg := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(emptyCfg, []byte("{}\n"), 0o600))

	capturedSet := false
	var captured kube.ClusterResolver
	cmd, cleanup := Root(&RootOptions{
		IOStreams:  ioStreams,
		BinaryName: "mycli",
		ConfigPath: emptyCfg,
		PreRunHook: func(c *cobra.Command, _ []string) error {
			captured = kube.ResolverFromContext(c.Context())
			capturedSet = true
			return nil
		},
	})
	defer cleanup()
	cmd.SetArgs([]string{"auth", "handlers", "-o", "json"})
	require.NoError(t, cmd.Execute())

	// With no resolver configured, the context must not carry one.
	assert.True(t, capturedSet, "PreRunHook should have run")
	assert.Nil(t, captured)
}

// TestRoot_ConfigPathHonored verifies that RootOptions.ConfigPath is actually
// read: a config file declaring a kube cluster alias must drive the attached
// resolver. Regression guard for the flag-default bug where StringVar reset
// configPath to "" and silently fell back to the global XDG path.
func TestRoot_ConfigPathHonored(t *testing.T) {
	t.Parallel()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(
		"kube:\n"+
			"  clusters:\n"+
			"    aliases:\n"+
			"      lab:\n"+
			"        server: https://api.lab.example.com:6443\n"+
			"        defaultHandler: oidc\n",
	), 0o600))

	var captured kube.ClusterResolver
	cmd, cleanup := Root(&RootOptions{
		IOStreams:  ioStreams,
		BinaryName: "mycli",
		ConfigPath: cfgPath,
		PreRunHook: func(c *cobra.Command, _ []string) error {
			captured = kube.ResolverFromContext(c.Context())
			return nil
		},
	})
	defer cleanup()
	cmd.SetArgs([]string{"auth", "handlers", "-o", "json"})
	require.NoError(t, cmd.Execute())

	// A resolver built from the ConfigPath file must be attached and resolve the
	// alias declared there, proving the embedder-provided ConfigPath was honored.
	require.NotNil(t, captured, "ConfigPath config must drive the cluster resolver")
	info, err := captured.Resolve(context.Background(), "lab")
	require.NoError(t, err)
	assert.Equal(t, "https://api.lab.example.com:6443", info.APIServerURL)
}

// TestRoot_GoTemplateFuncs_Discoverable verifies the embedder contract: a custom
// Go-template function supplied via RootOptions.GoTemplateFuncs is registered
// during startup and surfaces in the discoverability output, even under a
// non-default binary name. Uses a unique function name so the package-global
// registry cannot collide with other tests in this binary.
func TestRoot_GoTemplateFuncs_Discoverable(t *testing.T) {
	gotmpl.ResetRegistryForTesting()
	t.Cleanup(gotmpl.ResetRegistryForTesting)
	ioStreams, stdout, _ := terminal.NewTestIOStreams()

	cmd, cleanup := Root(&RootOptions{
		IOStreams:  ioStreams,
		BinaryName: "mycli",
		GoTemplateFuncs: template.FuncMap{
			"embedderRootDiscoverable": func(s string) string { return s },
		},
	})
	defer cleanup()
	cmd.SetArgs([]string{"get", "template", "functions", "--embedder", "-o", "json"})
	require.NoError(t, cmd.Execute())

	output := stdout.String()
	assert.Contains(t, output, "embedderRootDiscoverable",
		"embedder-registered function should appear in --embedder listing")
	assert.Contains(t, output, gotmpl.SourceEmbedder,
		"embedder function should be tagged with the embedder source")
}

// TestRoot_GoTemplateFuncs_CollisionFailsLoud verifies that an embedder build
// bug -- registering a function whose name collides with a built-in -- fails
// loudly at startup rather than silently dropping the function.
func TestRoot_GoTemplateFuncs_CollisionFailsLoud(t *testing.T) {
	gotmpl.ResetRegistryForTesting()
	t.Cleanup(gotmpl.ResetRegistryForTesting)
	// Ensure the extension factory is set so built-in collisions are detectable.
	RegisterDefaults()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	exitCalled := false
	cmd, cleanup := Root(&RootOptions{
		IOStreams: ioStreams,
		ExitFunc:  func(_ int) { exitCalled = true },
		GoTemplateFuncs: template.FuncMap{
			"upper": func(string) string { return "" },
		},
	})
	defer cleanup()
	cmd.SetArgs([]string{"version"})

	_ = cmd.Execute()
	assert.True(t, exitCalled,
		"ExitFunc should be called when an embedder function collides with a built-in")
}

// TestRoot_GoTemplateFuncs_ReExecuteNoSelfCollision verifies that executing the
// same command more than once does not re-register the process-global embedder
// functions and thus does not self-collide (register-once is enforced globally,
// so the root wiring must guard its own registration).
func TestRoot_GoTemplateFuncs_ReExecuteNoSelfCollision(t *testing.T) {
	gotmpl.ResetRegistryForTesting()
	t.Cleanup(gotmpl.ResetRegistryForTesting)
	ioStreams, _, _ := terminal.NewTestIOStreams()
	exitCalled := false
	cmd, cleanup := Root(&RootOptions{
		IOStreams: ioStreams,
		ExitFunc:  func(_ int) { exitCalled = true },
		GoTemplateFuncs: template.FuncMap{
			"embedderRootReexec": func(s string) string { return s },
		},
	})
	defer cleanup()

	cmd.SetArgs([]string{"version"})
	require.NoError(t, cmd.Execute())
	require.NoError(t, cmd.Execute(), "second execution must not self-collide")
	assert.False(t, exitCalled,
		"re-executing the same command must not fail the embedder registration")
}

// partialSuccessSolutionYAML is a workflow whose second action fails but is
// tolerated via continueOnError, yielding FinalStatus partial-success. The run
// completes (no hard failure), so without --detailed-exit-code it exits 0.
//
// The actions are serialized via dependsOn so they run in separate phases: the
// in-process test IOStreams use a plain bytes.Buffer, which the concurrent
// same-phase progress-callback writes would race on under -race (the real
// binary writes to os.Stderr and is unaffected).
const partialSuccessSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: embedder-partial-success
  version: 1.0.0
spec:
  resolvers: {}
  workflow:
    actions:
      good:
        provider: message
        inputs:
          message: "GOOD_RAN"
          type: info
      bad:
        provider: go-template
        dependsOn: [good]
        continueOnError: true
        inputs:
          template: "{{ undefinedFunc .x }}"
          data:
            x: hello
`

// TestRoot_DetailedExitCode_EmbedderDefault verifies that an embedder can opt
// into the distinct partial-success exit code via RootOptions.DetailedExitCode
// WITHOUT the caller passing the --detailed-exit-code flag. This proves the
// default flows through settings.Run (RootOptions does not thread into the run
// subcommand tree). It covers both run subcommands (independent call sites) and
// the three precedence cases: default true (exit 12), default false
// (non-breaking exit 0), and an explicit --detailed-exit-code=false overriding
// an embedder default of true.
func TestRoot_DetailedExitCode_EmbedderDefault(t *testing.T) {
	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(partialSuccessSolutionYAML), 0o600))

	run := func(t *testing.T, subcmd string, embedderDefault bool, extraArgs ...string) int {
		t.Helper()
		ioStreams, _, _ := terminal.NewTestIOStreams()
		cmd, cleanup := Root(&RootOptions{
			IOStreams:        ioStreams,
			ExitFunc:         func(int) {}, // never terminate the test process
			DetailedExitCode: embedderDefault,
		})
		defer cleanup()
		args := append([]string{"run", subcmd, "-f", solutionPath, "-o", "json"}, extraArgs...)
		cmd.SetArgs(args)
		return exitcode.GetCode(cmd.Execute())
	}

	for _, subcmd := range []string{"solution", "action"} {
		t.Run(subcmd+"/default true returns PartialSuccess without CLI flag", func(t *testing.T) {
			assert.Equal(t, exitcode.PartialSuccess, run(t, subcmd, true),
				"RootOptions.DetailedExitCode=true must yield exit 12 on partial success")
		})

		t.Run(subcmd+"/default false returns Success (non-breaking)", func(t *testing.T) {
			assert.Equal(t, exitcode.Success, run(t, subcmd, false),
				"RootOptions.DetailedExitCode=false must keep partial success at exit 0")
		})

		t.Run(subcmd+"/explicit flag=false overrides embedder default true", func(t *testing.T) {
			assert.Equal(t, exitcode.Success, run(t, subcmd, true, "--detailed-exit-code=false"),
				"an explicit --detailed-exit-code=false must override an embedder default of true")
		})
	}
}

func TestRoot_LoginConfigFileBrowserValidationAndNormalization(t *testing.T) {
	t.Parallel()

	t.Run("invalid auth.browser in config file aborts auth login and kube login with ConfigError", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		badCfgPath := filepath.Join(tmpDir, "bad-config.yaml")
		badCfgYAML := "version: 1\nauth:\n  browser: \"--headless\"\n  entra:\n    tenantId: configured-tenant\n"
		require.NoError(t, os.WriteFile(badCfgPath, []byte(badCfgYAML), 0o600))

		for _, args := range [][]string{
			{"auth", "login", "internal-idp"},
			{"auth", "login", "internal-idp", "--browser", "firefox"},
			{"kube", "login", "prod", "--handler", "internal-idp", "--server", "https://api.example.com:6443", "--kubeconfig", filepath.Join(tmpDir, "kubeconfig")},
		} {
			handler := &stubAuthHandler{name: "internal-idp", displayName: "Internal IdP"}
			ioStreams, _, stderr := terminal.NewTestIOStreams()
			cmd, cleanup := Root(&RootOptions{
				BinaryName:          "mycli",
				ConfigPath:          badCfgPath,
				IOStreams:           ioStreams,
				BuiltinAuthHandlers: []auth.Handler{handler},
			})
			cmd.SetArgs(args)
			err := cmd.Execute()
			cleanup()

			require.Error(t, err, "args %v must fail when config file fails validation", args)
			assert.Equal(t, exitcode.ConfigError, exitcode.GetCode(err))
			assert.Contains(t, stderr.String(), "must not start with '-'")
			assert.Empty(t, handler.loginCalls, "login must not execute with discarded config for args %v", args)
		}
	})

	t.Run("single-quoted auth.browser in config file normalizes before handler login", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		quotedCfgPath := filepath.Join(tmpDir, "quoted-config.yaml")
		quotedCfgYAML := "version: 1\nauth:\n  browser: \"'firefox'\"\n"
		require.NoError(t, os.WriteFile(quotedCfgPath, []byte(quotedCfgYAML), 0o600))

		authHandler := &stubAuthHandler{name: "internal-idp", displayName: "Internal IdP"}
		ioStreams, _, _ := terminal.NewTestIOStreams()
		authCmd, authCleanup := Root(&RootOptions{
			BinaryName:          "mycli",
			ConfigPath:          quotedCfgPath,
			IOStreams:           ioStreams,
			BuiltinAuthHandlers: []auth.Handler{authHandler},
		})
		defer authCleanup()
		authCmd.SetArgs([]string{"auth", "login", "internal-idp", "--flow", "interactive"})
		require.NoError(t, authCmd.Execute())
		require.Len(t, authHandler.loginCalls, 1)
		assert.Equal(t, "firefox", authHandler.loginCalls[0].Browser)

		kubeHandler := &stubAuthHandler{name: "internal-idp", displayName: "Internal IdP"}
		kubeStreams, _, _ := terminal.NewTestIOStreams()
		kubeCmd, kubeCleanup := Root(&RootOptions{
			BinaryName:          "mycli",
			ConfigPath:          quotedCfgPath,
			IOStreams:           kubeStreams,
			BuiltinAuthHandlers: []auth.Handler{kubeHandler},
		})
		defer kubeCleanup()
		kubeCmd.SetArgs([]string{
			"kube", "login", "prod",
			"--handler", "internal-idp",
			"--server", "https://api.example.com:6443",
			"--kubeconfig", filepath.Join(tmpDir, "kubeconfig"),
		})
		require.NoError(t, kubeCmd.Execute())
		require.Len(t, kubeHandler.loginCalls, 1)
		assert.Equal(t, "firefox", kubeHandler.loginCalls[0].Browser)
	})
}

// TestRoot_ResolverHelp_ConfigActionFiles verifies that `run resolver --help`
// and `validate resolver --help` render the dynamic resolver parameter table
// when custom action discovery filenames come from user configuration
// (ConfigPath / --config) or RootOptions.ConfigDefaults, even though Cobra
// skips PersistentPreRun on --help, while preserving explicit embedder
// RootOptions.ActionDiscoveryFileNames overrides.
func TestRoot_ResolverHelp_ConfigActionFiles(t *testing.T) {
	t.Parallel()

	const deploySolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: config-help-deploy
  version: 1.0.0
spec:
  resolvers:
    targetEnv:
      type: string
      description: target environment from deploy.yaml
      resolve:
        with:
          - provider: parameter
            inputs:
              key: deployEnv
`

	const overrideSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: embedder-override
  version: 1.0.0
spec:
  resolvers:
    clusterName:
      type: string
      description: target cluster from override.yaml
      resolve:
        with:
          - provider: parameter
            inputs:
              key: clusterParam
`

	runHelp := func(t *testing.T, workDir string, opts *RootOptions, args ...string) string {
		t.Helper()
		ioStreams, _, _ := terminal.NewTestIOStreams()
		rootOpts := *opts
		rootOpts.IOStreams = ioStreams
		cmd, cleanup := Root(&rootOpts)
		defer cleanup()

		var buf strings.Builder
		cmd.SetOut(&buf)
		cmd.SetArgs(args)
		err := cmd.ExecuteContext(provider.WithWorkingDirectory(t.Context(), workDir))
		require.NoError(t, err)
		return buf.String()
	}

	subcmds := [][]string{
		{"run", "resolver", "--help"},
		{"validate", "resolver", "--help"},
	}

	for _, args := range subcmds {
		label := strings.Join(args[:2], "_")

		t.Run(label+"/user config discovery.actionFiles", func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(workDir, "deploy.yaml"), []byte(deploySolutionYAML), 0o600))

			cfgPath := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(cfgPath, []byte("discovery:\n  actionFiles:\n    - deploy.yaml\n"), 0o600))

			out := runHelp(t, workDir, &RootOptions{
				BinaryName: "mycli",
				ConfigPath: cfgPath,
			}, args...)
			assert.Contains(t, out, "deployEnv")
			assert.Contains(t, out, "target environment from deploy.yaml")
		})

		t.Run(label+"/RootOptions.ConfigDefaults discovery.actionFiles", func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(workDir, "deploy.yaml"), []byte(deploySolutionYAML), 0o600))

			missingCfg := filepath.Join(t.TempDir(), "missing-config.yaml")
			out := runHelp(t, workDir, &RootOptions{
				BinaryName:     "mycli",
				ConfigPath:     missingCfg,
				ConfigDefaults: []byte("discovery:\n  actionFiles:\n    - deploy.yaml\n"),
			}, args...)
			assert.Contains(t, out, "deployEnv")
			assert.Contains(t, out, "target environment from deploy.yaml")
		})

		t.Run(label+"/explicit ActionDiscoveryFileNames overrides config and ConfigDefaults", func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(workDir, "deploy.yaml"), []byte(deploySolutionYAML), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(workDir, "override.yaml"), []byte(overrideSolutionYAML), 0o600))

			cfgPath := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(cfgPath, []byte("discovery:\n  actionFiles:\n    - deploy.yaml\n"), 0o600))

			out := runHelp(t, workDir, &RootOptions{
				BinaryName:               "mycli",
				ConfigPath:               cfgPath,
				ConfigDefaults:           []byte("discovery:\n  actionFiles:\n    - deploy.yaml\n"),
				ActionDiscoveryFileNames: []string{"override.yaml"},
			}, args...)
			assert.Contains(t, out, "clusterParam")
			assert.Contains(t, out, "target cluster from override.yaml")
			assert.NotContains(t, out, "deployEnv")
		})
	}
}

// TestRoot_ResolverHelp_EnvActionFilesDottedExeBinaryName verifies that a binary
// name like "my.cli.exe" (which Root sanitizes once to "my.cli") derives the
// same MY_CLI_* environment prefix during --help as during command execution
// rather than re-sanitizing "my.cli" into "my" (MY_*).
func TestRoot_ResolverHelp_EnvActionFilesDottedExeBinaryName(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv.
	const deploySolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: env-help-deploy
  version: 1.0.0
spec:
  resolvers:
    targetEnv:
      type: string
      description: target environment from env override
      resolve:
        with:
          - provider: parameter
            inputs:
              key: envOverrideParam
`
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "deploy.yaml"), []byte(deploySolutionYAML), 0o600))
	missingCfg := filepath.Join(t.TempDir(), "missing-config.yaml")

	t.Setenv("MY_CLI_DISCOVERY_ACTIONFILES", "deploy.yaml")
	t.Setenv("MY_DISCOVERY_ACTIONFILES", "wrong.yaml")

	for _, args := range [][]string{
		{"run", "resolver", "--help"},
		{"validate", "resolver", "--help"},
	} {
		ioStreams, _, _ := terminal.NewTestIOStreams()
		cmd, cleanup := Root(&RootOptions{
			IOStreams:  ioStreams,
			BinaryName: "my.cli.exe",
			ConfigPath: missingCfg,
		})
		var buf strings.Builder
		cmd.SetOut(&buf)
		cmd.SetArgs(args)
		require.NoError(t, cmd.ExecuteContext(provider.WithWorkingDirectory(t.Context(), workDir)))
		cleanup()

		assert.Contains(t, buf.String(), "envOverrideParam", "args %v must honor MY_CLI_DISCOVERY_ACTIONFILES", args)
		assert.Contains(t, buf.String(), "target environment from env override")
	}
}

func TestRoot_NoColorPrecedence(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv.
	emptyCfg := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(emptyCfg, []byte("{}\n"), 0o600))

	cfgNoColor := filepath.Join(t.TempDir(), "config-nocolor.yaml")
	require.NoError(t, os.WriteFile(cfgNoColor, []byte("settings:\n  noColor: true\n"), 0o600))

	runCheck := func(t *testing.T, cfgPath string, extraArgs ...string) (noColor, colorEnabled bool) {
		t.Helper()
		ioStreams, _, _ := terminal.NewTestIOStreams()
		ioStreams.ColorEnabled = true

		var gotParams *settings.Run
		cmd, cleanup := Root(&RootOptions{
			IOStreams:  ioStreams,
			ConfigPath: cfgPath,
			PreRunHook: func(c *cobra.Command, _ []string) error {
				gotParams, _ = settings.FromContext(c.Context())
				return nil
			},
		})
		defer cleanup()

		cmd.SetArgs(append([]string{"version", "-o", "json"}, extraArgs...))
		require.NoError(t, cmd.Execute())
		require.NotNil(t, gotParams)
		return gotParams.NoColor, ioStreams.ColorEnabled
	}

	t.Run("default keeps color enabled when NO_COLOR is empty", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		noColor, colorEnabled := runCheck(t, emptyCfg)
		assert.False(t, noColor)
		assert.True(t, colorEnabled)
	})

	t.Run("NO_COLOR=1 disables color and ioStreams.ColorEnabled", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		noColor, colorEnabled := runCheck(t, emptyCfg)
		assert.True(t, noColor)
		assert.False(t, colorEnabled)
	})

	t.Run("explicit --no-color=false overrides NO_COLOR=1 and config", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		noColor, colorEnabled := runCheck(t, cfgNoColor, "--no-color=false")
		assert.False(t, noColor)
		assert.True(t, colorEnabled)
	})

	t.Run("config settings.noColor disables color when NO_COLOR is unset", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		noColor, colorEnabled := runCheck(t, cfgNoColor)
		assert.True(t, noColor)
		assert.False(t, colorEnabled)
	})
}

func TestBuildBaseAuthClientOpts(t *testing.T) {
	t.Parallel()

	deps := &plugin.HostServiceDeps{PromptBroker: &plugin.AuthPromptBroker{}}
	tests := []struct {
		level     string
		wantDebug bool
	}{
		{level: "none", wantDebug: false},
		{level: "error", wantDebug: false},
		{level: "warn", wantDebug: false},
		{level: "info", wantDebug: false},
		{level: "0", wantDebug: false},
		{level: "invalid", wantDebug: false},
		{level: "debug", wantDebug: true},
		{level: "trace", wantDebug: true},
		{level: "1", wantDebug: true},
		{level: "2", wantDebug: true},
	}

	for _, tc := range tests {
		t.Run(tc.level, func(t *testing.T) {
			t.Parallel()
			base := buildBaseAuthClientOpts(deps, tc.level)
			assert.Equal(t, tc.wantDebug, plugin.DebugLoggingEnabled(base...))

			scoped := scopedAuthClientOpts(base, "github")
			assert.Equal(t, tc.wantDebug, plugin.DebugLoggingEnabled(scoped...))
			resolved := plugin.ResolvedHostDeps(scoped...)
			require.NotNil(t, resolved)
			assert.Equal(t, "scafctl.auth.github.", resolved.AllowedSecretPrefix)

			lazyCfg := cachedAuthHandlerConfig("entra", "/bin/fake", nil, base, nil)
			assert.Equal(t, tc.wantDebug, plugin.DebugLoggingEnabled(lazyCfg.ClientOpts...))
		})
	}
}

func TestRoot_AuthPluginDebugLogging_EmbedderPrecedence(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv.
	xdgCache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdgCache)

	cacheName := plugin.PluginCacheKey("github", solution.PluginKindAuthHandler)
	cache := plugin.NewCache(settings.PluginCacheDirFor("mycli"))
	_, err := cache.Put(cacheName, "1.0.0", plugin.CurrentPlatform(), []byte("cached-plugin"))
	require.NoError(t, err)

	emptyCfg := filepath.Join(t.TempDir(), "empty-config.yaml")
	require.NoError(t, os.WriteFile(emptyCfg, []byte("{}\n"), 0o600))

	debugCfg := filepath.Join(t.TempDir(), "debug-config.yaml")
	require.NoError(t, os.WriteFile(debugCfg, []byte("logging:\n  level: debug\n"), 0o600))

	infoCfg := filepath.Join(t.TempDir(), "info-config.yaml")
	require.NoError(t, os.WriteFile(infoCfg, []byte("logging:\n  level: info\n"), 0o600))

	runCheck := func(t *testing.T, cfgPath string, extraArgs ...string) bool {
		t.Helper()
		ioStreams, _, _ := terminal.NewTestIOStreams()
		officialReg := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
			{Name: "github", CatalogRef: "github"},
		})

		var lazyHandler *plugin.LazyAuthHandlerWrapper
		cmd, cleanup := Root(&RootOptions{
			IOStreams:            ioStreams,
			BinaryName:           "mycli",
			ConfigPath:           cfgPath,
			OfficialAuthHandlers: officialReg,
			PreRunHook: func(c *cobra.Command, _ []string) error {
				reg := auth.RegistryFromContext(c.Context())
				require.NotNil(t, reg)
				h, ok := reg.GetRegistered("github")
				require.True(t, ok, "cached official handler must be registered")
				var castOK bool
				lazyHandler, castOK = h.(*plugin.LazyAuthHandlerWrapper)
				require.True(t, castOK, "expected LazyAuthHandlerWrapper, got %T", h)
				return nil
			},
		})
		defer cleanup()

		cmd.SetArgs(append([]string{"version", "-o", "json"}, extraArgs...))
		require.NoError(t, cmd.Execute())
		require.NotNil(t, lazyHandler)
		return lazyHandler.DebugLoggingEnabled()
	}

	t.Run("default log level leaves plugin debug logging disabled", func(t *testing.T) {
		t.Setenv("MYCLI_DEBUG", "")
		t.Setenv("MYCLI_LOG_LEVEL", "")
		assert.False(t, runCheck(t, emptyCfg))
	})

	t.Run("info log level leaves plugin debug logging disabled", func(t *testing.T) {
		t.Setenv("MYCLI_DEBUG", "")
		t.Setenv("MYCLI_LOG_LEVEL", "")
		assert.False(t, runCheck(t, emptyCfg, "--log-level", "info"))
	})

	t.Run("--debug flag enables plugin debug logging", func(t *testing.T) {
		t.Setenv("MYCLI_DEBUG", "")
		t.Setenv("MYCLI_LOG_LEVEL", "")
		assert.True(t, runCheck(t, emptyCfg, "--debug"))
	})

	t.Run("--log-level debug enables plugin debug logging", func(t *testing.T) {
		t.Setenv("MYCLI_DEBUG", "")
		t.Setenv("MYCLI_LOG_LEVEL", "")
		assert.True(t, runCheck(t, emptyCfg, "--log-level", "debug"))
	})

	t.Run("--log-level trace enables plugin debug logging", func(t *testing.T) {
		t.Setenv("MYCLI_DEBUG", "")
		t.Setenv("MYCLI_LOG_LEVEL", "")
		assert.True(t, runCheck(t, emptyCfg, "--log-level", "trace"))
	})

	t.Run("embedder MYCLI_DEBUG env var enables plugin debug logging", func(t *testing.T) {
		t.Setenv("MYCLI_DEBUG", "1")
		t.Setenv("MYCLI_LOG_LEVEL", "")
		assert.True(t, runCheck(t, emptyCfg))
	})

	t.Run("embedder MYCLI_LOG_LEVEL=debug env var enables plugin debug logging", func(t *testing.T) {
		t.Setenv("MYCLI_DEBUG", "")
		t.Setenv("MYCLI_LOG_LEVEL", "debug")
		assert.True(t, runCheck(t, emptyCfg))
	})

	t.Run("config logging.level debug enables plugin debug logging", func(t *testing.T) {
		t.Setenv("MYCLI_DEBUG", "")
		t.Setenv("MYCLI_LOG_LEVEL", "")
		assert.True(t, runCheck(t, debugCfg))
	})

	t.Run("explicit --log-level info overrides config debug", func(t *testing.T) {
		t.Setenv("MYCLI_DEBUG", "")
		t.Setenv("MYCLI_LOG_LEVEL", "")
		assert.False(t, runCheck(t, debugCfg, "--log-level", "info"))
	})

	t.Run("explicit --debug overrides config info", func(t *testing.T) {
		t.Setenv("MYCLI_DEBUG", "")
		t.Setenv("MYCLI_LOG_LEVEL", "")
		assert.True(t, runCheck(t, infoCfg, "--debug"))
	})
}
