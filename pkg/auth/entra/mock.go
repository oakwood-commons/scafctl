// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync"
)

// MockHTTPClient is a mock implementation of HTTPClient for testing.
type MockHTTPClient struct {
	mu        sync.Mutex
	Responses []*MockResponse
	Requests  []*MockRequest
	callIndex int
}

// MockRequest records a request made to the mock client.
type MockRequest struct {
	Endpoint string
	Data     url.Values
}

// MockResponse defines a response to return from the mock client.
type MockResponse struct {
	StatusCode int
	Body       any // Will be JSON-encoded
	Err        error
}

// NewMockHTTPClient creates a new mock HTTP client.
func NewMockHTTPClient() *MockHTTPClient {
	return &MockHTTPClient{
		Responses: make([]*MockResponse, 0),
		Requests:  make([]*MockRequest, 0),
	}
}

// AddResponse adds a response to the queue.
func (m *MockHTTPClient) AddResponse(statusCode int, body any) *MockHTTPClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Responses = append(m.Responses, &MockResponse{
		StatusCode: statusCode,
		Body:       body,
	})
	return m
}

// AddError adds an error response to the queue.
func (m *MockHTTPClient) AddError(err error) *MockHTTPClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Responses = append(m.Responses, &MockResponse{
		Err: err,
	})
	return m
}

// PostForm implements HTTPClient.PostForm.
func (m *MockHTTPClient) PostForm(ctx context.Context, endpoint string, data url.Values) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Record the request
	m.Requests = append(m.Requests, &MockRequest{
		Endpoint: endpoint,
		Data:     data,
	})

	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get the next response
	if m.callIndex >= len(m.Responses) {
		// Default response if no more responses configured
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(bytes.NewBufferString(`{"error": "no mock response configured"}`)),
		}, nil
	}

	resp := m.Responses[m.callIndex]
	m.callIndex++

	if resp.Err != nil {
		return nil, resp.Err
	}

	// Encode body as JSON
	var bodyBytes []byte
	if resp.Body != nil {
		var err error
		bodyBytes, err = json.Marshal(resp.Body)
		if err != nil {
			return nil, err
		}
	}

	return &http.Response{
		StatusCode: resp.StatusCode,
		Body:       io.NopCloser(bytes.NewBuffer(bodyBytes)),
	}, nil
}

// GetRequests returns all recorded requests.
func (m *MockHTTPClient) GetRequests() []*MockRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Requests
}

// Reset clears all recorded requests and responses.
func (m *MockHTTPClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Requests = make([]*MockRequest, 0)
	m.Responses = make([]*MockResponse, 0)
	m.callIndex = 0
}
