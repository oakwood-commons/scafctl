// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package mockserver provides a configurable HTTP mock server for functional testing.
// It is designed to run as a test service, started by the test runner before
// test execution and stopped afterward. Routes are defined declaratively in
// solution YAML via the TestConfig.Services field.
package mockserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Route defines a single mock HTTP endpoint.
type Route struct {
	// Path is the URL path to match (exact match).
	Path string `json:"path" yaml:"path" doc:"URL path to match (exact)" maxLength:"500"`

	// Method is the HTTP method to match. Empty matches all methods.
	Method string `json:"method,omitempty" yaml:"method,omitempty" doc:"HTTP method to match (empty = all)" maxLength:"10"`

	// Status is the HTTP status code to return (default: 200).
	Status int `json:"status,omitempty" yaml:"status,omitempty" doc:"HTTP status code to return" maximum:"599"`

	// Body is the response body string.
	Body string `json:"body,omitempty" yaml:"body,omitempty" doc:"Response body" maxLength:"100000"`

	// Headers are response headers to set.
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty" doc:"Response headers"`

	// Delay is the simulated response delay as a Go duration string (e.g., "100ms").
	Delay string `json:"delay,omitempty" yaml:"delay,omitempty" doc:"Simulated response delay" maxLength:"20"`

	// Echo when true returns the request details as JSON instead of Body.
	Echo bool `json:"echo,omitempty" yaml:"echo,omitempty" doc:"Return request details as JSON"`
}

// EchoResponse is the JSON body returned when Echo is true.
type EchoResponse struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	Query   map[string]string `json:"query"`
}

// Request records a received HTTP request for later inspection.
type Request struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	Time    time.Time         `json:"time"`
}

// Server is a configurable HTTP mock server for testing.
type Server struct {
	routes   []Route
	server   *http.Server
	listener net.Listener
	port     int
	requests []Request
	mu       sync.Mutex
}

// New creates a new mock server with the given routes.
func New(routes []Route) *Server {
	return &Server{
		routes: routes,
	}
}

// Start begins listening on a random available port.
// The assigned port can be retrieved via Port().
func (s *Server) Start() error {
	lc := net.ListenConfig{}
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listening: %w", err)
	}

	s.listener = listener

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return fmt.Errorf("unexpected listener address type: %T", listener.Addr())
	}
	s.port = tcpAddr.Port

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handler)

	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		_ = s.server.Serve(listener)
	}()

	return s.waitReady()
}

// waitReady polls the server until it responds.
func (s *Server) waitReady() error {
	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/__health", s.port), nil)
		if err != nil {
			return fmt.Errorf("creating health request: %w", err)
		}
		resp, err := client.Do(req) //nolint:gosec // health-check URL is constructed from a known local port, not user input
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}

	return fmt.Errorf("server not ready after 5s")
}

// Stop shuts down the server gracefully.
func (s *Server) Stop() error {
	if s.server == nil {
		return nil
	}
	return s.server.Close()
}

// Port returns the port the server is listening on.
func (s *Server) Port() int {
	return s.port
}

// BaseURL returns the base URL for the server (e.g., "http://127.0.0.1:12345").
func (s *Server) BaseURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", s.port)
}

// Requests returns a copy of all recorded requests.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]Request, len(s.requests))
	copy(cp, s.requests)
	return cp
}

// handler routes incoming requests to configured routes.
func (s *Server) handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/__health" {
		w.WriteHeader(http.StatusOK)
		return
	}

	s.recordRequest(r)

	route := s.matchRoute(r)
	if route == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":  "no route matched",
			"path":   r.URL.Path,
			"method": r.Method,
		})
		return
	}

	if route.Delay != "" {
		d, err := time.ParseDuration(route.Delay)
		if err == nil {
			time.Sleep(d)
		}
	}

	for k, v := range route.Headers {
		w.Header().Set(k, v)
	}

	status := route.Status
	if status == 0 {
		status = http.StatusOK
	}

	if route.Echo {
		s.handleEcho(w, r, status)
		return
	}

	if w.Header().Get("Content-Type") == "" && route.Body != "" {
		trimmed := strings.TrimSpace(route.Body)
		if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
			(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
			w.Header().Set("Content-Type", "application/json")
		} else {
			w.Header().Set("Content-Type", "text/plain")
		}
	}

	w.WriteHeader(status)
	_, _ = io.WriteString(w, route.Body) //nolint:gosec // route body is test-defined, not user-tainted input
}

// handleEcho returns request details as JSON.
func (s *Server) handleEcho(w http.ResponseWriter, r *http.Request, status int) {
	body, _ := io.ReadAll(r.Body)

	headers := make(map[string]string)
	for k, v := range r.Header {
		headers[k] = strings.Join(v, ", ")
	}

	query := make(map[string]string)
	for k, v := range r.URL.Query() {
		query[k] = strings.Join(v, ", ")
	}

	echo := EchoResponse{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: headers,
		Body:    string(body),
		Query:   query,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(echo) //nolint:errcheck
}

// recordRequest saves request details for later inspection.
func (s *Server) recordRequest(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	headers := make(map[string]string)
	for k, v := range r.Header {
		headers[k] = strings.Join(v, ", ")
	}

	s.mu.Lock()
	s.requests = append(s.requests, Request{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: headers,
		Body:    string(body),
		Time:    time.Now(),
	})
	s.mu.Unlock()
}

// matchRoute finds the first route that matches the request.
func (s *Server) matchRoute(r *http.Request) *Route {
	for i := range s.routes {
		route := &s.routes[i]

		if route.Path != r.URL.Path {
			continue
		}

		if route.Method != "" && !strings.EqualFold(route.Method, r.Method) {
			continue
		}

		return route
	}
	return nil
}
