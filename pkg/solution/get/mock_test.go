// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package get

import (
	"context"
	"errors"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMockGetter_FromLocalFileSystem(t *testing.T) {
	t.Run("successful call", func(t *testing.T) {
		mockGetter := &MockGetter{}
		ctx := context.Background()
		expectedSolution := &solution.Solution{}

		mockGetter.On("FromLocalFileSystem", ctx, "/path/to/solution.yaml").
			Return(expectedSolution, nil)

		result, err := mockGetter.FromLocalFileSystem(ctx, "/path/to/solution.yaml")

		require.NoError(t, err)
		assert.Equal(t, expectedSolution, result)
		mockGetter.AssertExpectations(t)
	})

	t.Run("error case", func(t *testing.T) {
		mockGetter := &MockGetter{}
		ctx := context.Background()
		expectedError := errors.New("file not found")

		mockGetter.On("FromLocalFileSystem", ctx, "/invalid/path").
			Return(nil, expectedError)

		result, err := mockGetter.FromLocalFileSystem(ctx, "/invalid/path")

		require.Error(t, err)
		assert.Nil(t, result)
		assert.Equal(t, expectedError, err)
		mockGetter.AssertExpectations(t)
	})

	t.Run("with any context", func(t *testing.T) {
		mockGetter := &MockGetter{}
		expectedSolution := &solution.Solution{}

		// Using mock.Anything for context allows any context to be passed
		mockGetter.On("FromLocalFileSystem", mock.Anything, "/path/to/solution.yaml").
			Return(expectedSolution, nil)

		result, err := mockGetter.FromLocalFileSystem(context.Background(), "/path/to/solution.yaml")

		require.NoError(t, err)
		assert.Equal(t, expectedSolution, result)
		mockGetter.AssertExpectations(t)
	})
}

func TestMockGetter_FromURL(t *testing.T) {
	t.Run("successful call", func(t *testing.T) {
		mockGetter := &MockGetter{}
		ctx := context.Background()
		expectedSolution := &solution.Solution{}

		mockGetter.On("FromURL", ctx, "https://example.com/solution.yaml").
			Return(expectedSolution, nil)

		result, err := mockGetter.FromURL(ctx, "https://example.com/solution.yaml")

		require.NoError(t, err)
		assert.Equal(t, expectedSolution, result)
		mockGetter.AssertExpectations(t)
	})

	t.Run("error case", func(t *testing.T) {
		mockGetter := &MockGetter{}
		ctx := context.Background()
		expectedError := errors.New("network error")

		mockGetter.On("FromURL", ctx, "https://invalid.url").
			Return(nil, expectedError)

		result, err := mockGetter.FromURL(ctx, "https://invalid.url")

		require.Error(t, err)
		assert.Nil(t, result)
		assert.Equal(t, expectedError, err)
		mockGetter.AssertExpectations(t)
	})

	t.Run("with any context", func(t *testing.T) {
		mockGetter := &MockGetter{}
		expectedSolution := &solution.Solution{}

		mockGetter.On("FromURL", mock.Anything, "https://example.com/solution.yaml").
			Return(expectedSolution, nil)

		result, err := mockGetter.FromURL(context.Background(), "https://example.com/solution.yaml")

		require.NoError(t, err)
		assert.Equal(t, expectedSolution, result)
		mockGetter.AssertExpectations(t)
	})
}

func TestMockGetter_Get(t *testing.T) {
	t.Run("successful call with path", func(t *testing.T) {
		mockGetter := &MockGetter{}
		ctx := context.Background()
		expectedSolution := &solution.Solution{}

		mockGetter.On("Get", ctx, "/path/to/solution.yaml").
			Return(expectedSolution, nil)

		result, err := mockGetter.Get(ctx, "/path/to/solution.yaml")

		require.NoError(t, err)
		assert.Equal(t, expectedSolution, result)
		mockGetter.AssertExpectations(t)
	})

	t.Run("successful call with URL", func(t *testing.T) {
		mockGetter := &MockGetter{}
		ctx := context.Background()
		expectedSolution := &solution.Solution{}

		mockGetter.On("Get", ctx, "https://example.com/solution.yaml").
			Return(expectedSolution, nil)

		result, err := mockGetter.Get(ctx, "https://example.com/solution.yaml")

		require.NoError(t, err)
		assert.Equal(t, expectedSolution, result)
		mockGetter.AssertExpectations(t)
	})

	t.Run("empty path", func(t *testing.T) {
		mockGetter := &MockGetter{}
		ctx := context.Background()
		expectedSolution := &solution.Solution{}

		mockGetter.On("Get", ctx, "").
			Return(expectedSolution, nil)

		result, err := mockGetter.Get(ctx, "")

		require.NoError(t, err)
		assert.Equal(t, expectedSolution, result)
		mockGetter.AssertExpectations(t)
	})

	t.Run("error case", func(t *testing.T) {
		mockGetter := &MockGetter{}
		ctx := context.Background()
		expectedError := errors.New("solution not found")

		mockGetter.On("Get", ctx, "/invalid/path").
			Return(nil, expectedError)

		result, err := mockGetter.Get(ctx, "/invalid/path")

		require.Error(t, err)
		assert.Nil(t, result)
		assert.Equal(t, expectedError, err)
		mockGetter.AssertExpectations(t)
	})

	t.Run("with any context", func(t *testing.T) {
		mockGetter := &MockGetter{}
		expectedSolution := &solution.Solution{}

		mockGetter.On("Get", mock.Anything, "/path/to/solution.yaml").
			Return(expectedSolution, nil)

		result, err := mockGetter.Get(context.Background(), "/path/to/solution.yaml")

		require.NoError(t, err)
		assert.Equal(t, expectedSolution, result)
		mockGetter.AssertExpectations(t)
	})
}

func TestMockGetter_FindSolution(t *testing.T) {
	t.Run("returns found path", func(t *testing.T) {
		mockGetter := &MockGetter{}

		mockGetter.On("FindSolution").Return("/path/to/solution.yaml")

		result := mockGetter.FindSolution()

		assert.Equal(t, "/path/to/solution.yaml", result)
		mockGetter.AssertExpectations(t)
	})

	t.Run("returns empty string when not found", func(t *testing.T) {
		mockGetter := &MockGetter{}

		mockGetter.On("FindSolution").Return("")

		result := mockGetter.FindSolution()

		assert.Empty(t, result)
		mockGetter.AssertExpectations(t)
	})

	t.Run("can be called multiple times", func(t *testing.T) {
		mockGetter := &MockGetter{}

		mockGetter.On("FindSolution").Return("/first/path").Once()
		mockGetter.On("FindSolution").Return("/second/path").Once()

		result1 := mockGetter.FindSolution()
		result2 := mockGetter.FindSolution()

		assert.Equal(t, "/first/path", result1)
		assert.Equal(t, "/second/path", result2)
		mockGetter.AssertExpectations(t)
	})
}

func TestMockGetter_InterfaceCompliance(t *testing.T) {
	// Verify that MockGetter implements Interface
	var _ Interface = (*MockGetter)(nil)
}

// ExampleMockGetter demonstrates basic usage of the MockGetter for testing.
func ExampleMockGetter() {
	// Create a new mock
	mockGetter := &MockGetter{}

	// Set up expectations
	expectedSolution := &solution.Solution{}
	mockGetter.On("FromLocalFileSystem", mock.Anything, "/path/to/solution.yaml").
		Return(expectedSolution, nil)

	// Use the mock in your code
	ctx := context.Background()
	sol, err := mockGetter.FromLocalFileSystem(ctx, "/path/to/solution.yaml")
	if err != nil {
		panic(err)
	}

	_ = sol // Use the solution

	// In a real test, you would assert expectations at the end
	// mockGetter.AssertExpectations(t)
}
