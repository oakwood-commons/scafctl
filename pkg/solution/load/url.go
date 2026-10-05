// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/oakwood-commons/scafctl/pkg/filepath"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/telemetry"
)

// URLLoader loads a solution from an http(s) URL.
// Supported kinds: KindURL.
type URLLoader struct {
	client  *httpc.Client
	lenient bool
}

var _ Loader = (*URLLoader)(nil)

// URLOption configures a URLLoader.
type URLOption interface {
	applyURL(*URLLoader)
}

// NewURLLoader returns a URLLoader using client for requests. The client's
// destination-address (SSRF) policy applies to every fetch. When client is
// nil, the secure-default client is used (private, loopback, and link-local
// addresses denied; proxy routing disabled).
func NewURLLoader(client *httpc.Client, opts ...URLOption) *URLLoader {
	if client == nil {
		client = httpc.NewClientFromAppConfig(nil, logr.Discard())
	}
	l := &URLLoader{client: client}
	for _, opt := range opts {
		opt.applyURL(l)
	}
	return l
}

// Load fetches the solution at c.Ref and parses it. Compose entries are not
// applied, matching the current URL behavior.
func (l *URLLoader) Load(ctx context.Context, c source.Classification) (*MaterializedSolution, error) {
	if c.Kind != source.KindURL {
		return nil, fmt.Errorf("url loader: %w: %s", ErrUnsupportedKind, c.Kind)
	}

	url := c.Ref
	if !filepath.IsURL(url) {
		return nil, fmt.Errorf("the provided path to the solution is not a valid URL: %s", url)
	}

	ctx, span := telemetry.Tracer(telemetry.TracerSolution).Start(
		ctx, "solution.load.URL",
		trace.WithAttributes(attribute.String("solution.url", url)),
	)
	defer span.End()

	log := logger.FromContext(ctx).WithValues("url", url)
	log.V(1).Info("fetching solution from URL")

	// Destination-address policy is enforced by the HTTP client at dial time;
	// denials surface here as errors wrapping httpc.ErrBlockedByPolicy.
	resp, err := l.client.Get(ctx, url)
	if err != nil {
		err = httpc.ExplainBlocked(err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("fetching solution from URL %q: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("non-200 response from URL %q: %d", url, resp.StatusCode)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("reading response body from URL %q: %w", url, err)
	}

	sol := &solution.Solution{}
	// Set the path before parsing so the source map built during
	// unmarshalling records the correct source in finding positions.
	sol.SetPath(url)
	if err := parseSolution(sol, data, l.lenient); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("loading solution from URL %q: %w", url, err)
	}

	log.V(1).Info("loaded solution from URL", "size", len(data))

	return &MaterializedSolution{
		Solution: sol,
		Source:   c,
	}, nil
}
