// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandSet creates the 'state set' command.
func CommandSet(_ *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	var (
		path      string
		key       string
		value     string
		valueType string
		immutable bool
		persist   bool
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set a state value",
		Long:  "Set or update a value in a state file. Defaults to the parameters section. Use --immutable to store a locked value or --persist to store a persisted resolver value.",
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			sd, err := state.LoadFromFile(path, cwd)
			if err != nil {
				err := fmt.Errorf("failed to load state: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			if immutable && persist {
				err := fmt.Errorf("--immutable and --persist are mutually exclusive")
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			coerced, coerceErr := coerceValue(value, valueType)
			if coerceErr != nil {
				w.Errorf("%v", coerceErr)
				return exitcode.WithCode(coerceErr, exitcode.InvalidInput)
			}

			// Block writes to keys locked by an immutable entry.
			if existing, ok := sd.Resolvers[key]; ok && existing.Immutable {
				err := fmt.Errorf("key %q is immutable; use 'state delete --force --key %s' to remove it first", key, key)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			now := time.Now().UTC()
			switch {
			case immutable:
				sd.Resolvers[key] = &state.PersistedEntry{
					Value:     coerced,
					Type:      valueType,
					Immutable: true,
					CreatedAt: now,
					UpdatedAt: now,
				}
			case persist:
				createdAt := now
				if existing, ok := sd.Resolvers[key]; ok {
					createdAt = existing.CreatedAt
				}
				sd.Resolvers[key] = &state.PersistedEntry{
					Value:     coerced,
					Type:      valueType,
					Immutable: false,
					CreatedAt: createdAt,
					UpdatedAt: now,
				}
			default:
				sd.Parameters[key] = coerced
			}

			if err := state.SaveToFile(path, cwd, sd); err != nil {
				err := fmt.Errorf("failed to save state: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			w.Successf("Set key %q\n", key)
			return nil
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "State file path (relative to working directory or absolute)")
	cmd.Flags().StringVar(&key, "key", "", "Key to set")
	cmd.Flags().StringVar(&value, "value", "", "Value to store")
	cmd.Flags().StringVar(&valueType, "type", "string", "Value type (string, int, bool, etc.)")
	cmd.Flags().BoolVar(&immutable, "immutable", false, "Store as an immutable value (locked; cannot be overwritten)")
	cmd.Flags().BoolVar(&persist, "persist", false, "Store as a persisted resolver value (overwritten on the next run)")
	_ = cmd.MarkFlagRequired("path")
	_ = cmd.MarkFlagRequired("key")
	_ = cmd.MarkFlagRequired("value")

	return cmd
}

// coerceValue converts a string CLI value to the appropriate Go type based on
// the --type flag, so that state entries are stored with the correct type.
// Returns an error if the value cannot be parsed as the requested type.
func coerceValue(raw, typ string) (any, error) {
	switch typ {
	case "int":
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as int: %w", raw, err)
		}
		return v, nil
	case "float":
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as float: %w", raw, err)
		}
		return v, nil
	case "bool":
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as bool: %w", raw, err)
		}
		return v, nil
	default:
		return raw, nil
	}
}
