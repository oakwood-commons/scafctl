// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync"
)

// MockHTTPClient is a configurable mock of HTTPClient for unit tests.
type MockHTTPClient struct {
	mu        sync.Mutex
	Requests  []*MockRequest
	Responses []*MockResponse
	callIndex int
}

// MockRequest records a request made via the mock client.
type MockRequest struct {
	Method   string
	Endpoint string
	Data     url.Values
	Headers  map[string]string
	Body     []byte
}

// MockResponse defines a canned response.
type MockResponse struct {
	StatusCode int
	Body       any // Will be JSON-encoded
	Err        error
}

// NewMockHTTPClient creates a new mock HTTP client.
func NewMockHTTPClient() *MockHTTPClient {
	return &MockHTTPClient{
		Requests:  make([]*MockRequest, 0),
		Responses: make([]*MockResponse, 0),
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

// nextResponse records the request and returns the next canned response.
func (m *MockHTTPClient) nextResponse(method, endpoint string, data url.Values, headers map[string]string, body []byte) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Requests = append(m.Requests, &MockRequest{
		Method:   method,
		Endpoint: endpoint,
		Data:     data,
		Headers:  headers,
		Body:     body,
	})

	if m.callIndex >= len(m.Responses) {
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

// PostForm implements HTTPClient.PostForm.
func (m *MockHTTPClient) PostForm(_ context.Context, endpoint string, data url.Values) (*http.Response, error) {
	return m.nextResponse(http.MethodPost, endpoint, data, nil, nil)
}

// Get implements HTTPClient.Get.
func (m *MockHTTPClient) Get(_ context.Context, endpoint string, headers map[string]string) (*http.Response, error) {
	return m.nextResponse(http.MethodGet, endpoint, nil, headers, nil)
}

// Do implements HTTPClient.Do.
func (m *MockHTTPClient) Do(_ context.Context, req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	headers := make(map[string]string)
	for k, v := range req.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	return m.nextResponse(req.Method, req.URL.String(), nil, headers, body)
}
