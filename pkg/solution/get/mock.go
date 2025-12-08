package get

import (
	"context"

	"github.com/kcloutie/scafctl/pkg/solution"
	"github.com/stretchr/testify/mock"
)

// MockGetter is a mock implementation of the Interface for testing purposes.
// It uses testify/mock to provide flexible mocking capabilities.
//
// Basic Usage Example:
//
//	func TestMyFunction(t *testing.T) {
//	    // Create a mock
//	    mockGetter := &MockGetter{}
//
//	    // Set up expectations
//	    expectedSolution := &solution.Solution{Name: "test-solution"}
//	    mockGetter.On("FromLocalFileSystem", mock.Anything, "/path/to/solution.yaml").
//	        Return(expectedSolution, nil)
//
//	    // Use the mock in your code
//	    result, err := mockGetter.FromLocalFileSystem(context.Background(), "/path/to/solution.yaml")
//	    require.NoError(t, err)
//	    assert.Equal(t, expectedSolution, result)
//
//	    // Verify all expectations were met
//	    mockGetter.AssertExpectations(t)
//	}
//
// Advanced Usage - Multiple Calls:
//
//	mockGetter.On("FromLocalFileSystem", mock.Anything, "/path1").
//	    Return(&solution.Solution{}, nil).Once()
//	mockGetter.On("FromLocalFileSystem", mock.Anything, "/path2").
//	    Return(nil, errors.New("not found")).Once()
//
// Advanced Usage - Argument Matchers:
//
//	// Match any context
//	mockGetter.On("FromUrl", mock.Anything, "https://example.com").
//	    Return(&solution.Solution{}, nil)
//
//	// Match specific context with custom matcher
//	mockGetter.On("FromUrl", mock.MatchedBy(func(ctx context.Context) bool {
//	    return ctx.Value("key") == "value"
//	}), "https://example.com").Return(&solution.Solution{}, nil)
type MockGetter struct {
	mock.Mock
}

// FromLocalFileSystem mocks the FromLocalFileSystem method of the Interface.
// It returns the configured mock response for the given path.
//
// Example usage:
//
//	mockGetter := &MockGetter{}
//	expectedSolution := &solution.Solution{Name: "test"}
//	mockGetter.On("FromLocalFileSystem", mock.Anything, "/path/to/solution.yaml").
//	    Return(expectedSolution, nil)
func (m *MockGetter) FromLocalFileSystem(ctx context.Context, path string) (*solution.Solution, error) {
	args := m.Called(ctx, path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	sol, ok := args.Get(0).(*solution.Solution)
	if !ok {
		return nil, args.Error(1)
	}
	return sol, args.Error(1)
}

// FromURL mocks the FromURL method which retrieves a solution from a URL.
// It returns the configured mock response for the given URL.
//
// Example usage:
//
//	mockGetter := &MockGetter{}
//	expectedSolution := &solution.Solution{Name: "test"}
//	mockGetter.On("FromURL", mock.Anything, "https://example.com/solution.yaml").
//	    Return(expectedSolution, nil)
func (m *MockGetter) FromURL(ctx context.Context, url string) (*solution.Solution, error) {
	args := m.Called(ctx, url)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	sol, ok := args.Get(0).(*solution.Solution)
	if !ok {
		return nil, args.Error(1)
	}
	return sol, args.Error(1)
}

// Get mocks the Get method which retrieves a solution from a path (URL or local file).
// It returns the configured mock response for the given path.
//
// Example usage:
//
//	mockGetter := &MockGetter{}
//	expectedSolution := &solution.Solution{Name: "test"}
//	mockGetter.On("Get", mock.Anything, "/path/to/solution.yaml").
//	    Return(expectedSolution, nil)
func (m *MockGetter) Get(ctx context.Context, path string) (*solution.Solution, error) {
	args := m.Called(ctx, path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	sol, ok := args.Get(0).(*solution.Solution)
	if !ok {
		return nil, args.Error(1)
	}
	return sol, args.Error(1)
}

// FindSolution mocks the FindSolution method which searches for a solution file
// in default locations. It returns the configured mock response.
//
// Example usage:
//
//	mockGetter := &MockGetter{}
//	mockGetter.On("FindSolution").Return("/path/to/solution.yaml")
func (m *MockGetter) FindSolution() string {
	args := m.Called()
	return args.String(0)
}

// AssertExpectations asserts that all expected calls were made.
// This should be called at the end of tests to verify mock expectations.
//
// Example usage:
//
//	func TestSomething(t *testing.T) {
//	    mockGetter := &MockGetter{}
//	    mockGetter.On("FromLocalFileSystem", mock.Anything, "/path").Return(&solution.Solution{}, nil)
//	    // ... test code that uses mockGetter ...
//	    mockGetter.AssertExpectations(t)
//	}
func (m *MockGetter) AssertExpectations(t mock.TestingT) bool {
	return m.Mock.AssertExpectations(t)
}
