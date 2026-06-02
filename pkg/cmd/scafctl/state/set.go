// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
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
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set a state value",
		Long:  "Set or update a value in a state file. Defaults to the parameters section. Use --immutable to set an immutable value.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			sd, err := state.LoadFromFile(path)
			if err != nil {
				err := fmt.Errorf("failed to load state: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			coerced, coerceErr := coerceValue(value, valueType)
			if coerceErr != nil {
				w.Errorf("%v", coerceErr)
				return exitcode.WithCode(coerceErr, exitcode.InvalidInput)
			}

			// Block writes to keys that exist in immutables
			if _, isImmutable := sd.Immutables[key]; isImmutable {
				err := fmt.Errorf("key %q is immutable; use 'state delete --force --key %s' to remove it first", key, key)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			if immutable {
				sd.Immutables[key] = &state.ImmutableEntry{
					Value:     coerced,
					Type:      valueType,
					CreatedAt: time.Now().UTC(),
				}
			} else {
				sd.Parameters[key] = coerced
			}

			if err := state.SaveToFile(path, sd); err != nil {
				err := fmt.Errorf("failed to save state: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			w.Successf("Set key %q\n", key)
			return nil
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "State file path (relative to state directory)")
	cmd.Flags().StringVar(&key, "key", "", "Key to set")
	cmd.Flags().StringVar(&value, "value", "", "Value to store")
	cmd.Flags().StringVar(&valueType, "type", "string", "Value type (string, int, bool, etc.)")
	cmd.Flags().BoolVar(&immutable, "immutable", false, "Store as an immutable value (cannot be overwritten)")
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
