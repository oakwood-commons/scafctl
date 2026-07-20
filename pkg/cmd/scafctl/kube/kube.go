// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandKube creates the 'kube' command group for Kubernetes / OpenShift
// cluster operations.
func CommandKube(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:     "kube",
		Aliases: []string{"k8s"},
		Short:   "Manage Kubernetes / OpenShift cluster access",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Manage authenticated access to Kubernetes and OpenShift clusters.

			These commands authenticate with a scafctl auth handler and wire kubectl /
			oc to a cluster by writing a kubeconfig exec-credential entry. Subsequent
			kubectl / oc calls mint fresh tokens on demand without re-running login.

			Use 'scafctl kube login <cluster> --handler <name>' to authenticate and
			write a kubeconfig entry.
			Use 'scafctl kube logout <cluster>' to remove the entry and optionally
			revoke the handler's cached credentials.
			Use 'scafctl kube list' to see clusters the resolver knows about, and
			'scafctl kube status' to inspect the current kubeconfig context.
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
	})

	cmdPath := fmt.Sprintf("%s/%s", path, cmd.Use)
	cmd.AddCommand(CommandLogin(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandLogout(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandList(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandStatus(cliParams, ioStreams, cmdPath))

	return cmd
}
