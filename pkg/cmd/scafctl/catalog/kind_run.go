// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// runKind is the shared flow of the kind subcommands (`catalog pull|push|delete
// <kind>`): validate the options, build the request from them, then hand it to
// run.
//
// Validators and builders return plain domain errors. The exit code is assigned
// here, once: a failure in either phase means the user supplied bad input, so
// both map to exitcode.InvalidInput. Failures inside run carry their own codes
// because those depend on what the catalog returned.
//
// verb ("pull", "push", "delete") and reference only shape the messages.
func runKind[O, B any](
	ctx context.Context,
	verb, reference string,
	opts O,
	validate func(O) error,
	build func(context.Context, O) (B, error),
	run func(context.Context, O, B) error,
) error {
	w := writer.FromContext(ctx)

	if err := validate(opts); err != nil {
		w.Errorf("validation failed for %s %q: %v", verb, reference, err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	built, err := build(ctx, opts)
	if err != nil {
		w.Errorf("failed to build %s reference for %q: %v", verb, reference, err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	return run(ctx, opts, built)
}
