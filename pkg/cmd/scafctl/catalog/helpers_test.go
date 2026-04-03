// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// newCatalogTestCtx creates a context with writer for catalog command tests.
func newCatalogTestCtx(tb testing.TB) context.Context {
	tb.Helper()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	return writer.WithWriter(context.Background(), w)
}

// writerFromCtx retrieves the Writer from a test context.
func writerFromCtx(ctx context.Context) *writer.Writer {
	return writer.FromContext(ctx)
}
