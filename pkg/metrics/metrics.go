package metrics

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/kcloutie/scafctl/pkg/settings"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	statusCodeLabel = "status_code"
	urlLabel        = "url"
	pathLabel       = "path"
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

	// HTTPClientCallsTimeHistogram is a Prometheus histogram vector that records the duration of HTTP client calls in seconds.
	// It uses custom buckets defined by requestTimesBuckets and labels each observation with the request URL and HTTP status code.
	// This metric helps monitor and analyze the performance of outbound HTTP requests made by the CLI.
	HTTPClientCallsTimeHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    fmt.Sprintf("%s_http_call_duration_seconds", settings.CliBinaryName),
		Help:    "Histogram of the time it takes to make a client http call in seconds",
		Buckets: requestTimesBuckets,
	}, []string{urlLabel, statusCodeLabel})

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
	prometheus.MustRegister(HTTPClientCallsTimeHistogram)
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
