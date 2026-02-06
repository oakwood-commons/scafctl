package auth

import (
	"context"
	"net/http"
	"sync"
)

// MockHandler implements Handler for testing.
type MockHandler struct {
	mu sync.Mutex

	NameValue        string
	DisplayNameValue string
	FlowsValue       []Flow

	LoginResult    *Result
	LoginErr       error
	LogoutErr      error
	StatusResult   *Status
	StatusErr      error
	GetTokenResult *Token
	GetTokenErr    error
	InjectAuthErr  error

	LoginCalls      []LoginOptions
	LogoutCalls     int
	StatusCalls     int
	GetTokenCalls   []TokenOptions
	InjectAuthCalls []TokenOptions
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

func (m *MockHandler) Login(_ context.Context, opts LoginOptions) (*Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LoginCalls = append(m.LoginCalls, opts)
	return m.LoginResult, m.LoginErr
}

func (m *MockHandler) Logout(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LogoutCalls++
	return m.LogoutErr
}

func (m *MockHandler) Status(_ context.Context) (*Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StatusCalls++
	return m.StatusResult, m.StatusErr
}

func (m *MockHandler) GetToken(_ context.Context, opts TokenOptions) (*Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetTokenCalls = append(m.GetTokenCalls, opts)
	return m.GetTokenResult, m.GetTokenErr
}

func (m *MockHandler) InjectAuth(_ context.Context, req *http.Request, opts TokenOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.InjectAuthCalls = append(m.InjectAuthCalls, opts)
	if m.InjectAuthErr != nil {
		return m.InjectAuthErr
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

var _ Handler = (*MockHandler)(nil)
