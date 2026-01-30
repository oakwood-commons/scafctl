package snapshot

import (
	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandSnapshot creates the snapshot command
func CommandSnapshot(cliParams *settings.Run, ioStreams terminal.IOStreams, binaryName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage resolver execution snapshots",
		Long: heredoc.Doc(`
			Manage resolver execution snapshots for debugging, testing, and comparison.
			
			Snapshots capture the complete state of resolver execution including values,
			status, timing, errors, and metadata. They can be saved to files for later
			analysis, used in golden file testing, or compared to detect configuration drift.
		`),
		Example: heredoc.Docf(`
			# Capture and save execution snapshot
			$ %s snapshot save config.yaml --output snapshot.json
			
			# Load and display a snapshot
			$ %s snapshot show snapshot.json
			
			# Compare two snapshots
			$ %s snapshot diff before.json after.json
			
			# Save with sensitive data redaction
			$ %s snapshot save config.yaml --output snapshot.json --redact
		`, binaryName, binaryName, binaryName, binaryName),
	}

	cmd.AddCommand(CommandSave(cliParams, ioStreams, binaryName))
	cmd.AddCommand(CommandShow(cliParams, ioStreams, binaryName))
	cmd.AddCommand(CommandDiff(cliParams, ioStreams, binaryName))

	return cmd
}
