// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/kvx/pkg/tui"
	customoauth2 "github.com/oakwood-commons/scafctl/pkg/auth/oauth2"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

//go:embed oauth2_list_schema.json
var oauth2ListSchemaJSON []byte

// oauth2ListColumnOrder defines the visible columns for non-interactive table output.
var oauth2ListColumnOrder = []string{"name", "displayName", "defaultFlow", "tokenURL", "registry"}

// oauth2ListColumnHints caps every visible column so responsive tables fit standard
// terminals (~90-100+ cols) and hides secondary fields (still available in -o json/yaml and -i).
var oauth2ListColumnHints = map[string]tui.ColumnHint{
	"name":                      {MaxWidth: 16, Priority: 10, DisplayName: "Name"},
	"displayName":               {MaxWidth: 20, Priority: 8, DisplayName: "Display Name"},
	"defaultFlow":               {MaxWidth: 18, Priority: 9, DisplayName: "Default Flow"},
	"tokenURL":                  {MaxWidth: 26, Flex: true, Priority: 7, DisplayName: "Token URL"},
	"registry":                  {MaxWidth: 20, Priority: 6, DisplayName: "Registry"},
	"authorizeURL":              {Hidden: true},
	"deviceAuthURL":             {Hidden: true},
	"clientID":                  {Hidden: true},
	"clientSecret":              {Hidden: true},
	"federatedToken":            {Hidden: true},
	"federatedTokenFile":        {Hidden: true},
	"audience":                  {Hidden: true},
	"workloadIdentityGrantType": {Hidden: true},
	"subjectTokenType":          {Hidden: true},
	"token":                     {Hidden: true},
	"username":                  {Hidden: true},
	"scopes":                    {Hidden: true},
	"callbackPort":              {Hidden: true},
	"callbackPath":              {Hidden: true},
	"callbackHost":              {Hidden: true},
	"deviceCodePollInterval":    {Hidden: true},
	"disablePKCE":               {Hidden: true},
	"responseType":              {Hidden: true},
	"verifyURL":                 {Hidden: true},
	"identityFields":            {Hidden: true},
	"registryUsername":          {Hidden: true},
	"tokenExchange":             {Hidden: true},
	"dynamicClientRegistration": {Hidden: true},
}

func resolveBinaryName(cliParams *settings.Run) string {
	if cliParams != nil && cliParams.BinaryName != "" {
		return cliParams.BinaryName
	}
	return settings.CliBinaryName
}

// CommandOAuth2 creates the 'auth oauth2' command group for managing custom OAuth2 handlers.
func CommandOAuth2(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	binName := resolveBinaryName(cliParams)

	cmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:          "oauth2",
		Short:        "Manage custom OAuth2 authentication handlers",
		SilenceUsage: true,
		Long: heredoc.Docf(`
			Manage custom OAuth2 authentication handlers in application configuration
			(auth.customOAuth2).

			Custom OAuth2 handlers let %[1]s authenticate against any OAuth2-capable
			service, API, or OCI container registry using interactive browser login,
			device code, client credentials, workload identity federation, or static
			personal/robot tokens.

			Note: '%[1]s auth handlers' inspects all registered and catalog-available
			handlers and manages cached plugin binaries, whereas '%[1]s auth oauth2'
			manages auth.customOAuth2 entries in config.yaml.

			Examples:
			  # Add an interactive OAuth2 handler
			  %[1]s auth oauth2 add my-idp \
			    --authorize-url https://idp.example.com/oauth2/authorize \
			    --token-url https://idp.example.com/oauth2/token \
			    --client-id my-cli-client \
			    --default-flow interactive

			  # Add a client-credentials handler reading the secret from stdin
			  printf '%%s' "$CLIENT_SECRET" | %[1]s auth oauth2 add ci-service \
			    --token-url https://idp.example.com/oauth2/token \
			    --client-id ci-client \
			    --default-flow client_credentials \
			    --client-secret-stdin

			  # List configured custom OAuth2 handlers
			  %[1]s auth oauth2 list

			  # Remove a custom OAuth2 handler
			  %[1]s auth oauth2 remove my-idp
		`, binName),
	})

	cmdPath := fmt.Sprintf("%s/%s", path, cmd.Use)
	cmd.AddCommand(commandOAuth2Add(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(commandOAuth2Remove(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(commandOAuth2List(cliParams, ioStreams, cmdPath))

	return cmd
}

// OAuth2AddOptions holds CLI flag values for 'auth oauth2 add'.
type OAuth2AddOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string

	Name                      string
	FromFile                  string
	DisplayName               string
	AuthorizeURL              string
	TokenURL                  string
	DeviceAuthURL             string
	ClientID                  string
	ClientSecret              string //nolint:gosec // G117: holds SecretRef URI flag value, bare secrets are rejected
	ClientSecretStdin         bool
	FederatedToken            string //nolint:gosec // G101: holds SecretRef URI flag value, bare secrets are rejected
	FederatedTokenFile        string //nolint:gosec // G101: projected token file path
	Audience                  string
	WorkloadIdentityGrantType string
	SubjectTokenType          string
	Token                     string //nolint:gosec // G101: holds SecretRef URI flag value, bare secrets are rejected
	Username                  string
	Scopes                    []string
	DefaultFlow               string
	CallbackPort              int
	CallbackPath              string
	CallbackHost              string
	DeviceCodePollInterval    int
	DisablePKCE               bool
	ResponseType              string
	VerifyURL                 string
	Registry                  string
	RegistryUsername          string
	Force                     bool
}

func commandOAuth2Add(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &OAuth2AddOptions{
		IOStreams: ioStreams,
		CliParams: cliParams,
	}
	binName := resolveBinaryName(cliParams)

	cmd := &cobra.Command{
		Use:   "add [name]",
		Short: "Add or update a custom OAuth2 auth handler",
		Long: heredoc.Docf(`
			Add a custom OAuth2 authentication handler to auth.customOAuth2 in config.yaml.

			Handlers can be defined via CLI flags or loaded from a YAML/JSON file with
			--from-file (required for nested blocks such as tokenExchange,
			dynamicClientRegistration, or identityFields). When both --from-file and
			explicit flags are supplied, the flags override the file's fields.

			Security: plaintext secrets are never accepted on CLI flags. Pass a SecretRef
			URI (secret://<name>, env://<VAR>, or file://<path>) to --client-secret,
			--federated-token, or --token, or pipe a client secret via --client-secret-stdin
			(stored encrypted as secret://<name>-client-secret).

			Examples:
			  # Add an interactive OAuth2 handler (implicit grant)
			  %[1]s auth oauth2 add quay \
			    --display-name "Quay.io" \
			    --authorize-url https://quay.io/oauth/authorize \
			    --client-id my-quay-app \
			    --default-flow interactive \
			    --response-type token \
			    --callback-port 8400 \
			    --scopes repo:read \
			    --registry quay.io \
			    --registry-username '$oauthtoken'

			  # Add a workload identity handler
			  %[1]s auth oauth2 add deploy-api \
			    --token-url https://idp.example.com/oauth2/token \
			    --client-id deployer \
			    --federated-token file:///var/run/secrets/tokens/oidc \
			    --audience api://deploy-service \
			    --default-flow workload_identity

			  # Add a static robot token handler
			  %[1]s auth oauth2 add quay-ci \
			    --default-flow pat \
			    --token env://QUAY_ROBOT_TOKEN \
			    --username acme+ci_robot \
			    --registry quay.io

			  # Load a handler with tokenExchange or DCR from YAML
			  %[1]s auth oauth2 add --from-file ./handler.yaml
		`, binName),
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Name = ""
			if len(args) > 0 {
				opts.Name = args[0]
			}
			opts.ConfigPath = configPathFromCmd(cmd)
			return runOAuth2Add(cmd.Context(), opts, cmd.Flags())
		},
	}

	cmd.Flags().StringVar(&opts.FromFile, "from-file", "", "Load handler configuration from a YAML or JSON file")
	cmd.Flags().StringVar(&opts.DisplayName, "display-name", "", "Human-readable display name")
	cmd.Flags().StringVar(&opts.AuthorizeURL, "authorize-url", "", "OAuth2 authorization endpoint URL")
	cmd.Flags().StringVar(&opts.TokenURL, "token-url", "", "OAuth2 token endpoint URL")
	cmd.Flags().StringVar(&opts.DeviceAuthURL, "device-auth-url", "", "OAuth2 device authorization endpoint URL")
	cmd.Flags().StringVar(&opts.ClientID, "client-id", "", "OAuth2 client ID")
	cmd.Flags().StringVar(&opts.ClientSecret, "client-secret", "", "OAuth2 client secret SecretRef URI (secret://, env://, or file://)")
	cmd.Flags().BoolVar(&opts.ClientSecretStdin, "client-secret-stdin", false, "Read OAuth2 client secret from stdin and store as secret://<name>-client-secret")
	cmd.Flags().StringVar(&opts.FederatedToken, "federated-token", "", "Workload identity JWT SecretRef URI (secret://, env://, or file://)")
	cmd.Flags().StringVar(&opts.FederatedTokenFile, "federated-token-file", "", "Path to projected workload identity JWT file")
	cmd.Flags().StringVar(&opts.Audience, "audience", "", "Target audience for workload identity token exchange")
	cmd.Flags().StringVar(&opts.WorkloadIdentityGrantType, "workload-identity-grant-type", "", "Workload identity grant mode (client_credentials or token_exchange)")
	cmd.Flags().StringVar(&opts.SubjectTokenType, "subject-token-type", "", "RFC 8693 subject_token_type URI for token_exchange mode")
	cmd.Flags().StringVar(&opts.Token, "token", "", "Static/robot bearer token SecretRef URI (secret://, env://, or file://)")
	cmd.Flags().StringVar(&opts.Username, "username", "", "Static/robot account username (literal or SecretRef URI)")
	cmd.Flags().StringSliceVar(&opts.Scopes, "scopes", nil, "Default OAuth2 scopes (comma-separated or repeatable)")
	cmd.Flags().StringVar(&opts.DefaultFlow, "default-flow", "", "Default OAuth2 flow (interactive, device_code, client_credentials, workload_identity, pat)")
	cmd.Flags().IntVar(&opts.CallbackPort, "callback-port", 0, "Local callback port for interactive flow (0 = random, or 1024-65535)")
	cmd.Flags().StringVar(&opts.CallbackPath, "callback-path", "", "Callback URL path for interactive flow (default: /)")
	cmd.Flags().StringVar(&opts.CallbackHost, "callback-host", "", "Callback host for interactive flow (localhost, 127.0.0.1, ::1)")
	cmd.Flags().IntVar(&opts.DeviceCodePollInterval, "device-code-poll-interval", 0, "Polling interval in seconds for device_code flow (0 = server default)")
	cmd.Flags().BoolVar(&opts.DisablePKCE, "disable-pkce", false, "Disable PKCE for servers that reject code_challenge parameters")
	cmd.Flags().StringVar(&opts.ResponseType, "response-type", "", "OAuth2 authorization response type (code or token)")
	cmd.Flags().StringVar(&opts.VerifyURL, "verify-url", "", "Optional token verification endpoint URL")
	cmd.Flags().StringVar(&opts.Registry, "registry", "", "Optional OCI registry host for automatic credential bridging")
	cmd.Flags().StringVar(&opts.RegistryUsername, "registry-username", "", "Optional OCI registry username convention")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Overwrite an existing custom OAuth2 handler with the same name")

	return cmd
}

func runOAuth2Add(ctx context.Context, opts *OAuth2AddOptions, flagSet *pflag.FlagSet) error {
	w := writer.FromContext(ctx)
	binName := resolveBinaryName(opts.CliParams)
	if existingRun, ok := settings.FromContext(ctx); !ok || existingRun == nil || existingRun.BinaryName == "" {
		runCopy := settings.Run{BinaryName: binName}
		if opts.CliParams != nil {
			runCopy = *opts.CliParams
			runCopy.BinaryName = binName
		}
		ctx = settings.IntoContext(ctx, &runCopy)
	}

	if opts.ClientSecret != "" && opts.ClientSecretStdin {
		err := fmt.Errorf("--client-secret and --client-secret-stdin are mutually exclusive")
		emitAuthCmdError(w, err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Enforce SecretRef URI syntax on secret-bearing CLI argv flags before any file or store I/O.
	for _, check := range []struct {
		flag  string
		val   string
		stdin bool
	}{
		{flag: "client-secret", val: opts.ClientSecret, stdin: true},
		{flag: "federated-token", val: opts.FederatedToken, stdin: false},
		{flag: "token", val: opts.Token, stdin: false},
	} {
		if err := customoauth2.ValidateFlagSecretRef(check.flag, check.val, check.stdin, binName); err != nil {
			emitAuthCmdError(w, err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
	}

	var entry appconfig.CustomOAuth2Config
	if opts.FromFile != "" {
		loaded, err := customoauth2.LoadConfigFile(opts.FromFile)
		if err != nil {
			emitAuthCmdError(w, err)
			if errors.Is(err, os.ErrNotExist) {
				return exitcode.WithCode(err, exitcode.FileNotFound)
			}
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
		entry = loaded
	}

	applyOAuth2AddFlagOverrides(&entry, opts, flagSet)

	if entry.Name == "" {
		err := fmt.Errorf("handler name is required: pass <name> as an argument or set 'name' in --from-file")
		emitAuthCmdError(w, err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	var stdinReader *terminal.IOStreams
	if opts.IOStreams != nil {
		stdinReader = opts.IOStreams
	}

	officialReg := authofficial.RegistryFromContext(ctx)
	if officialReg == nil {
		officialReg = authofficial.NewRegistry()
	}

	addOpts := customoauth2.AddOptions{
		ConfigPath:            opts.ConfigPath,
		BinaryName:            binName,
		Entry:                 entry,
		Force:                 opts.Force,
		ReadClientSecretStdin: opts.ClientSecretStdin,
		OfficialHandlers:      officialReg,
	}
	if stdinReader != nil {
		addOpts.ClientSecretReader = stdinReader.In
	}

	res, err := customoauth2.AddCustomOAuth2(ctx, addOpts)
	if err != nil {
		emitAuthCmdError(w, err)
		return exitcode.WithCode(err, mapOAuth2ManageExitCode(err))
	}

	if w != nil {
		if opts.FromFile != "" && !opts.ClientSecretStdin && entry.ClientSecret != "" && !appconfig.IsSecretRef(entry.ClientSecret) {
			w.WarnStderrf("clientSecret in %q uses a literal value; prefer secret://, env://, or file:// to keep secrets out of config.yaml", opts.FromFile)
		}
		if opts.FromFile != "" && entry.DynamicClientRegistration != nil && entry.DynamicClientRegistration.InitialAccessToken != "" && !appconfig.IsSecretRef(entry.DynamicClientRegistration.InitialAccessToken) {
			w.WarnStderrf("dynamicClientRegistration.initialAccessToken in %q uses a literal value; prefer secret://, env://, or file:// to keep secrets out of config.yaml", opts.FromFile)
		}
		if res.Updated {
			w.Successf("Updated custom OAuth2 handler %q", res.Name)
		} else {
			w.Successf("Added custom OAuth2 handler %q", res.Name)
		}
		if res.StoredSecretName != "" {
			w.Infof("Stored client secret as secret://%s", res.StoredSecretName)
		}
	}

	return nil
}

func applyOAuth2AddFlagOverrides(entry *appconfig.CustomOAuth2Config, opts *OAuth2AddOptions, flagSet *pflag.FlagSet) {
	isSet := func(flagName string, nonZero bool) bool {
		if flagSet != nil {
			return flagSet.Changed(flagName)
		}
		return nonZero
	}

	if opts.Name != "" {
		entry.Name = opts.Name
	}
	if isSet("display-name", opts.DisplayName != "") {
		entry.DisplayName = opts.DisplayName
	}
	if isSet("authorize-url", opts.AuthorizeURL != "") {
		entry.AuthorizeURL = opts.AuthorizeURL
	}
	if isSet("token-url", opts.TokenURL != "") {
		entry.TokenURL = opts.TokenURL
	}
	if isSet("device-auth-url", opts.DeviceAuthURL != "") {
		entry.DeviceAuthURL = opts.DeviceAuthURL
	}
	if isSet("client-id", opts.ClientID != "") {
		entry.ClientID = opts.ClientID
	}
	if isSet("client-secret", opts.ClientSecret != "") {
		entry.ClientSecret = opts.ClientSecret
	}
	fedTokenSet := isSet("federated-token", opts.FederatedToken != "")
	fedTokenFileSet := isSet("federated-token-file", opts.FederatedTokenFile != "")
	if fedTokenSet {
		entry.FederatedToken = appconfig.SecretRef(opts.FederatedToken)
		if opts.FromFile != "" && !fedTokenFileSet && opts.FederatedToken != "" {
			entry.FederatedTokenFile = ""
		}
	}
	if fedTokenFileSet {
		entry.FederatedTokenFile = opts.FederatedTokenFile
		if opts.FromFile != "" && !fedTokenSet && opts.FederatedTokenFile != "" {
			entry.FederatedToken = ""
		}
	}
	if isSet("audience", opts.Audience != "") {
		entry.Audience = opts.Audience
	}
	if isSet("workload-identity-grant-type", opts.WorkloadIdentityGrantType != "") {
		entry.WorkloadIdentityGrantType = opts.WorkloadIdentityGrantType
	}
	if isSet("subject-token-type", opts.SubjectTokenType != "") {
		entry.SubjectTokenType = opts.SubjectTokenType
	}
	if isSet("token", opts.Token != "") {
		entry.Token = appconfig.SecretRef(opts.Token)
	}
	if isSet("username", opts.Username != "") {
		entry.Username = opts.Username
	}
	if isSet("scopes", len(opts.Scopes) > 0) {
		entry.Scopes = opts.Scopes
	}
	if isSet("default-flow", opts.DefaultFlow != "") {
		entry.DefaultFlow = opts.DefaultFlow
	}
	if isSet("callback-port", opts.CallbackPort != 0) {
		entry.CallbackPort = opts.CallbackPort
	}
	if isSet("callback-path", opts.CallbackPath != "") {
		entry.CallbackPath = opts.CallbackPath
	}
	if isSet("callback-host", opts.CallbackHost != "") {
		entry.CallbackHost = opts.CallbackHost
	}
	if isSet("device-code-poll-interval", opts.DeviceCodePollInterval != 0) {
		entry.DeviceCodePollInterval = opts.DeviceCodePollInterval
	}
	if isSet("disable-pkce", opts.DisablePKCE) {
		entry.DisablePKCE = opts.DisablePKCE
	}
	if isSet("response-type", opts.ResponseType != "") {
		entry.ResponseType = opts.ResponseType
	}
	if isSet("verify-url", opts.VerifyURL != "") {
		entry.VerifyURL = opts.VerifyURL
	}
	if isSet("registry", opts.Registry != "") {
		entry.Registry = opts.Registry
	}
	if isSet("registry-username", opts.RegistryUsername != "") {
		entry.RegistryUsername = opts.RegistryUsername
	}
}

// --- oauth2 remove ---

// OAuth2RemoveOptions holds options for 'auth oauth2 remove'.
type OAuth2RemoveOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string
	Name       string
}

func commandOAuth2Remove(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &OAuth2RemoveOptions{
		IOStreams: ioStreams,
		CliParams: cliParams,
	}
	binName := resolveBinaryName(cliParams)

	return &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a custom OAuth2 auth handler",
		Long: heredoc.Docf(`
			Remove a custom OAuth2 authentication handler from auth.customOAuth2 in config.yaml.

			Note: cached tokens or secrets stored in '%[1]s secrets' are not deleted
			automatically; run '%[1]s auth logout <name>' before removing if needed.

			Example:
			  %[1]s auth oauth2 remove quay
		`, binName),
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			items, err := customoauth2.ListCustomOAuth2(cmd.Context(), configPathFromCmd(cmd))
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			names := make([]string, 0, len(items))
			for _, it := range items {
				names = append(names, it.Name)
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Name = args[0]
			opts.ConfigPath = configPathFromCmd(cmd)
			return runOAuth2Remove(cmd.Context(), opts)
		},
	}
}

func runOAuth2Remove(ctx context.Context, opts *OAuth2RemoveOptions) error {
	w := writer.FromContext(ctx)

	res, err := customoauth2.RemoveCustomOAuth2WithResult(ctx, opts.ConfigPath, opts.Name)
	if err != nil {
		if errors.Is(err, customoauth2.ErrHandlerNotFound) {
			notFoundErr := fmt.Errorf("custom OAuth2 handler %q not found in configuration", opts.Name)
			emitAuthCmdError(w, notFoundErr)
			return exitcode.WithCode(notFoundErr, exitcode.FileNotFound)
		}
		emitAuthCmdError(w, err)
		return exitcode.WithCode(err, mapOAuth2ManageExitCode(err))
	}

	if w != nil {
		if res.RestoredLowerLayer {
			w.Successf("Removed user override for custom OAuth2 handler %q", opts.Name)
			w.Infof("Base or config.d definition for %q remains active.", opts.Name)
		} else {
			w.Successf("Removed custom OAuth2 handler %q", opts.Name)
		}
	}
	return nil
}

// --- oauth2 list ---

func commandOAuth2List(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var kvxFlags flags.KvxOutputFlags
	binName := resolveBinaryName(cliParams)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List configured custom OAuth2 auth handlers",
		Long: heredoc.Docf(`
			List all custom OAuth2 authentication handlers configured under
			auth.customOAuth2. Sensitive credential fields are redacted.

			Examples:
			  %[1]s auth oauth2 list
			  %[1]s auth oauth2 list -i
			  %[1]s auth oauth2 list -o json
			  %[1]s auth oauth2 list -o yaml
		`, binName),
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			kvxFlags.AppName = binName + " auth oauth2 list"
			noColor := cliParams != nil && cliParams.NoColor
			outputOpts := flags.ToKvxOutputOptions(&kvxFlags,
				kvx.WithIOStreams(ioStreams),
				kvx.WithOutputContext(cmd.Context()),
				kvx.WithOutputNoColor(noColor),
				kvx.WithOutputAppName(binName+" auth oauth2 list"),
				kvx.WithOutputColumnarMode(tui.ColumnarModeAlways),
				kvx.WithOutputColumnOrder(oauth2ListColumnOrder),
				kvx.WithOutputDisplaySchemaJSON(oauth2ListSchemaJSON),
				kvx.WithOutputColumnHints(oauth2ListColumnHints),
			)
			return runOAuth2List(cmd.Context(), configPathFromCmd(cmd), binName, outputOpts)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &kvxFlags)
	return cmd
}

func runOAuth2List(ctx context.Context, configPath, binName string, outputOpts *kvx.OutputOptions) error {
	w := writer.FromContext(ctx)
	if binName == "" {
		binName = settings.BinaryNameFromContext(ctx)
	}

	items, err := customoauth2.ListCustomOAuth2(ctx, configPath)
	if err != nil {
		emitAuthCmdError(w, err)
		return exitcode.WithCode(err, mapOAuth2ManageExitCode(err))
	}

	structured := kvx.IsStructuredFormat(outputOpts.Format)
	quiet := kvx.IsQuietFormat(outputOpts.Format)

	if len(items) == 0 {
		if structured || quiet || outputOpts.Interactive || outputOpts.Expression != "" || outputOpts.Where != "" {
			return outputOpts.Write(items)
		}
		if w != nil {
			w.WarnStderr("no custom OAuth2 handlers configured")
			w.PlainStderrf("Run '%s auth oauth2 add <name> ...' to configure one.", binName)
		}
		return nil
	}

	return outputOpts.Write(items)
}

func emitAuthCmdError(w *writer.Writer, err error) {
	if w != nil {
		w.Errorf("%v", err)
	}
}

func mapOAuth2ManageExitCode(err error) int {
	var cfgErr *customoauth2.ConfigPersistenceError
	if errors.As(err, &cfgErr) {
		return exitcode.ConfigError
	}
	if errors.Is(err, customoauth2.ErrHandlerNotFound) {
		return exitcode.FileNotFound
	}
	return exitcode.InvalidInput
}
