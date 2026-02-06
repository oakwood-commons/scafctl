package solution

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/spf13/cobra"
)

var ValidOutputTypes = []string{"json", "yaml"}

type GetLatestVersionFunc func(ctx context.Context) (string, error)

type CmdOptionsVersion struct {
	IOStreams *terminal.IOStreams
	CliParams *settings.Run
	Output    string
	Path      string
}

func CommandSolution(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &CmdOptionsVersion{}
	cCmd := &cobra.Command{
		Use:     "solution",
		Aliases: []string{"sol", "SOL", "Solution", "solutions"},
		Short:   fmt.Sprintf("Gets %s solutions", settings.CliBinaryName),
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)

			lgr := logger.FromContext(ctx)
			lgr.V(3).Info("Solution command invoked")

			options.IOStreams = ioStreams
			options.CliParams = cliParams

			err := output.ValidateCommands(args)
			if err != nil {
				output.NewWriteMessageOptions(
					options.IOStreams,
					output.MessageTypeError,
					options.CliParams.NoColor,
					options.CliParams.ExitOnError,
				).WriteMessage(err.Error())

				return err
			}

			err = output.ValidateOutputType(options.Output, ValidOutputTypes)
			if err != nil {
				output.NewWriteMessageOptions(
					options.IOStreams,
					output.MessageTypeError,
					options.CliParams.NoColor,
					options.CliParams.ExitOnError,
				).WriteMessage(err.Error())

				return err
			}
			return options.GetSolution(ctx)
		},
		SilenceUsage: true,
	}
	cCmd.PersistentFlags().StringVarP(&options.Output, "output", "o", "", fmt.Sprintf("Output format. One of: (%s)", strings.Join(ValidOutputTypes, ", ")))
	cCmd.PersistentFlags().StringVarP(&options.Path, "path", "p", "", "Path to the solution. This can be a local file path or a URL. If not provided, the command will attempt to locate a solution file in default locations.")
	return cCmd
}

func (o *CmdOptionsVersion) GetSolution(ctx context.Context) error {
	lgr := logger.FromContext(ctx)

	// Set up getter with catalog resolver for bare name resolution
	var getterOpts []get.Option
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err == nil {
		resolver := catalog.NewSolutionResolver(localCatalog, *lgr)
		getterOpts = append(getterOpts, get.WithCatalogResolver(resolver))
	} else {
		lgr.V(1).Info("catalog not available for solution resolution", "error", err)
	}

	getter := get.NewGetter(getterOpts...)
	return o.GetSolutionWithGetter(ctx, getter)
}

// GetSolutionWithGetter retrieves a solution using the provided getter implementation.
// This method allows for dependency injection, making it easier to test with mock implementations.
// The getter parameter should implement the get.Interface.
func (o *CmdOptionsVersion) GetSolutionWithGetter(ctx context.Context, getter get.Interface) error {
	sol, err := getter.Get(ctx, o.Path)
	if err != nil {
		return err
	}

	err = output.WriteOutput(o.IOStreams, o.Output, sol, nil)
	if err != nil {
		return err
	}
	return nil
}
