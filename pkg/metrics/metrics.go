package metrics

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	statusCodeLabel   = "status_code"
	urlLabel          = "url"
	pathLabel         = "path"
	methodLabel       = "method"
	errorTypeLabel    = "error_type"
	LabelHost         = "host"
	LabelPathTemplate = "path_template"
)

var (
	requestTimesBuckets = []float64{.1, .2, .4, .6, .8, 1, 2.5, 5, 10, 20}

	// httpRequestsTotal is a Prometheus counter vector that tracks the total number of HTTP requests.
	// It is labeled by request path and status code, allowing for detailed metrics on request volume and outcomes.
	// The metric name is dynamically generated based on the CLI binary name from settings.
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_http_requests_total", settings.CliBinaryName),
			Help: "Number of HTTP requests",
		},
		[]string{pathLabel, statusCodeLabel},
	)

	// HTTPClientDuration is a Prometheus histogram vector that records the duration of HTTP client requests in seconds.
	// It is labeled by HTTP method, host, path template, and status code to provide detailed performance metrics
	// while maintaining bounded cardinality through path parameterization.
	HTTPClientDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    fmt.Sprintf("%s_http_client_duration_seconds", settings.CliBinaryName),
		Help:    "HTTP client request duration in seconds",
		Buckets: requestTimesBuckets,
	}, []string{methodLabel, LabelHost, LabelPathTemplate, statusCodeLabel})

	// HTTPClientRequestsTotal is a Prometheus counter vector that tracks the total number of HTTP client requests.
	// It is labeled by HTTP method, host, path template, and status code.
	HTTPClientRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: fmt.Sprintf("%s_http_client_requests_total", settings.CliBinaryName),
		Help: "Total number of HTTP client requests",
	}, []string{methodLabel, LabelHost, LabelPathTemplate, statusCodeLabel})

	// HTTPClientErrorsTotal is a Prometheus counter vector that tracks the total number of HTTP client errors.
	// It is labeled by HTTP method, host, path template, and error type (e.g., client_error, server_error, timeout, etc.).
	HTTPClientErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: fmt.Sprintf("%s_http_client_errors_total", settings.CliBinaryName),
		Help: "Total number of HTTP client errors by type",
	}, []string{methodLabel, LabelHost, LabelPathTemplate, errorTypeLabel})

	// HTTPClientRetriesTotal is a Prometheus counter vector that tracks the total number of HTTP client retry attempts.
	// It is labeled by HTTP method, host, and path template.
	HTTPClientRetriesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: fmt.Sprintf("%s_http_client_retries_total", settings.CliBinaryName),
		Help: "Total number of HTTP client retry attempts",
	}, []string{methodLabel, LabelHost, LabelPathTemplate})

	// HTTPClientCacheHits is a Prometheus gauge that tracks the total number of HTTP cache hits.
	HTTPClientCacheHits = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: fmt.Sprintf("%s_http_client_cache_hits_total", settings.CliBinaryName),
		Help: "Total number of HTTP cache hits",
	})

	// HTTPClientCacheMisses is a Prometheus gauge that tracks the total number of HTTP cache misses.
	HTTPClientCacheMisses = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: fmt.Sprintf("%s_http_client_cache_misses_total", settings.CliBinaryName),
		Help: "Total number of HTTP cache misses",
	})

	// HTTPClientRequestSize is a Prometheus histogram vector that records the size of HTTP request bodies in bytes.
	HTTPClientRequestSize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    fmt.Sprintf("%s_http_client_request_size_bytes", settings.CliBinaryName),
		Help:    "HTTP client request body size in bytes",
		Buckets: []float64{100, 1000, 10000, 100000, 1000000, 10000000},
	}, []string{methodLabel, LabelHost, LabelPathTemplate})

	// HTTPClientResponseSize is a Prometheus histogram vector that records the size of HTTP response bodies in bytes.
	HTTPClientResponseSize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    fmt.Sprintf("%s_http_client_response_size_bytes", settings.CliBinaryName),
		Help:    "HTTP client response body size in bytes",
		Buckets: []float64{100, 1000, 10000, 100000, 1000000, 10000000},
	}, []string{methodLabel, LabelHost, LabelPathTemplate})

	// HTTPClientCacheSizeBytes is a Prometheus gauge that tracks the total size of cached data in bytes.
	HTTPClientCacheSizeBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: fmt.Sprintf("%s_http_client_cache_size_bytes", settings.CliBinaryName),
		Help: "Total size of HTTP client cache in bytes",
	})

	// HTTPClientConcurrentRequests is a Prometheus gauge that tracks the current number of concurrent HTTP requests.
	HTTPClientConcurrentRequests = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: fmt.Sprintf("%s_http_client_concurrent_requests", settings.CliBinaryName),
		Help: "Current number of concurrent HTTP client requests",
	})

	// HTTPClientCircuitBreakerState is a Prometheus gauge vector that tracks circuit breaker states (0=closed, 1=open, 2=half-open).
	HTTPClientCircuitBreakerState = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: fmt.Sprintf("%s_http_client_circuit_breaker_state", settings.CliBinaryName),
		Help: "Circuit breaker state: 0=closed, 1=open, 2=half-open",
	}, []string{LabelHost})

	// GetSolutionTimeHistogram is a Prometheus histogram vector that records the duration (in seconds)
	// it takes to retrieve a solution. The histogram is labeled by the request path and uses predefined
	// buckets specified by requestTimesBuckets. This metric helps monitor and analyze the performance
	// of solution retrieval operations.
	GetSolutionTimeHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    fmt.Sprintf("%s_get_solution_duration_seconds", settings.CliBinaryName),
		Help:    "Histogram of the time it takes to get a solution in seconds",
		Buckets: requestTimesBuckets,
	}, []string{pathLabel})
)

// Handler returns an HTTP handler that exposes Prometheus metrics.
// It can be used to serve metrics at an endpoint for scraping by Prometheus.
func Handler() http.Handler {
	return promhttp.Handler()
}

// RegisterMetrics registers Prometheus metrics and sets up the HTTP handler for the "/metrics" endpoint.
// It ensures that the application's metrics are exposed and available for scraping by Prometheus.
func RegisterMetrics() {
	prometheus.MustRegister(httpRequestsTotal)
	prometheus.MustRegister(HTTPClientDuration)
	prometheus.MustRegister(HTTPClientRequestsTotal)
	prometheus.MustRegister(HTTPClientErrorsTotal)
	prometheus.MustRegister(HTTPClientRetriesTotal)
	prometheus.MustRegister(HTTPClientCacheHits)
	prometheus.MustRegister(HTTPClientCacheMisses)
	prometheus.MustRegister(HTTPClientRequestSize)
	prometheus.MustRegister(HTTPClientResponseSize)
	prometheus.MustRegister(HTTPClientCacheSizeBytes)
	prometheus.MustRegister(HTTPClientConcurrentRequests)
	prometheus.MustRegister(HTTPClientCircuitBreakerState)
	prometheus.MustRegister(GetSolutionTimeHistogram)
	http.Handle("/metrics", Handler())
}

// PrometheusMiddleware returns a Gin middleware handler that records the total number of HTTP requests.
// It increments a Prometheus counter metric labeled with the request path and response status code
// after each request is processed.
func PrometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		statusCode := strconv.Itoa(c.Writer.Status())
		httpRequestsTotal.With(prometheus.Labels{pathLabel: c.Request.URL.Path, statusCodeLabel: statusCode}).Inc()
	}
}
