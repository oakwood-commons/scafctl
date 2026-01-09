package httpc

import (
	"errors"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/metrics"
)

// ErrCircuitBreakerOpen is returned when the circuit breaker is open and prevents requests
var ErrCircuitBreakerOpen = errors.New("circuit breaker is open")

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState int

const (
	// StateClosed allows all requests through
	StateClosed CircuitBreakerState = iota
	// StateOpen blocks all requests
	StateOpen
	// StateHalfOpen allows a single test request through
	StateHalfOpen
)

// CircuitBreakerConfig holds configuration for circuit breaker
type CircuitBreakerConfig struct {
	// MaxFailures is the number of consecutive failures before opening the circuit
	MaxFailures int
	// OpenTimeout is how long to wait before transitioning from Open to HalfOpen
	OpenTimeout time.Duration
	// HalfOpenMaxRequests is the number of successful requests in HalfOpen before closing
	HalfOpenMaxRequests int
}

// DefaultCircuitBreakerConfig returns default circuit breaker configuration
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		MaxFailures:         5,
		OpenTimeout:         30 * time.Second,
		HalfOpenMaxRequests: 1,
	}
}

// circuitBreaker implements the circuit breaker pattern per host.
//
// Note: This implementation is currently internal to the httpc package but could be
// extracted into a separate, reusable package in the future. The circuit breaker logic
// is generic and could be useful for other types of operations beyond HTTP requests.
//
// Thread-Safety: circuitBreaker is safe for concurrent use by multiple goroutines.
// All state changes are protected by a mutex.
type circuitBreaker struct {
	config            *CircuitBreakerConfig
	mu                sync.RWMutex
	breakers          map[string]*hostBreaker
	metricsUpdateFunc func(string, CircuitBreakerState)
}

// hostBreaker tracks state for a specific host
type hostBreaker struct {
	state           CircuitBreakerState
	failures        int
	lastStateChange time.Time
	halfOpenSuccess int
}

// newCircuitBreaker creates a new circuit breaker
func newCircuitBreaker(config *CircuitBreakerConfig) *circuitBreaker {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}

	return &circuitBreaker{
		config:   config,
		breakers: make(map[string]*hostBreaker),
		metricsUpdateFunc: func(host string, state CircuitBreakerState) {
			metrics.HTTPClientCircuitBreakerState.WithLabelValues(host).Set(float64(state))
		},
	}
}

// allow checks if a request should be allowed for the given host
func (cb *circuitBreaker) allow(host string) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	hb := cb.getOrCreateBreaker(host)

	// Check if we should transition from Open to HalfOpen
	if hb.state == StateOpen {
		if time.Since(hb.lastStateChange) >= cb.config.OpenTimeout {
			cb.setState(host, hb, StateHalfOpen)
		} else {
			return ErrCircuitBreakerOpen
		}
	}

	// Allow the request
	return nil
}

// recordSuccess records a successful request
func (cb *circuitBreaker) recordSuccess(host string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	hb := cb.getOrCreateBreaker(host)

	switch hb.state {
	case StateHalfOpen:
		hb.halfOpenSuccess++
		if hb.halfOpenSuccess >= cb.config.HalfOpenMaxRequests {
			cb.setState(host, hb, StateClosed)
			hb.failures = 0
			hb.halfOpenSuccess = 0
		}
	case StateClosed:
		// Reset failures on success
		hb.failures = 0
	case StateOpen:
		// No action needed in open state
	}
}

// recordFailure records a failed request
func (cb *circuitBreaker) recordFailure(host string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	hb := cb.getOrCreateBreaker(host)

	switch hb.state {
	case StateHalfOpen:
		// Failed in half-open, go back to open
		cb.setState(host, hb, StateOpen)
		hb.halfOpenSuccess = 0
	case StateClosed:
		hb.failures++
		if hb.failures >= cb.config.MaxFailures {
			cb.setState(host, hb, StateOpen)
		}
	case StateOpen:
		// No action needed, already open
	}
}

// getOrCreateBreaker gets or creates a breaker for a host
func (cb *circuitBreaker) getOrCreateBreaker(host string) *hostBreaker {
	hb, exists := cb.breakers[host]
	if !exists {
		hb = &hostBreaker{
			state:           StateClosed,
			lastStateChange: time.Now(),
		}
		cb.breakers[host] = hb
		cb.metricsUpdateFunc(host, StateClosed)
	}
	return hb
}

// setState updates the state of a host breaker
func (cb *circuitBreaker) setState(host string, hb *hostBreaker, state CircuitBreakerState) {
	hb.state = state
	hb.lastStateChange = time.Now()
	cb.metricsUpdateFunc(host, state)
}

// getState returns the current state for a host
func (cb *circuitBreaker) getState(host string) CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	hb, exists := cb.breakers[host]
	if !exists {
		return StateClosed
	}
	return hb.state
}
