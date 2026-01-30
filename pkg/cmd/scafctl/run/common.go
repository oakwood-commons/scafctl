package run

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/spf13/cobra"
)

// runCommandRunner defines the interface for command options that can run
type runCommandRunner interface {
	Run(ctx context.Context) error
}

// runCommandConfig holds common configuration for building run commands
type runCommandConfig struct {
	cliParams     *settings.Run
	ioStreams     *terminal.IOStreams
	path          string
	runner        runCommandRunner
	writeErrorFn  func(msg string)
	getOutputFn   func() string
	setIOStreamFn func(ios *terminal.IOStreams, cli *settings.Run)
}

// makeRunEFunc creates a RunE function for run subcommands
func makeRunEFunc(cfg runCommandConfig, cmdUse string) func(*cobra.Command, []string) error {
	return func(cCmd *cobra.Command, args []string) error {
		cfg.cliParams.EntryPointSettings.Path = filepath.Join(cfg.path, cmdUse)
		ctx := settings.IntoContext(context.Background(), cfg.cliParams)

		lgr := logger.FromContext(cCmd.Context())
		if lgr != nil {
			ctx = logger.WithLogger(ctx, lgr)
		}

		cfg.setIOStreamFn(cfg.ioStreams, cfg.cliParams)

		err := output.ValidateCommands(args)
		if err != nil {
			cfg.writeErrorFn(err.Error())
			return err
		}

		if currentOutput := cfg.getOutputFn(); currentOutput != "" && currentOutput != "quiet" {
			err = output.ValidateOutputType(currentOutput, ValidOutputTypes[:2])
			if err != nil {
				cfg.writeErrorFn(err.Error())
				return err
			}
		}

		return cfg.runner.Run(ctx)
	}
}

// writeMetrics outputs provider execution metrics to stderr
func writeMetrics(errOut io.Writer) {
	allMetrics := provider.GlobalMetrics.GetAllMetrics()
	if len(allMetrics) == 0 {
		return
	}

	fmt.Fprintln(errOut, "")
	fmt.Fprintln(errOut, "Provider Execution Metrics:")
	fmt.Fprintln(errOut, strings.Repeat("-", 80))
	fmt.Fprintf(errOut, "%-25s %8s %8s %8s %12s %12s\n",
		"Provider", "Total", "Success", "Failure", "Avg Duration", "Success %")
	fmt.Fprintln(errOut, strings.Repeat("-", 80))

	// Sort provider names for consistent output
	names := make([]string, 0, len(allMetrics))
	for name := range allMetrics {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		m := allMetrics[name]
		avgDuration := m.AverageDuration()
		successRate := m.SuccessRate()
		fmt.Fprintf(errOut, "%-25s %8d %8d %8d %12s %11.1f%%\n",
			name,
			m.ExecutionCount,
			m.SuccessCount,
			m.FailureCount,
			avgDuration.Round(time.Millisecond),
			successRate)
	}
	fmt.Fprintln(errOut, strings.Repeat("-", 80))
}
