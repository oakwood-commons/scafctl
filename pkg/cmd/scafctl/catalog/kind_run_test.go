// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunKind(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")

	t.Run("validate failure skips build and run, maps to InvalidInput", func(t *testing.T) {
		t.Parallel()

		built, ran := false, false
		err := runKind(newCatalogTestCtx(t), "push", "my-sol", "opts",
			func(string) error { return errBoom },
			func(context.Context, string) (int, error) { built = true; return 0, nil },
			func(context.Context, string, int) error { ran = true; return nil })

		require.ErrorIs(t, err, errBoom)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		assert.False(t, built, "build must not run after failed validation")
		assert.False(t, ran, "run must not run after failed validation")
	})

	t.Run("validate failure is reported on stderr under quiet", func(t *testing.T) {
		t.Parallel()

		ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
		w := writer.New(ioStreams, &settings.Run{IsQuiet: true})
		ctx := writer.WithWriter(t.Context(), w)

		err := runKind(ctx, "push", "my-sol", "opts",
			func(string) error { return errBoom },
			func(context.Context, string) (int, error) { return 0, nil },
			func(context.Context, string, int) error { return nil })

		require.ErrorIs(t, err, errBoom)
		assert.Contains(t, errBuf.String(), `validation failed for push "my-sol": boom`)
		assert.Empty(t, outBuf.String())
	})

	t.Run("build failure skips run, maps to InvalidInput", func(t *testing.T) {
		t.Parallel()

		ran := false
		err := runKind(newCatalogTestCtx(t), "pull", "my-sol", "opts",
			func(string) error { return nil },
			func(context.Context, string) (int, error) { return 0, errBoom },
			func(context.Context, string, int) error { ran = true; return nil })

		require.ErrorIs(t, err, errBoom)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		assert.False(t, ran, "run must not run after failed build")
	})

	t.Run("success passes opts and build to run", func(t *testing.T) {
		t.Parallel()

		var gotOpts string
		var gotBuild int
		err := runKind(newCatalogTestCtx(t), "delete", "my-sol@1.0.0", "opts",
			func(string) error { return nil },
			func(context.Context, string) (int, error) { return 42, nil },
			func(_ context.Context, o string, b int) error { gotOpts, gotBuild = o, b; return nil })

		require.NoError(t, err)
		assert.Equal(t, "opts", gotOpts)
		assert.Equal(t, 42, gotBuild)
	})

	t.Run("run error keeps its own exit code", func(t *testing.T) {
		t.Parallel()

		err := runKind(newCatalogTestCtx(t), "push", "my-sol", "opts",
			func(string) error { return nil },
			func(context.Context, string) (int, error) { return 0, nil },
			func(context.Context, string, int) error {
				return exitcode.WithCode(errBoom, exitcode.FileNotFound)
			})

		require.ErrorIs(t, err, errBoom)
		assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
	})
}
