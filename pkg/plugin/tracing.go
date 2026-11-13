// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"github.com/oakwood-commons/scafctl/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Span names for plugin acquisition.
const (
	spanEnsure  = "plugin.ensure"
	spanAcquire = "plugin.acquire"
	spanFetch   = "plugin.fetch"
	spanSpawn   = "plugin.spawn"
)

// Span event names.
const (
	eventProcessStarted  = "process_started"
	eventProvidersListed = "providers_listed"
)

// Span attribute keys.
const (
	attrDependencyCount     = attribute.Key("plugin.dependency_count")
	attrPoolActive          = attribute.Key("plugin.pool_active")
	attrPoolIdle            = attribute.Key("plugin.pool_idle")
	attrPoolDead            = attribute.Key("plugin.pool_dead")
	attrName                = attribute.Key("plugin.name")
	attrCatalog             = attribute.Key("plugin.catalog")
	attrVersion             = attribute.Key("plugin.version")
	attrVersionConstraint   = attribute.Key("plugin.version_constraint")
	attrResolvedVersion     = attribute.Key("plugin.resolved_version")
	attrOutcome             = attribute.Key("plugin.outcome")
	attrSource              = attribute.Key("plugin.source")
	attrFromLock            = attribute.Key("plugin.from_lock")
	attrProvidersRegistered = attribute.Key("plugin.providers_registered")
	attrErrorStage          = attribute.Key("plugin.error_stage")
)

// Values of the plugin.outcome attribute on plugin.acquire spans.
const (
	// outcomePoolHit means the plugin was already running and ready.
	outcomePoolHit = "pool_hit"
	// outcomeWaited means another request was already spawning the plugin and
	// this one waited for it.
	outcomeWaited = "waited"
	// outcomeSpawned means this request started the plugin.
	outcomeSpawned = "spawned"
)

// Values of the plugin.source attribute, shared with
// metrics.RecordPluginResolution so traces and metrics agree.
const (
	fetchSourceCache    = "cache"
	fetchSourceRegistry = "registry"
)

// pluginTracer returns the plugin tracer from tp, or from the global provider
// when tp is nil.
func pluginTracer(tp trace.TracerProvider) trace.Tracer {
	if tp == nil {
		return telemetry.Tracer(telemetry.TracerPlugin)
	}
	return tp.Tracer(telemetry.TracerPlugin)
}

// endSpan records err (if any) on span, tags it with the failing acquisition
// stage when known, and ends the span.
func endSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if stage, ok := ErrorStage(err); ok {
			span.SetAttributes(attrErrorStage.String(string(stage)))
		}
	}
	span.End()
}
