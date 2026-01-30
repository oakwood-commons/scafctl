package resolver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testResolver is a minimal resolver definition for testing redaction
type testResolver struct {
	Name      string
	Sensitive bool
}

func (r *testResolver) GetName() string {
	return r.Name
}

func (r *testResolver) GetSensitive() bool {
	return r.Sensitive
}

func TestCaptureSnapshot(t *testing.T) {
	tests := []struct {
		name            string
		solutionName    string
		solutionVersion string
		buildVersion    string
		parameters      map[string]any
		results         map[string]*ExecutionResult
		totalDuration   time.Duration
		overallStatus   ExecutionStatus
		wantErr         bool
	}{
		{
			name:            "basic snapshot",
			solutionName:    "test-solution",
			solutionVersion: "1.0.0",
			buildVersion:    "0.1.0",
			parameters: map[string]any{
				"env":    "production",
				"region": "us-west-2",
			},
			results: map[string]*ExecutionResult{
				"resolver1": {
					Value:             "value1",
					Status:            ExecutionStatusSuccess,
					Phase:             1,
					TotalDuration:     100 * time.Millisecond,
					ValueSizeBytes:    6,
					ProviderCallCount: 1,
				},
				"resolver2": {
					Value:             "value2",
					Status:            ExecutionStatusSuccess,
					Phase:             2,
					TotalDuration:     200 * time.Millisecond,
					ValueSizeBytes:    6,
					ProviderCallCount: 2,
				},
			},
			totalDuration: 300 * time.Millisecond,
			overallStatus: ExecutionStatusSuccess,
			wantErr:       false,
		},
		{
			name:            "snapshot with failed resolver",
			solutionName:    "test-solution",
			solutionVersion: "1.0.0",
			buildVersion:    "0.1.0",
			parameters: map[string]any{
				"env": "production",
			},
			results: map[string]*ExecutionResult{
				"success": {
					Value:             "value",
					Status:            ExecutionStatusSuccess,
					Phase:             1,
					TotalDuration:     50 * time.Millisecond,
					ValueSizeBytes:    5,
					ProviderCallCount: 1,
				},
				"failed": {
					Value:             nil,
					Status:            ExecutionStatusFailed,
					Phase:             2,
					TotalDuration:     100 * time.Millisecond,
					Error:             errors.New("test error"),
					ProviderCallCount: 1,
				},
			},
			totalDuration: 150 * time.Millisecond,
			overallStatus: ExecutionStatusFailed,
			wantErr:       false,
		},
		{
			name:            "snapshot with failed attempts",
			solutionName:    "test-solution",
			solutionVersion: "1.0.0",
			buildVersion:    "0.1.0",
			parameters:      map[string]any{},
			results: map[string]*ExecutionResult{
				"withRetries": {
					Value:             "final-value",
					Status:            ExecutionStatusSuccess,
					Phase:             1,
					TotalDuration:     500 * time.Millisecond,
					ValueSizeBytes:    11,
					ProviderCallCount: 3,
					FailedAttempts: []ProviderAttempt{
						{
							Provider:   "primary-api",
							Phase:      "resolve",
							Error:      "timeout",
							Duration:   200 * time.Millisecond,
							SourceStep: 0,
							Timestamp:  time.Date(2026, 1, 14, 10, 0, 0, 0, time.UTC),
						},
						{
							Provider:   "backup-api",
							Phase:      "resolve",
							Error:      "connection refused",
							Duration:   100 * time.Millisecond,
							SourceStep: 1,
							Timestamp:  time.Date(2026, 1, 14, 10, 0, 1, 0, time.UTC),
						},
					},
				},
			},
			totalDuration: 500 * time.Millisecond,
			overallStatus: ExecutionStatusSuccess,
			wantErr:       false,
		},
		{
			name:            "snapshot with skipped resolver",
			solutionName:    "test-solution",
			solutionVersion: "1.0.0",
			buildVersion:    "0.1.0",
			parameters:      map[string]any{},
			results: map[string]*ExecutionResult{
				"skipped": {
					Value:         nil,
					Status:        ExecutionStatusSkipped,
					Phase:         1,
					TotalDuration: 0,
				},
			},
			totalDuration: 0,
			overallStatus: ExecutionStatusSuccess,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create resolver context with results
			resolverCtx := NewContext()
			for name, result := range tt.results {
				resolverCtx.SetResult(name, result)
			}

			// Add resolver context to Go context
			ctx := WithContext(context.Background(), resolverCtx)

			snapshot, err := CaptureSnapshot(
				ctx,
				tt.solutionName,
				tt.solutionVersion,
				tt.buildVersion,
				tt.parameters,
				tt.totalDuration,
				tt.overallStatus,
			)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, snapshot)

			// Verify metadata
			assert.Equal(t, tt.solutionName, snapshot.Metadata.Solution)
			assert.Equal(t, tt.solutionVersion, snapshot.Metadata.Version)
			assert.Equal(t, tt.buildVersion, snapshot.Metadata.ScafctlVersion)
			assert.Equal(t, string(tt.overallStatus), snapshot.Metadata.Status)
			assert.NotZero(t, snapshot.Metadata.Timestamp)

			// Verify parameters
			assert.Equal(t, tt.parameters, snapshot.Parameters)

			// Verify resolvers
			assert.Equal(t, len(tt.results), len(snapshot.Resolvers))
			for name, expectedResult := range tt.results {
				sr, ok := snapshot.Resolvers[name]
				require.True(t, ok, "resolver %s should be in snapshot", name)
				assert.Equal(t, string(expectedResult.Status), sr.Status)
				assert.Equal(t, expectedResult.Phase, sr.Phase)
				assert.Equal(t, expectedResult.Value, sr.Value)
				assert.Equal(t, expectedResult.ProviderCallCount, sr.ProviderCalls)

				if expectedResult.Error != nil {
					assert.Equal(t, expectedResult.Error.Error(), sr.Error)
				}

				// Verify failed attempts
				if len(expectedResult.FailedAttempts) > 0 {
					require.Equal(t, len(expectedResult.FailedAttempts), len(sr.FailedAttempts))
					for i, attempt := range expectedResult.FailedAttempts {
						assert.Equal(t, attempt.Provider, sr.FailedAttempts[i].Provider)
						assert.Equal(t, attempt.Error, sr.FailedAttempts[i].Error)
						assert.Equal(t, attempt.SourceStep, sr.FailedAttempts[i].SourceStep)
					}
				}
			}

			// Verify phases
			assert.NotEmpty(t, snapshot.Phases)
		})
	}
}

func TestCaptureSnapshot_NoContext(t *testing.T) {
	ctx := context.Background() // No resolver context

	snapshot, err := CaptureSnapshot(
		ctx,
		"test",
		"1.0.0",
		"0.1.0",
		map[string]any{},
		100*time.Millisecond,
		ExecutionStatusSuccess,
	)

	assert.Error(t, err)
	assert.Nil(t, snapshot)
	assert.Contains(t, err.Error(), "resolver context not found")
}

func TestRedactSensitiveValues(t *testing.T) {
	snapshot := &Snapshot{
		Metadata: SnapshotMetadata{
			Solution: "test",
		},
		Resolvers: map[string]*SnapshotResolver{
			"publicValue": {
				Value:          "visible",
				Status:         "success",
				Phase:          1,
				ValueSizeBytes: 7,
			},
			"secretApiKey": {
				Value:          "super-secret-key",
				Status:         "success",
				Phase:          1,
				ValueSizeBytes: 16,
			},
			"anotherSecret": {
				Value:          map[string]any{"password": "secret123"},
				Status:         "success",
				Phase:          2,
				ValueSizeBytes: 20,
			},
		},
	}

	resolvers := []ResolverLike{
		&testResolver{Name: "publicValue", Sensitive: false},
		&testResolver{Name: "secretApiKey", Sensitive: true},
		&testResolver{Name: "anotherSecret", Sensitive: true},
	}

	RedactSensitiveValues(snapshot, resolvers)

	// Public value should be unchanged
	assert.Equal(t, "visible", snapshot.Resolvers["publicValue"].Value)
	assert.False(t, snapshot.Resolvers["publicValue"].Sensitive)
	assert.Equal(t, int64(7), snapshot.Resolvers["publicValue"].ValueSizeBytes)

	// Sensitive values should be redacted
	assert.Equal(t, "<redacted>", snapshot.Resolvers["secretApiKey"].Value)
	assert.True(t, snapshot.Resolvers["secretApiKey"].Sensitive)
	assert.Equal(t, int64(0), snapshot.Resolvers["secretApiKey"].ValueSizeBytes)

	assert.Equal(t, "<redacted>", snapshot.Resolvers["anotherSecret"].Value)
	assert.True(t, snapshot.Resolvers["anotherSecret"].Sensitive)
	assert.Equal(t, int64(0), snapshot.Resolvers["anotherSecret"].ValueSizeBytes)
}

func TestSaveAndLoadSnapshot(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "snapshot.json")

	// Create a snapshot
	originalSnapshot := &Snapshot{
		Metadata: SnapshotMetadata{
			Solution:       "test-solution",
			Version:        "1.0.0",
			Timestamp:      time.Date(2026, 1, 14, 10, 0, 0, 0, time.UTC),
			ScafctlVersion: "0.1.0",
			TotalDuration:  "1.234s",
			Status:         "success",
		},
		Parameters: map[string]any{
			"env":    "production",
			"region": "us-west-2",
		},
		Resolvers: map[string]*SnapshotResolver{
			"resolver1": {
				Value:          "value1",
				Status:         "success",
				Phase:          1,
				Duration:       "100ms",
				ProviderCalls:  1,
				ValueSizeBytes: 6,
			},
			"resolver2": {
				Value:          "value2",
				Status:         "success",
				Phase:          2,
				Duration:       "200ms",
				ProviderCalls:  2,
				ValueSizeBytes: 6,
			},
		},
		Phases: []SnapshotPhase{
			{
				Phase:     1,
				Duration:  "100ms",
				Resolvers: []string{"resolver1"},
			},
			{
				Phase:     2,
				Duration:  "200ms",
				Resolvers: []string{"resolver2"},
			},
		},
	}

	// Save snapshot
	err := SaveSnapshot(originalSnapshot, filePath)
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	// Load snapshot
	loadedSnapshot, err := LoadSnapshot(filePath)
	require.NoError(t, err)
	require.NotNil(t, loadedSnapshot)

	// Verify loaded snapshot matches original
	assert.Equal(t, originalSnapshot.Metadata.Solution, loadedSnapshot.Metadata.Solution)
	assert.Equal(t, originalSnapshot.Metadata.Version, loadedSnapshot.Metadata.Version)
	assert.Equal(t, originalSnapshot.Metadata.ScafctlVersion, loadedSnapshot.Metadata.ScafctlVersion)
	assert.Equal(t, originalSnapshot.Metadata.Status, loadedSnapshot.Metadata.Status)

	assert.Equal(t, originalSnapshot.Parameters, loadedSnapshot.Parameters)
	assert.Equal(t, len(originalSnapshot.Resolvers), len(loadedSnapshot.Resolvers))
	assert.Equal(t, len(originalSnapshot.Phases), len(loadedSnapshot.Phases))

	// Verify resolver details
	for name, original := range originalSnapshot.Resolvers {
		loaded, ok := loadedSnapshot.Resolvers[name]
		require.True(t, ok, "resolver %s should be in loaded snapshot", name)
		assert.Equal(t, original.Value, loaded.Value)
		assert.Equal(t, original.Status, loaded.Status)
		assert.Equal(t, original.Phase, loaded.Phase)
		assert.Equal(t, original.Duration, loaded.Duration)
		assert.Equal(t, original.ProviderCalls, loaded.ProviderCalls)
		assert.Equal(t, original.ValueSizeBytes, loaded.ValueSizeBytes)
	}
}

func TestSaveSnapshot_InvalidPath(t *testing.T) {
	snapshot := &Snapshot{
		Metadata: SnapshotMetadata{
			Solution: "test",
		},
	}

	err := SaveSnapshot(snapshot, "/invalid/path/that/does/not/exist/snapshot.json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write snapshot file")
}

func TestLoadSnapshot_FileNotFound(t *testing.T) {
	snapshot, err := LoadSnapshot("/nonexistent/file.json")
	assert.Error(t, err)
	assert.Nil(t, snapshot)
	assert.Contains(t, err.Error(), "failed to read snapshot file")
}

func TestLoadSnapshot_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "invalid.json")

	// Write invalid JSON
	err := os.WriteFile(filePath, []byte("not valid json {"), 0o644)
	require.NoError(t, err)

	snapshot, err := LoadSnapshot(filePath)
	assert.Error(t, err)
	assert.Nil(t, snapshot)
	assert.Contains(t, err.Error(), "failed to unmarshal snapshot")
}

func TestSnapshot_CompleteWorkflow(t *testing.T) {
	// Create a complete resolver execution context
	resolverCtx := NewContext()

	// Add successful resolver
	resolverCtx.SetResult("environment", &ExecutionResult{
		Value:             "production",
		Status:            ExecutionStatusSuccess,
		Phase:             1,
		TotalDuration:     10 * time.Millisecond,
		ValueSizeBytes:    10,
		ProviderCallCount: 1,
	})

	// Add resolver with failed attempts
	resolverCtx.SetResult("apiUrl", &ExecutionResult{
		Value:             "https://fallback.example.com/api",
		Status:            ExecutionStatusSuccess,
		Phase:             2,
		TotalDuration:     500 * time.Millisecond,
		ValueSizeBytes:    35,
		ProviderCallCount: 3,
		FailedAttempts: []ProviderAttempt{
			{
				Provider:   "primary-api",
				Phase:      "resolve",
				Error:      "timeout",
				Duration:   200 * time.Millisecond,
				SourceStep: 0,
				Timestamp:  time.Now(),
			},
			{
				Provider:   "backup-api",
				Phase:      "resolve",
				Error:      "connection refused",
				Duration:   100 * time.Millisecond,
				SourceStep: 1,
				Timestamp:  time.Now(),
			},
		},
	})

	// Add sensitive resolver
	resolverCtx.SetResult("secretApiKey", &ExecutionResult{
		Value:             "super-secret-key",
		Status:            ExecutionStatusSuccess,
		Phase:             1,
		TotalDuration:     5 * time.Millisecond,
		ValueSizeBytes:    16,
		ProviderCallCount: 1,
	})

	ctx := WithContext(context.Background(), resolverCtx)

	// Capture snapshot
	snapshot, err := CaptureSnapshot(
		ctx,
		"myapp",
		"1.0.0",
		"0.1.0",
		map[string]any{
			"environment": "production",
			"region":      "us-west-2",
		},
		510*time.Millisecond,
		ExecutionStatusSuccess,
	)
	require.NoError(t, err)

	// Redact sensitive values
	resolvers := []ResolverLike{
		&testResolver{Name: "environment", Sensitive: false},
		&testResolver{Name: "apiUrl", Sensitive: false},
		&testResolver{Name: "secretApiKey", Sensitive: true},
	}
	RedactSensitiveValues(snapshot, resolvers)

	// Verify redaction
	assert.Equal(t, "<redacted>", snapshot.Resolvers["secretApiKey"].Value)
	assert.True(t, snapshot.Resolvers["secretApiKey"].Sensitive)

	// Save snapshot
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "snapshot.json")
	err = SaveSnapshot(snapshot, filePath)
	require.NoError(t, err)

	// Load snapshot
	loadedSnapshot, err := LoadSnapshot(filePath)
	require.NoError(t, err)

	// Verify sensitive value is still redacted
	assert.Equal(t, "<redacted>", loadedSnapshot.Resolvers["secretApiKey"].Value)

	// Verify failed attempts are preserved
	assert.Len(t, loadedSnapshot.Resolvers["apiUrl"].FailedAttempts, 2)
	assert.Equal(t, "primary-api", loadedSnapshot.Resolvers["apiUrl"].FailedAttempts[0].Provider)
	assert.Equal(t, "timeout", loadedSnapshot.Resolvers["apiUrl"].FailedAttempts[0].Error)
}

func TestSnapshot_LoadFixture(t *testing.T) {
	// This test verifies that snapshot JSON fixtures can be loaded correctly
	// The fixtures are used for regression testing

	tests := []struct {
		name          string
		fixturePath   string
		expectedCount int
		expectedSol   string
	}{
		{
			name:          "simple chain fixture",
			fixturePath:   "testdata/snapshots/simple_chain_expected.json",
			expectedCount: 3,
			expectedSol:   "simple-chain-snapshot",
		},
		{
			name:          "diamond pattern fixture",
			fixturePath:   "testdata/snapshots/diamond_pattern_expected.json",
			expectedCount: 4,
			expectedSol:   "diamond-pattern-snapshot",
		},
		{
			name:          "error handling fixture",
			fixturePath:   "testdata/snapshots/error_handling_expected.json",
			expectedCount: 3,
			expectedSol:   "error-handling-snapshot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot, err := LoadSnapshot(tt.fixturePath)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedSol, snapshot.Metadata.Solution)
			assert.Equal(t, tt.expectedCount, len(snapshot.Resolvers))
		})
	}
}
