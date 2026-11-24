// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/oakwood-commons/kvx/pkg/tui"
	authpkg "github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func executeOAuth2RootCmd(
	t *testing.T,
	ctx context.Context,
	cliParams *settings.Run,
	configPath string,
	stdin string,
	args ...string,
) (stdoutStr, stderrStr string, err error) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	in := io.NopCloser(strings.NewReader(stdin))
	ioStreams := terminal.NewIOStreams(in, &stdout, &stderr, false)
	w := writer.New(ioStreams, cliParams)
	ctx = writer.WithWriter(ctx, w)

	root := &cobra.Command{Use: resolveBinaryName(cliParams), SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().String("config", configPath, "")
	root.AddCommand(CommandOAuth2(cliParams, ioStreams, resolveBinaryName(cliParams)+"/auth"))
	root.SetContext(ctx)
	root.SetArgs(append([]string{"oauth2"}, args...))

	err = root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestCommandOAuth2_SubcommandRegistrationAndEmbedderName(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams := terminal.NewIOStreams(nil, &bytes.Buffer{}, &bytes.Buffer{}, false)

	cmd := CommandOAuth2(cliParams, ioStreams, "mycli/auth")
	assert.Equal(t, "oauth2", cmd.Use)
	assert.Contains(t, cmd.Long, "mycli auth oauth2")
	assert.NotContains(t, cmd.Long, "scafctl")

	subNames := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		subNames = append(subNames, sub.Name())
		assert.Contains(t, sub.Long, "mycli auth oauth2", "subcommand %q must use embedder binary name", sub.Name())
		assert.NotContains(t, sub.Long, "scafctl", "subcommand %q must not hardcode scafctl", sub.Name())
	}
	assert.ElementsMatch(t, []string{"add", "remove", "list"}, subNames)
}

func TestCommandOAuth2_AddListRemove_EndToEndUnit(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	store := secrets.NewMockStore()
	officialReg := authofficial.NewRegistry()
	ctx := authofficial.WithRegistry(secrets.WithStore(context.Background(), store), officialReg)
	cliParams := settings.NewCliParams()

	// 1. Empty list: -o json emits [], -o quiet emits nothing, human format writes stderr notice.
	stdout, stderr, err := executeOAuth2RootCmd(t, ctx, cliParams, configPath, "", "list", "-o", "json")
	require.NoError(t, err)
	assert.Equal(t, "[]\n", stdout)
	assert.Empty(t, stderr)

	stdout, stderr, err = executeOAuth2RootCmd(t, ctx, cliParams, configPath, "", "list", "-o", "quiet")
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)

	stdout, stderr, err = executeOAuth2RootCmd(t, ctx, cliParams, configPath, "", "list")
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "no custom OAuth2 handlers configured")

	// 2. Add client_credentials handler with --client-secret-stdin.
	stdout, stderr, err = executeOAuth2RootCmd(
		t, ctx, cliParams, configPath, "super-secret-value\n",
		"add", "quay",
		"--display-name", "Quay.io",
		"--token-url", "https://quay.io/oauth/token",
		"--client-id", "quay-app",
		"--default-flow", "client_credentials",
		"--scopes", "repo:read,repo:write",
		"--registry", "quay.io",
		"--registry-username", "$oauthtoken",
		"--client-secret-stdin",
	)
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, `Added custom OAuth2 handler "quay"`)
	assert.Contains(t, stdout, "secret://quay-client-secret")
	assert.NotContains(t, stdout+stderr, "super-secret-value")

	secretBytes, getErr := store.Get(ctx, "quay-client-secret")
	require.NoError(t, getErr)
	assert.Equal(t, "super-secret-value", string(secretBytes))

	// 3. Add workload_identity handler via flags.
	_, stderr, err = executeOAuth2RootCmd(
		t, ctx, cliParams, configPath, "",
		"add", "sts-broker",
		"--display-name", "STS Broker",
		"--token-url", "https://sts.example.com/v1/oauth/token",
		"--federated-token-file", "/var/run/secrets/tokens/jwt",
		"--workload-identity-grant-type", "token_exchange",
		"--subject-token-type", "urn:ietf:params:oauth:token-type:jwt",
		"--audience", "https://api.example.com",
		"--default-flow", "workload_identity",
	)
	require.NoError(t, err, "stderr: %s", stderr)

	// 4. Add PAT handler via flags.
	_, stderr, err = executeOAuth2RootCmd(
		t, ctx, cliParams, configPath, "",
		"add", "quay-robot",
		"--default-flow", "pat",
		"--token", "env://QUAY_ROBOT_TOKEN",
		"--username", "acme+ci_robot",
		"--verify-url", "https://quay.io/api/v1/user/",
	)
	require.NoError(t, err, "stderr: %s", stderr)

	// 5. List -o json: secrets must be redacted via SanitizedCustomOAuth2.
	stdout, stderr, err = executeOAuth2RootCmd(t, ctx, cliParams, configPath, "", "list", "-o", "json")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.NotContains(t, stdout, "super-secret-value")
	assert.NotContains(t, stdout, "quay-client-secret")
	assert.NotContains(t, stdout, "QUAY_ROBOT_TOKEN")

	var listed []appconfig.SanitizedCustomOAuth2
	require.NoError(t, json.Unmarshal([]byte(stdout), &listed))
	require.Len(t, listed, 3)
	assert.Equal(t, "quay", listed[0].Name)
	assert.Equal(t, appconfig.RedactedValue, listed[0].ClientSecret)
	assert.Equal(t, []string{"repo:read", "repo:write"}, listed[0].Scopes)
	assert.Equal(t, "sts-broker", listed[1].Name)
	assert.Equal(t, "token_exchange", listed[1].WorkloadIdentityGrantType)
	assert.Equal(t, "quay-robot", listed[2].Name)
	assert.Equal(t, appconfig.RedactedValue, listed[2].Token)
	assert.Equal(t, "acme+ci_robot", listed[2].Username)

	// 6. Overwrite with --force (implicit grant responseType=token).
	stdout, stderr, err = executeOAuth2RootCmd(
		t, ctx, cliParams, configPath, "",
		"add", "quay",
		"--display-name", "Quay Implicit",
		"--authorize-url", "https://quay.io/oauth/authorize",
		"--client-id", "quay-implicit",
		"--default-flow", "interactive",
		"--response-type", "token",
		"--callback-port", "8400",
		"--callback-host", "localhost",
		"--callback-path", "/callback",
		"--disable-pkce",
		"--force",
	)
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, `Updated custom OAuth2 handler "quay"`)

	// 7. Shell completion on remove suggests configured customOAuth2 names.
	rmCmd := commandOAuth2Remove(cliParams, terminal.NewIOStreams(nil, &bytes.Buffer{}, &bytes.Buffer{}, false), "")
	root := &cobra.Command{Use: "scafctl"}
	root.PersistentFlags().String("config", configPath, "")
	root.AddCommand(rmCmd)
	rmCmd.SetContext(ctx)
	completions, directive := rmCmd.ValidArgsFunction(rmCmd, nil, "")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	assert.ElementsMatch(t, []string{"quay", "sts-broker", "quay-robot"}, completions)

	// 8. Remove existing handler.
	stdout, stderr, err = executeOAuth2RootCmd(t, ctx, cliParams, configPath, "", "remove", "quay")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, `Removed custom OAuth2 handler "quay"`)

	// 9. Remove absent handler returns exitcode.FileNotFound.
	_, stderr, err = executeOAuth2RootCmd(t, ctx, cliParams, configPath, "", "remove", "quay")
	require.Error(t, err)
	assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
	assert.Contains(t, stderr, `custom OAuth2 handler "quay" not found`)
}

func TestCommandOAuth2Add_FromFileAndFlagOverrides(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	handlerFile := filepath.Join(tmpDir, "dcr-handler.yaml")

	yamlContent := `
name: corp-dcr
displayName: Corp DCR
deviceAuthURL: https://idp.example.com/oauth2/device
tokenURL: https://idp.example.com/oauth2/token
defaultFlow: device_code
deviceCodePollInterval: 5
dynamicClientRegistration:
  registrationEndpoint: https://idp.example.com/oauth2/register
  initialAccessToken: env://DCR_TOKEN
`
	require.NoError(t, os.WriteFile(handlerFile, []byte(yamlContent), 0o600))

	cliParams := settings.NewCliParams()
	stdout, stderr, err := executeOAuth2RootCmd(
		t, context.Background(), cliParams, configPath, "",
		"add", "corp-dcr-override",
		"--from-file", handlerFile,
		"--display-name", "Corp DCR Override",
		"--device-code-poll-interval", "10",
	)
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, `Added custom OAuth2 handler "corp-dcr-override"`)

	listOut, _, err := executeOAuth2RootCmd(t, context.Background(), cliParams, configPath, "", "list", "-o", "json")
	require.NoError(t, err)

	var listed []appconfig.SanitizedCustomOAuth2
	require.NoError(t, json.Unmarshal([]byte(listOut), &listed))
	require.Len(t, listed, 1)
	assert.Equal(t, "corp-dcr-override", listed[0].Name)
	assert.Equal(t, "Corp DCR Override", listed[0].DisplayName)
	assert.Equal(t, 10, listed[0].DeviceCodePollInterval)
	require.NotNil(t, listed[0].DynamicClientRegistration)
	assert.Equal(t, appconfig.RedactedValue, listed[0].DynamicClientRegistration.InitialAccessToken)

	// Verify --federated-token flag overrides federatedTokenFile from --from-file
	// without failing mutual-exclusion validation, and literal secrets emit warnings.
	wifFile := filepath.Join(tmpDir, "wif-handler.yaml")
	wifYAML := `
name: wif-file
tokenURL: https://idp.example.com/oauth2/token
clientID: wif-client
clientSecret: literal-secret-in-file
federatedTokenFile: /var/run/secrets/tokens/jwt
defaultFlow: workload_identity
dynamicClientRegistration:
  registrationEndpoint: https://idp.example.com/oauth2/register
  initialAccessToken: literal-dcr-token
`
	require.NoError(t, os.WriteFile(wifFile, []byte(wifYAML), 0o600))

	_, stderr, err = executeOAuth2RootCmd(
		t, context.Background(), cliParams, configPath, "",
		"add",
		"--from-file", wifFile,
		"--federated-token", "env://ACTIONS_ID_TOKEN",
	)
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stderr, "clientSecret in")
	assert.Contains(t, stderr, "dynamicClientRegistration.initialAccessToken in")
}

func TestCommandOAuth2_EmbedderRuntimeExecutionAndFormats(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(""), 0o600))

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"

	// 1. Empty human list uses embedder binary name in stderr guidance.
	stdout, stderr, err := executeOAuth2RootCmd(t, context.Background(), cliParams, configPath, "", "list")
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "mycli auth oauth2 add")
	assert.NotContains(t, stderr, "scafctl")

	// 2. Empty list with CEL expression evaluates 0 rather than short-circuiting.
	stdout, stderr, err = executeOAuth2RootCmd(t, context.Background(), cliParams, configPath, "", "list", "-o", "json", "-e", "size(_)")
	require.NoError(t, err)
	assert.Equal(t, "0\n", stdout)
	assert.Empty(t, stderr)

	// 3. Rejects reserved embedder secret prefix on CLI flag.
	_, stderr, err = executeOAuth2RootCmd(
		t, context.Background(), cliParams, configPath, "",
		"add", "quay",
		"--default-flow", "pat",
		"--token", "secret://mycli.internal",
	)
	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	assert.Contains(t, stderr, "mycli.")

	// 4. WithBaseConfig propagates through Cobra list across YAML, CSV, TOML, -w, and -e.
	baseYAML := []byte(`
auth:
  customOAuth2:
    - name: embedder-idp
      displayName: Embedder IDP
      defaultFlow: pat
      token: env://EMBEDDER_SECRET_TOKEN
      registry: registry.example.com
`)
	ctx := appconfig.WithManagerOptions(context.Background(), []appconfig.ManagerOption{
		appconfig.WithBaseConfig(baseYAML),
	})

	for _, format := range []string{"yaml", "csv", "toml"} {
		out, errOut, fmtErr := executeOAuth2RootCmd(t, ctx, cliParams, configPath, "", "list", "-o", format)
		require.NoError(t, fmtErr, "format %s stderr: %s", format, errOut)
		assert.Contains(t, out, "embedder-idp")
		assert.Contains(t, out, appconfig.RedactedValue)
		assert.NotContains(t, out, "EMBEDDER_SECRET_TOKEN")
	}

	exprOut, _, err := executeOAuth2RootCmd(t, ctx, cliParams, configPath, "", "list", "-o", "json", "-e", "_[0].displayName")
	require.NoError(t, err)
	assert.Equal(t, "\"Embedder IDP\"\n", exprOut)

	whereOut, _, err := executeOAuth2RootCmd(t, ctx, cliParams, configPath, "", "list", "-o", "json", "-w", `_.name == "missing"`)
	require.NoError(t, err)
	assert.Equal(t, "[]\n", whereOut)
}

func TestCommandOAuth2_MalformedConfigReturnsConfigError(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "broken.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("auth:\n  customOAuth2: [\n"), 0o600))
	cliParams := settings.NewCliParams()

	for _, subArgs := range [][]string{
		{"list", "-o", "json"},
		{"remove", "quay"},
		{"add", "quay", "--default-flow", "pat", "--token", "env://TOK"},
	} {
		_, _, err := executeOAuth2RootCmd(t, context.Background(), cliParams, configPath, "", subArgs...)
		require.Error(t, err)
		assert.Equal(t, exitcode.ConfigError, exitcode.GetCode(err))
	}
}

func TestCommandOAuth2Add_SecurityAndValidationErrors(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	officialReg := authofficial.NewRegistry()
	authReg := authpkg.NewRegistry()
	require.NoError(t, authReg.Register(authpkg.NewMockHandler("builtin-sso")))
	ctx := authpkg.WithRegistry(authofficial.WithRegistry(context.Background(), officialReg), authReg)
	cliParams := settings.NewCliParams()

	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{
			name: "rejects bare --client-secret",
			args: []string{
				"add", "quay",
				"--token-url", "https://quay.io/oauth/token",
				"--client-id", "cid",
				"--client-secret", "raw-secret-on-argv",
				"--default-flow", "client_credentials",
			},
			wantCode: exitcode.InvalidInput,
			wantErr:  "--client-secret-stdin",
		},
		{
			name: "rejects bare --federated-token",
			args: []string{
				"add", "wif",
				"--token-url", "https://idp.example.com/token",
				"--client-id", "cid",
				"--federated-token", "eyJhbGciOiJSUzI1NiJ9.raw",
				"--default-flow", "workload_identity",
			},
			wantCode: exitcode.InvalidInput,
			wantErr:  "must be a SecretRef URI",
		},
		{
			name: "rejects bare --token",
			args: []string{
				"add", "pat-h",
				"--default-flow", "pat",
				"--token", "quay_raw_pat_value",
			},
			wantCode: exitcode.InvalidInput,
			wantErr:  "must be a SecretRef URI",
		},
		{
			name: "rejects --client-secret with --client-secret-stdin",
			args: []string{
				"add", "quay",
				"--client-secret", "env://SECRET",
				"--client-secret-stdin",
			},
			wantCode: exitcode.InvalidInput,
			wantErr:  "mutually exclusive",
		},
		{
			name:     "rejects missing handler name without --from-file",
			args:     []string{"add", "--token-url", "https://idp.example.com/token"},
			wantCode: exitcode.InvalidInput,
			wantErr:  "handler name is required",
		},
		{
			name:     "rejects missing --from-file with FileNotFound",
			args:     []string{"add", "--from-file", filepath.Join(t.TempDir(), "does-not-exist.yaml")},
			wantCode: exitcode.FileNotFound,
			wantErr:  "reading handler file",
		},
		{
			name: "rejects official handler collision even with --force",
			args: []string{
				"add", "github",
				"--authorize-url", "https://github.com/login/oauth/authorize",
				"--token-url", "https://github.com/login/oauth/access_token",
				"--client-id", "cid",
				"--force",
			},
			wantCode: exitcode.InvalidInput,
			wantErr:  "official auth handler",
		},
		{
			name: "rejects registered builtin/plugin handler collision even with --force",
			args: []string{
				"add", "builtin-sso",
				"--authorize-url", "https://sso.example.com/authorize",
				"--token-url", "https://sso.example.com/token",
				"--client-id", "cid",
				"--force",
			},
			wantCode: exitcode.InvalidInput,
			wantErr:  "built-in or plugin auth handler",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, stderr, err := executeOAuth2RootCmd(t, ctx, cliParams, configPath, "", tc.args...)
			require.Error(t, err)
			assert.Equal(t, tc.wantCode, exitcode.GetCode(err))
			assert.Contains(t, stderr, tc.wantErr)
		})
	}
}

func TestOAuth2ListSchema_ValidAndAlignedWithSanitizedStruct(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	require.NoError(t, json.Unmarshal(oauth2ListSchemaJSON, &schema))

	hints, ds, err := tui.ParseSchemaWithDisplay(oauth2ListSchemaJSON)
	require.NoError(t, err)
	assert.NotNil(t, hints)
	require.NotNil(t, ds)
	require.NotNil(t, ds.List)
	assert.Equal(t, "name", ds.List.TitleField)

	itemsObj, ok := schema["items"].(map[string]any)
	require.True(t, ok)
	props, ok := itemsObj["properties"].(map[string]any)
	require.True(t, ok)

	// Every SanitizedCustomOAuth2 json field must exist in both oauth2_list_schema.json
	// and oauth2ListColumnHints so no field is accidentally uncapped or unmapped.
	rt := reflect.TypeFor[appconfig.SanitizedCustomOAuth2]()
	for i := range rt.NumField() {
		jsonTag := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
		if jsonTag == "" || jsonTag == "-" {
			continue
		}
		_, inSchema := props[jsonTag]
		assert.True(t, inSchema, "oauth2_list_schema.json missing property %q", jsonTag)
		_, inHints := oauth2ListColumnHints[jsonTag]
		assert.True(t, inHints, "oauth2ListColumnHints missing entry for %q", jsonTag)
	}
}

func TestCommandOAuth2List_TUISnapshotAndTable(t *testing.T) {
	t.Parallel()

	items := []appconfig.SanitizedCustomOAuth2{
		{
			Name:         "quay",
			DisplayName:  "Quay.io",
			AuthorizeURL: "https://quay.io/oauth/authorize",
			ClientID:     "quay-app",
			DefaultFlow:  "interactive",
			ResponseType: "token",
			Registry:     "quay.io",
		},
		{
			Name:         "deploy-api",
			DisplayName:  "Deploy API",
			TokenURL:     "https://idp.example.com/oauth2/token",
			ClientID:     "deployer",
			ClientSecret: appconfig.RedactedValue,
			DefaultFlow:  "client_credentials",
		},
	}

	out, err := kvx.Snapshot(items,
		kvx.WithDisplaySchemaJSON(oauth2ListSchemaJSON),
		kvx.WithColumnHints(oauth2ListColumnHints),
		kvx.WithColumnarMode(tui.ColumnarModeAlways),
		kvx.WithDimensions(120, 30),
		kvx.WithNoColor(true),
		kvx.WithAppName("scafctl auth oauth2 list"),
	)
	require.NoError(t, err)
	assert.Contains(t, out, "quay")
	assert.Contains(t, out, "deploy-api")
	assert.Contains(t, out, "interactive")
	assert.Contains(t, out, "client_credentials")

	// Also verify piped text table rendering with heterogeneous omitempty fields
	// renders multi-column rows rather than a collapsed JSON value blob.
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	kvxFlags := flags.KvxOutputFlags{Output: string(kvx.OutputFormatText)}
	outOpts := flags.ToKvxOutputOptions(&kvxFlags,
		kvx.WithIOStreams(ioStreams),
		kvx.WithOutputColumnarMode(tui.ColumnarModeAlways),
		kvx.WithOutputColumnOrder(oauth2ListColumnOrder),
		kvx.WithOutputDisplaySchemaJSON(oauth2ListSchemaJSON),
		kvx.WithOutputColumnHints(oauth2ListColumnHints),
	)
	require.NoError(t, outOpts.Write(items))
	tableText := stdout.String()
	assert.Contains(t, tableText, "quay")
	assert.Contains(t, tableText, "deploy-api")
	assert.Contains(t, tableText, "https://idp.example.com/oauth2/token")
}
