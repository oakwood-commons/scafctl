package version

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
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
}

func CommandVersion(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &CmdOptionsVersion{}
	cCmd := &cobra.Command{
		Use:     "version",
		Aliases: []string{"v"},
		Short:   fmt.Sprintf("Prints the %s version", settings.CliBinaryName),
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)

			lgr := logger.FromContext(ctx)
			lgr.V(3).Info("Version command invoked")

			options.IOStreams = ioStreams
			options.CliParams = cliParams

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
	if latestVersion != "" && latestVersion != "<unable to determine>" {
		l, err := semver.NewVersion(latestVersion)
		if err == nil {
			c, err := semver.NewVersion(settings.VersionInformation.BuildVersion)
			if err == nil {
				outOfDate = l.GreaterThan(c)
			} else {
				lgr.V(1).Info(fmt.Sprintf("unable to parse latest version: %v", err))
			}
		} else {
			lgr.V(1).Info(fmt.Sprintf("unable to parse latest version: %v", err))
		}
	}
	if outOfDate {
		lgr.V(0).Info("A newer version of scafctl is available. Please consider updating to the latest version.")
	}
	verDetails := newVersionDetails(latestVersion)
	err = output.WriteOutput(options.IOStreams, options.Output, verDetails, customOutput)
	if err != nil {
		if w != nil {
			w.Errorf("%v", err)
		}
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	return nil
}

func GetLatestVersion(_ context.Context) (string, error) {
	// TODO Need to implement getting the latest version
	return "", errors.New("unable to get the latest version, not implemented yet")
}

func customOutput(ioStreams *terminal.IOStreams, data map[string]any) error {
	fmt.Fprintf(ioStreams.Out, "\n%s        %s\n", styles.SuccessStyle.Render("Version:"), settings.VersionInformation.BuildVersion)
	fmt.Fprintf(ioStreams.Out, "%s %s\n", styles.SuccessStyle.Render("Latest Version:"), data["latestVersion"])
	fmt.Fprintf(ioStreams.Out, "%s         %s\n", styles.SuccessStyle.Render("Commit:"), settings.VersionInformation.Commit)
	fmt.Fprintf(ioStreams.Out, "%s     %s\n\n", styles.SuccessStyle.Render("Build Time:"), settings.VersionInformation.BuildTime)
	return nil
}

func newVersionDetails(latestVersion string) map[string]any {
	return map[string]any{
		"version":        settings.VersionInformation.BuildVersion,
		"commit":         settings.VersionInformation.Commit,
		"buildTime":      settings.VersionInformation.BuildTime,
		"latestVersion":  latestVersion,
		"updateRequired": (settings.VersionInformation.BuildVersion != latestVersion),
	}
}
