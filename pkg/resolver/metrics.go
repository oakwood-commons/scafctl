// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	resolverNameLabel = "resolver_name"
	phaseLabel        = "phase"
	statusLabel       = "status"
	providerLabel     = "provider"
)

var (
	// ResolverExecutionDuration tracks the duration of resolver executions
	ResolverExecutionDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    fmt.Sprintf("%s_resolver_execution_duration_seconds", settings.CliBinaryName),
		Help:    "Histogram of resolver execution duration in seconds",
		Buckets: []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30},
	}, []string{resolverNameLabel, statusLabel})

	// ResolverPhaseDuration tracks the duration of individual resolver phases
	ResolverPhaseDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    fmt.Sprintf("%s_resolver_phase_duration_seconds", settings.CliBinaryName),
		Help:    "Histogram of resolver phase duration in seconds",
		Buckets: []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30},
	}, []string{resolverNameLabel, phaseLabel})

	// ResolverExecutionsTotal tracks the total number of resolver executions
	ResolverExecutionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: fmt.Sprintf("%s_resolver_executions_total", settings.CliBinaryName),
		Help: "Total number of resolver executions",
	}, []string{resolverNameLabel, statusLabel})

	// ResolverProviderCallsTotal tracks the total number of provider calls made by resolvers
	ResolverProviderCallsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: fmt.Sprintf("%s_resolver_provider_calls_total", settings.CliBinaryName),
		Help: "Total number of provider calls made by resolvers",
	}, []string{resolverNameLabel, providerLabel, phaseLabel})

	// ResolverValueSize tracks the size of resolver values in bytes
	ResolverValueSize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    fmt.Sprintf("%s_resolver_value_size_bytes", settings.CliBinaryName),
		Help:    "Histogram of resolver value sizes in bytes",
		Buckets: []float64{100, 1000, 10000, 100000, 1000000, 10000000},
	}, []string{resolverNameLabel})

	// ResolverDependencyCount tracks the number of dependencies per resolver
	ResolverDependencyCount = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    fmt.Sprintf("%s_resolver_dependency_count", settings.CliBinaryName),
		Help:    "Histogram of resolver dependency counts",
		Buckets: []float64{0, 1, 2, 5, 10, 20, 50},
	}, []string{resolverNameLabel})

	// ResolverFailedAttemptsTotal tracks the total number of failed provider attempts
	ResolverFailedAttemptsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: fmt.Sprintf("%s_resolver_failed_attempts_total", settings.CliBinaryName),
		Help: "Total number of failed provider attempts before success or final failure",
	}, []string{resolverNameLabel, providerLabel, phaseLabel})

	// ResolverPhaseExecutionsTotal tracks the total number of phase executions
	ResolverPhaseExecutionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: fmt.Sprintf("%s_resolver_phase_executions_total", settings.CliBinaryName),
		Help: "Total number of resolver phase executions",
	}, []string{phaseLabel})

	// ResolverConcurrentExecutions tracks the current number of concurrent resolver executions
	ResolverConcurrentExecutions = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: fmt.Sprintf("%s_resolver_concurrent_executions", settings.CliBinaryName),
		Help: "Current number of concurrent resolver executions",
	})
)

// RegisterResolverMetrics registers all resolver-related Prometheus metrics
func RegisterResolverMetrics() {
	prometheus.MustRegister(ResolverExecutionDuration)
	prometheus.MustRegister(ResolverPhaseDuration)
	prometheus.MustRegister(ResolverExecutionsTotal)
	prometheus.MustRegister(ResolverProviderCallsTotal)
	prometheus.MustRegister(ResolverValueSize)
	prometheus.MustRegister(ResolverDependencyCount)
	prometheus.MustRegister(ResolverFailedAttemptsTotal)
	prometheus.MustRegister(ResolverPhaseExecutionsTotal)
	prometheus.MustRegister(ResolverConcurrentExecutions)
}

// RecordResolverExecution records metrics for a completed resolver execution
func RecordResolverExecution(resolverName string, result *ExecutionResult) {
	status := string(result.Status)

	// Record execution duration
	ResolverExecutionDuration.WithLabelValues(resolverName, status).Observe(result.TotalDuration.Seconds())

	// Record execution count
	ResolverExecutionsTotal.WithLabelValues(resolverName, status).Inc()

	// Record phase durations
	for _, pm := range result.PhaseMetrics {
		ResolverPhaseDuration.WithLabelValues(resolverName, pm.Phase).Observe(pm.Duration.Seconds())
	}

	// Record value size
	if result.ValueSizeBytes > 0 {
		ResolverValueSize.WithLabelValues(resolverName).Observe(float64(result.ValueSizeBytes))
	}

	// Record dependency count
	ResolverDependencyCount.WithLabelValues(resolverName).Observe(float64(result.DependencyCount))

	// Record failed attempts
	for _, attempt := range result.FailedAttempts {
		ResolverFailedAttemptsTotal.WithLabelValues(resolverName, attempt.Provider, attempt.Phase).Inc()
	}
}
