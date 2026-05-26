// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"net/http"
	"sync"
)

// MockHandler implements Handler for testing.
type MockHandler struct {
	mu sync.Mutex

	NameValue         string
	DisplayNameValue  string
	FlowsValue        []Flow
	CapabilitiesValue []Capability

	LoginResult            *Result
	LoginErr               error
	LogoutErr              error
	StatusResult           *Status
	StatusErr              error
	GetTokenResult         *Token
	GetTokenErr            error
	InjectAuthErr          error
	ListCachedTokensResult []*CachedTokenInfo
	ListCachedTokensErr    error
	PurgeExpiredResult     int
	PurgeExpiredErr        error

	LoginCalls        []LoginOptions
	LogoutCalls       int
	StatusCalls       int
	GetTokenCalls     []TokenOptions
	InjectAuthCalls   []TokenOptions
	PurgeExpiredCalls int

	// LastContextProfile captures the profile from context on the most recent call.
	// This enables tests to verify that profile propagation reaches the handler.
	LastContextProfile string
}

// NewMockHandler creates a new mock auth handler with default values.
func NewMockHandler(name string) *MockHandler {
	return &MockHandler{
		NameValue:        name,
		DisplayNameValue: name,
		FlowsValue:       []Flow{FlowDeviceCode},
		StatusResult:     &Status{Authenticated: false},
	}
}

func (m *MockHandler) Name() string        { return m.NameValue }
func (m *MockHandler) DisplayName() string { return m.DisplayNameValue }
func (m *MockHandler) SupportedFlows() []Flow {
	if m.FlowsValue == nil {
		return []Flow{FlowDeviceCode}
	}
	return m.FlowsValue
}

func (m *MockHandler) Capabilities() []Capability {
	return m.CapabilitiesValue
}

func (m *MockHandler) Login(ctx context.Context, opts LoginOptions) (*Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastContextProfile = ProfileFromContext(ctx)
	m.LoginCalls = append(m.LoginCalls, opts)
	return m.LoginResult, m.LoginErr
}

func (m *MockHandler) Logout(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastContextProfile = ProfileFromContext(ctx)
	m.LogoutCalls++
	return m.LogoutErr
}

func (m *MockHandler) Status(ctx context.Context) (*Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastContextProfile = ProfileFromContext(ctx)
	m.StatusCalls++
	return m.StatusResult, m.StatusErr
}

func (m *MockHandler) GetToken(ctx context.Context, opts TokenOptions) (*Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastContextProfile = ProfileFromContext(ctx)
	m.GetTokenCalls = append(m.GetTokenCalls, opts)
	return m.GetTokenResult, m.GetTokenErr
}

func (m *MockHandler) InjectAuth(ctx context.Context, req *http.Request, opts TokenOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastContextProfile = ProfileFromContext(ctx)
	m.InjectAuthCalls = append(m.InjectAuthCalls, opts)
	if m.InjectAuthErr != nil {
		return m.InjectAuthErr
	}
	if m.GetTokenErr != nil {
		return m.GetTokenErr
	}
	if m.GetTokenResult != nil {
		req.Header.Set("Authorization", m.GetTokenResult.TokenType+" "+m.GetTokenResult.AccessToken)
	}
	return nil
}

func (m *MockHandler) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LoginCalls = nil
	m.LogoutCalls = 0
	m.StatusCalls = 0
	m.GetTokenCalls = nil
	m.InjectAuthCalls = nil
	m.PurgeExpiredCalls = 0
	m.LastContextProfile = ""
}

func (m *MockHandler) SetAuthenticated(claims *Claims) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StatusResult = &Status{Authenticated: true, Claims: claims}
}

func (m *MockHandler) SetNotAuthenticated() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StatusResult = &Status{Authenticated: false}
}

func (m *MockHandler) SetToken(token *Token) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetTokenResult = token
	m.GetTokenErr = nil
}

func (m *MockHandler) SetTokenError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetTokenResult = nil
	m.GetTokenErr = err
}

func (m *MockHandler) ListCachedTokens(ctx context.Context) ([]*CachedTokenInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastContextProfile = ProfileFromContext(ctx)
	return m.ListCachedTokensResult, m.ListCachedTokensErr
}

func (m *MockHandler) PurgeExpiredTokens(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastContextProfile = ProfileFromContext(ctx)
	m.PurgeExpiredCalls++
	return m.PurgeExpiredResult, m.PurgeExpiredErr
}

var (
	_ Handler     = (*MockHandler)(nil)
	_ TokenLister = (*MockHandler)(nil)
	_ TokenPurger = (*MockHandler)(nil)
)

// MockFlowDetectorHandler embeds MockHandler and adds FlowDetector support.
type MockFlowDetectorHandler struct {
	*MockHandler
	DetectFlowsResult []FlowAvailability
	DetectFlowsErr    error
}

// NewMockFlowDetectorHandler creates a mock handler that implements FlowDetector.
func NewMockFlowDetectorHandler(name string) *MockFlowDetectorHandler {
	return &MockFlowDetectorHandler{
		MockHandler: NewMockHandler(name),
	}
}

// DetectAvailableFlows implements FlowDetector.
func (m *MockFlowDetectorHandler) DetectAvailableFlows(_ context.Context) ([]FlowAvailability, error) {
	return m.DetectFlowsResult, m.DetectFlowsErr
}

var _ FlowDetector = (*MockFlowDetectorHandler)(nil)

// MockConfigurerHandler embeds MockHandler and adds Configurer support.
type MockConfigurerHandler struct {
	*MockHandler
	ApplyOverridesErr   error
	ApplyOverridesCalls []map[string]string
}

// NewMockConfigurerHandler creates a mock handler that implements Configurer.
func NewMockConfigurerHandler(name string) *MockConfigurerHandler {
	return &MockConfigurerHandler{
		MockHandler: NewMockHandler(name),
	}
}

// ApplyOverrides implements Configurer.
func (m *MockConfigurerHandler) ApplyOverrides(_ context.Context, overrides map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[string]string, len(overrides))
	for k, v := range overrides {
		cp[k] = v
	}
	m.ApplyOverridesCalls = append(m.ApplyOverridesCalls, cp)
	return m.ApplyOverridesErr
}

var _ Configurer = (*MockConfigurerHandler)(nil)
