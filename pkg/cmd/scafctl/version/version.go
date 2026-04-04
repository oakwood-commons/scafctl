// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package version //nolint:revive // intentional name matching the command

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/oakwood-commons/scafctl/pkg/terminal/styles"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

var ValidOutputTypes = []string{"json", "yaml"}

type GetLatestVersionFunc func(ctx context.Context) (string, error)

type CmdOptionsVersion struct {
	IOStreams        *terminal.IOStreams
	CliParams        *settings.Run
	Output           string
	GetLatestVersion GetLatestVersionFunc
	BinaryName       string
	VersionExtra     *settings.VersionInfo
}

func CommandVersion(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string, versionExtra *settings.VersionInfo) *cobra.Command {
	options := &CmdOptionsVersion{}
	cCmd := &cobra.Command{
		Use:     "version",
		Aliases: []string{"v"},
		Short:   fmt.Sprintf("Prints the %s version", path),
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(cCmd.Context(), cliParams)

			lgr := logger.FromContext(ctx)
			lgr.V(3).Info("Version command invoked")

			options.IOStreams = ioStreams
			options.CliParams = cliParams
			options.BinaryName = path
			options.VersionExtra = versionExtra

			// Get writer from parent context or create new one
			w := writer.FromContext(cCmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx = writer.WithWriter(ctx, w)

			err := output.ValidateCommands(args)
			if err != nil {
				w.Error(err.Error())
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			err = output.ValidateOutputType(options.Output, ValidOutputTypes)
			if err != nil {
				w.Error(err.Error())
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
			return options.PrintVersion(ctx)
		},
		SilenceUsage: true,
	}
	cCmd.PersistentFlags().StringVarP(&options.Output, "output", "o", "", fmt.Sprintf("Output format. One of: (%s)", strings.Join(ValidOutputTypes, ", ")))
	return cCmd
}

func (options *CmdOptionsVersion) PrintVersion(ctx context.Context) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)
	if options.GetLatestVersion == nil {
		options.GetLatestVersion = GetLatestVersion
	}
	latestVersion, err := options.GetLatestVersion(ctx)
	if err != nil {
		if w != nil {
			w.Warning(err.Error())
		}
		latestVersion = "<unable to determine>"
	}
	lgr = logger.WithValues(lgr, "latest_version", latestVersion, "current_version", settings.VersionInformation.BuildVersion)
	outOfDate := false
	if latestVersion != "" && latestVersion != "<unable to determine>" && settings.VersionInformation.BuildVersion != "" {
		l, err := semver.NewVersion(latestVersion)
		if err == nil {
			c, err := semver.NewVersion(settings.VersionInformation.BuildVersion)
			if err == nil {
				outOfDate = l.GreaterThan(c)
			} else {
				lgr.V(1).Info(fmt.Sprintf("unable to parse current version: %v", err))
			}
		} else {
			lgr.V(1).Info(fmt.Sprintf("unable to parse latest version: %v", err))
		}
	}
	if outOfDate {
		lgr.V(0).Info(fmt.Sprintf("A newer version of %s is available. Please consider updating to the latest version.", options.BinaryName))
	}
	verDetails := newVersionDetails(latestVersion, options.BinaryName, options.VersionExtra)
	customOutputFn := func(_ *terminal.IOStreams, data map[string]any) error {
		if w != nil {
			if options.VersionExtra != nil {
				w.Plainf("\n%s %s version %s\n", styles.SuccessStyle.Render("Embedder:"), options.BinaryName, options.VersionExtra.BuildVersion)
				w.Plainf("%s  %s\n", styles.SuccessStyle.Render("  Commit:"), options.VersionExtra.Commit)
				w.Plainf("%s  %s\n\n", styles.SuccessStyle.Render("  Built:"), options.VersionExtra.BuildTime)
			}
			w.Plainf("%s        %s\n", styles.SuccessStyle.Render("Version:"), settings.VersionInformation.BuildVersion)
			w.Plainf("%s %s\n", styles.SuccessStyle.Render("Latest Version:"), data["latestVersion"])
			w.Plainf("%s         %s\n", styles.SuccessStyle.Render("Commit:"), settings.VersionInformation.Commit)
			w.Plainf("%s     %s\n\n", styles.SuccessStyle.Render("Build Time:"), settings.VersionInformation.BuildTime)
		}
		return nil
	}
	err = output.WriteOutput(options.IOStreams, options.Output, verDetails, customOutputFn)
	if err != nil {
		if w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	return nil
}

func GetLatestVersion(ctx context.Context) (string, error) {
	const url = "https://api.github.com/repos/oakwood-commons/scafctl/releases/latest"

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	client := httpc.NewClient(&httpc.ClientConfig{
		Timeout:           5 * time.Second,
		RetryMax:          1,
		RetryWaitMin:      500 * time.Millisecond,
		RetryWaitMax:      2 * time.Second,
		EnableCache:       false,
		EnableCompression: true,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	return strings.TrimPrefix(release.TagName, "v"), nil
}

func newVersionDetails(latestVersion, binaryName string, versionExtra *settings.VersionInfo) map[string]any {
	details := map[string]any{
		"version":        settings.VersionInformation.BuildVersion,
		"commit":         settings.VersionInformation.Commit,
		"buildTime":      settings.VersionInformation.BuildTime,
		"latestVersion":  latestVersion,
		"updateRequired": (settings.VersionInformation.BuildVersion != latestVersion),
	}
	if versionExtra != nil {
		details["embedder"] = map[string]any{
			"name":      binaryName,
			"version":   versionExtra.BuildVersion,
			"commit":    versionExtra.Commit,
			"buildTime": versionExtra.BuildTime,
		}
	}
	return details
}
