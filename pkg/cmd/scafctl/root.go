package scafctl

import (
	"context"
	"fmt"
	"os"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/kcloutie/scafctl/pkg/cmd/scafctl/get"
	"github.com/kcloutie/scafctl/pkg/cmd/scafctl/version"
	"github.com/kcloutie/scafctl/pkg/logger"
	"github.com/kcloutie/scafctl/pkg/profiler"
	"github.com/kcloutie/scafctl/pkg/settings"
	"github.com/kcloutie/scafctl/pkg/terminal"
	"github.com/kcloutie/scafctl/pkg/terminal/output"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var cliParams = settings.NewCliParams()

// Root creates and returns the root cobra.Command for the scafctl CLI tool.
// It sets up persistent flags, configures logging, handles profiler options,
// validates command arguments, and adds subcommands. The command provides
// configuration discovery and scaffolding functionality.
func Root() *cobra.Command {
	cCmd := &cobra.Command{
		Use:   "scafctl",
		Short: "A configuration discovery and scaffolding tool",
		Long: heredoc.Doc(`
			A configuration discovery and scaffolding tool
		`),
		SilenceUsage: false,
		PersistentPreRun: func(cCmd *cobra.Command, args []string) {
			lgr := logger.Get(cliParams.MinLogLevel * -1)
			cCmd.SetContext(logger.WithLogger(context.Background(), lgr))
			ioStreams := terminal.NewIOStreams(os.Stdin, os.Stdout, os.Stderr, true)

			err := output.ValidateCommands(args)
			if err != nil {
				output.NewWriteMessageOptions(
					ioStreams,
					output.MessageTypeError,
					cliParams.NoColor,
					cliParams.ExitOnError,
				).WriteMessage(err.Error())
				return
			}

			if cCmd.Flags().Changed("pprof") {
				profileType, _ := cCmd.Flags().GetString("pprof")
				profilePath, _ := cCmd.Flags().GetString("pprof-output-dir")
				p, err := profiler.GetProfiler(profileType, profilePath, lgr)
				if err != nil {
					output.NewWriteMessageOptions(
						ioStreams,
						output.MessageTypeError,
						cliParams.NoColor,
						cliParams.ExitOnError,
					).WriteMessage(fmt.Sprintf("Error starting profiler: %v", err))
					return
				}

				go func() {
					e := p.Start(lgr)
					if e != nil {
						lgr.V(1).Info("Error starting profiler", zap.Error(e))

						output.NewWriteMessageOptions(
							ioStreams,
							output.MessageTypeError,
							cliParams.NoColor,
							cliParams.ExitOnError,
						).WriteMessage(fmt.Sprintf("Error starting profiler: %v", e))
						return
					}
				}()
			}
		},
		Annotations: map[string]string{
			"commandType": "main",
		},
	}

	ioStreams := terminal.NewIOStreams(os.Stdin, os.Stdout, os.Stderr, true)

	cCmd.PersistentFlags().Int8Var(&cliParams.MinLogLevel, "log-level", 0, "Set the minimum log level (-1=Debug, 0=Info, 1=Warn, 2=Error)")
	cCmd.PersistentFlags().BoolVarP(&cliParams.IsQuiet, "quiet", "q", false, "Do not print additional information")
	cCmd.PersistentFlags().BoolVar(&cliParams.NoColor, "no-color", false, "Disable color output")
	cCmd.PersistentFlags().String("pprof", "", "Enable profiling (options: memory, cpu)")
	cCmd.PersistentFlags().String("pprof-output-dir", "./", "directory path to save the profiler.prof file (default: current working directory)")

	if err := cCmd.PersistentFlags().MarkHidden("pprof"); err != nil {
		return nil
	}
	if err := cCmd.PersistentFlags().MarkHidden("pprof-output-dir"); err != nil {
		return nil
	}
	cCmd.AddCommand(version.CommandVersion(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(get.CommandGet(cliParams, ioStreams, settings.CliBinaryName))
	return cCmd
}
