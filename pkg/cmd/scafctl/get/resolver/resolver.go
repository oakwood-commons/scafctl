package resolver

import (
	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandResolver creates the get resolver command
func CommandResolver(cliParams *settings.Run, ioStreams *terminal.IOStreams, binaryName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "resolver",
		Aliases: []string{"resolvers", "res"},
		Short:   "Get resolver information",
		Long: heredoc.Doc(`
			Get information about resolvers.

			This command provides tools for inspecting resolver definitions,
			extracting references, and analyzing resolver configurations.
		`),
		Example: heredoc.Docf(`
			# Extract resolver references from a template file
			$ %[1]s get resolver refs --template-file template.tmpl

			# Extract references from a template with custom delimiters
			$ %[1]s get resolver refs --template-file template.tmpl --left-delim '<%' --right-delim '%%>'

			# Extract references from an inline template
			$ %[1]s get resolver refs --template '{{ ._.config.host }}:{{ ._.port }}'

			# Extract references from a CEL expression
			$ %[1]s get resolver refs --expr '_.config.host + ":" + string(_.port)'
		`, binaryName),
	}

	cmd.AddCommand(CommandRefs(cliParams, ioStreams, binaryName))

	return cmd
}
