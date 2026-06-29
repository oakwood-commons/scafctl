// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"os"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/fingerprint"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// fingerprintsOptions holds the options for the fingerprints command.
type fingerprintsOptions struct {
	flags.KvxOutputFlags
	Path   string
	Action string
}

// CommandFingerprints creates the 'state fingerprints' command.
func CommandFingerprints(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &fingerprintsOptions{
		KvxOutputFlags: flags.KvxOutputFlags{AppName: cliParams.BinaryName},
	}

	cmd := &cobra.Command{
		Use:   "fingerprints",
		Short: "List fingerprint entries grouped by action",
		Long:  "Show fingerprint cache entries from a state file, grouped by action name.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFingerprints(cmd, opts, cliParams, ioStreams)
		},
	}

	cmd.Flags().StringVar(&opts.Path, "path", "", "State file path (relative to working directory or absolute)")
	cmd.Flags().StringVar(&opts.Action, "action", "", "Filter to a specific action name")
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)
	_ = cmd.MarkFlagRequired("path")

	return cmd
}

func runFingerprints(cmd *cobra.Command, opts *fingerprintsOptions, cliParams *settings.Run, ioStreams *terminal.IOStreams) error {
	ctx := cmd.Context()
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	cwd, err := os.Getwd()
	if err != nil {
		err := fmt.Errorf("cannot determine working directory: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	resolved, err := state.ResolveStatePath(opts.Path, cwd)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}
	if _, statErr := os.Stat(resolved); os.IsNotExist(statErr) {
		err := fmt.Errorf("state file not found: %s", resolved)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	sd, err := state.LoadFromFile(opts.Path, cwd)
	if err != nil {
		err := fmt.Errorf("failed to load state: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	actions := fingerprint.ListActions(sd)
	if opts.Action != "" {
		found := false
		for _, a := range actions {
			if a == opts.Action {
				found = true
				break
			}
		}
		if !found {
			if !cliParams.IsQuiet {
				w.WarnStderrf("No fingerprint entries found for action %q", opts.Action)
			}
			return nil
		}
		actions = []string{opts.Action}
	}

	if len(actions) == 0 {
		if !cliParams.IsQuiet {
			w.WarnStderr("No fingerprint entries found")
		}
		return nil
	}

	data := buildFingerprintRows(sd, actions)
	kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags, kvx.WithIOStreams(ioStreams))
	return kvxOpts.Write(data)
}

// buildFingerprintRows converts fingerprint state into a flat list of rows
// for kvx output, sorted by action name then type.
func buildFingerprintRows(sd *state.Data, actions []string) []map[string]any {
	var data []map[string]any
	for _, name := range actions {
		keys := fingerprintKeysForAction(sd, name)
		for _, key := range keys {
			entry := sd.Fingerprints[key]
			_, typ := splitFingerprintKey(key)
			row := map[string]any{
				"action": name,
				"type":   typ,
				"hash":   entry.Value,
			}
			if !entry.UpdatedAt.IsZero() {
				row["updatedAt"] = entry.UpdatedAt.Format("2006-01-02T15:04:05Z")
			}
			data = append(data, row)
		}
	}
	return data
}

// fingerprintKeysForAction returns all fingerprint keys for a given action, sorted.
func fingerprintKeysForAction(sd *state.Data, actionName string) []string {
	var keys []string
	for key := range sd.Fingerprints {
		if name, ok := fingerprint.ParseActionName(key); ok && name == actionName {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// splitFingerprintKey extracts the type suffix (sources, generates, inputs) from a
// fingerprint key. Returns the action name and type.
func splitFingerprintKey(key string) (string, string) {
	name, typ, ok := fingerprint.SplitKey(key)
	if !ok {
		return "", ""
	}
	return name, typ
}
