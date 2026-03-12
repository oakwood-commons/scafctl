// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package profiler

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/pprof/profile"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/stretchr/testify/assert"
)

// resetProfilerSingleton resets the profiler singleton state for testing.
// This is safe because it uses the same mutex as GetProfiler/StopProfiler.
func resetProfilerSingleton() {
	profilerMu.Lock()
	defer profilerMu.Unlock()
	instance = nil
	profilerStarted = false
}

func TestGetProfiler(t *testing.T) {
	// Create a logger for testing
	lgr := logger.GetNoopLogger()

	t.Run("Valid Profiler Type - CPU", func(t *testing.T) {
		// Reset the singleton instance for testing
		instance = nil

		profiler, err := GetProfiler("cpu", "", lgr)
		assert.NoError(t, err, "Expected no error for valid profiler type")
		assert.NotNil(t, profiler, "Expected a valid profilerProxy instance")
		assert.Equal(t, "cpu", profiler.profileType, "Expected profiler type to be 'cpu'")
		assert.Equal(t, "./", profiler.path, "Expected default path to be './'")
		resetProfilerSingleton()
	})

	t.Run("Valid Profiler Type - Memory", func(t *testing.T) {
		// Reset the singleton instance for testing
		instance = nil

		tmpDir := t.TempDir()
		profiler, err := GetProfiler("memory", tmpDir, lgr)
		assert.NoError(t, err, "Expected no error for valid profiler type")
		assert.NotNil(t, profiler, "Expected a valid profilerProxy instance")
		assert.Equal(t, "memory", profiler.profileType, "Expected profiler type to be 'memory'")
		assert.Equal(t, tmpDir, profiler.path, "Expected path to be tmpDir")
		resetProfilerSingleton()
	})

	t.Run("Invalid Profiler Type", func(t *testing.T) {
		// Reset the singleton instance for testing
		instance = nil

		profiler, err := GetProfiler("invalid", "", lgr)
		assert.Error(t, err, "Expected an error for invalid profiler type")
		assert.Nil(t, profiler, "Expected no profilerProxy instance for invalid profiler type")
		assert.Contains(t, err.Error(), "invalid value for --profiler", "Expected error message to indicate invalid profiler type")
		resetProfilerSingleton()
	})

	t.Run("Singleton Behavior", func(t *testing.T) {
		// Reset the singleton instance for testing
		instance = nil

		tmpDir1 := t.TempDir()
		tmpDir2 := t.TempDir()
		profiler1, err := GetProfiler("cpu", tmpDir1, lgr)
		assert.NoError(t, err, "Expected no error for valid profiler type")
		assert.NotNil(t, profiler1, "Expected a valid profilerProxy instance")

		profiler2, err := GetProfiler("memory", tmpDir2, lgr)
		assert.NoError(t, err, "Expected no error for valid profiler type")
		assert.NotNil(t, profiler2, "Expected a valid profilerProxy instance")

		// Ensure the singleton instance is reused
		assert.Equal(t, profiler1, profiler2, "Expected the same singleton instance to be returned")
		assert.Equal(t, "cpu", profiler2.profileType, "Expected the singleton instance to retain the first initialized profiler type")
		assert.Equal(t, tmpDir1, profiler2.path, "Expected the singleton instance to retain the first initialized path")
		resetProfilerSingleton()
	})
}

func TestWriteMergedProfile(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	t.Run("Nil Profile", func(t *testing.T) {
		// Create a profilerProxy instance
		proxy := &Proxy{
			path: tempDir,
		}

		// Call writeMergedProfile with a nil profile
		err := proxy.writeMergedProfile(nil)

		// Assert that an error is returned
		assert.Error(t, err, "Expected an error when profile is nil")
		assert.Contains(t, err.Error(), "no profile data to write", "Expected error message to indicate nil profile")
	})

	t.Run("Successful Write", func(t *testing.T) {
		// Create a dummy profile
		dummyProfile := &profile.Profile{
			SampleType: []*profile.ValueType{
				{Type: "samples", Unit: "count"},
			},
			Sample: []*profile.Sample{
				{
					Value: []int64{1},
				},
			},
		}

		// Create a profilerProxy instance
		proxy := &Proxy{
			path:        tempDir,
			profileType: "cpu",
		}

		// Call writeMergedProfile with the dummy profile
		err := proxy.writeMergedProfile(dummyProfile)

		// Assert that no error is returned
		assert.NoError(t, err, "Expected no error when writing merged profile")

		// Verify that the file was created
		expectedFilePath := filepath.Join(tempDir, "cpu.prof")
		_, err = os.Stat(expectedFilePath)
		assert.NoError(t, err, "Expected the merged profile file to exist")

		// Verify the contents of the file
		f, err := os.Open(expectedFilePath)
		assert.NoError(t, err, "Expected to open the merged profile file")
		defer f.Close()

		// Parse the written profile
		parsedProfile, err := profile.Parse(f)
		assert.NoError(t, err, "Expected to parse the written profile")
		assert.Equal(t, dummyProfile.SampleType[0].Type, parsedProfile.SampleType[0].Type, "Expected the sample type to match")
		assert.Equal(t, dummyProfile.Sample[0].Value[0], parsedProfile.Sample[0].Value[0], "Expected the sample value to match")
		resetProfilerSingleton()
	})
}

func TestStop(t *testing.T) {
	tempDir := t.TempDir()

	temp, err := os.CreateTemp(tempDir, "testfile")
	assert.NoError(t, err, "Expected no error creating temp file")

	dummyProfile := &profile.Profile{
		SampleType: []*profile.ValueType{
			{Type: "samples", Unit: "count"},
		},
		Sample: []*profile.Sample{
			{
				Value: []int64{1},
			},
		},
	}

	defer func() {
		if temp != nil {
			temp.Close()
		}
		os.Remove(temp.Name())
	}()

	var buf bytes.Buffer
	err = dummyProfile.Write(&buf)

	assert.NoError(t, err, "Expected no error writing dummy profile")

	temp.Write(buf.Bytes())

	proxy := &Proxy{
		profiler: &cpuProfiler{
			fileManager: &fileManager{
				files: []*os.File{temp},
			},
		},
		stopCh:      make(chan struct{}),
		path:        tempDir,
		profileType: "cpu",
	}

	err = proxy.Stop(logger.GetNoopLogger())
	assert.NoError(t, err, "Expected no error stopping profiler")
	assert.Nil(t, proxy.profiler.(*cpuProfiler).fileManager.files, "Expected files to be cleaned up")
	assert.False(t, proxy.profiler.(*cpuProfiler).active, "Expected CPU profiler to be inactive after stop")
	assert.NotEmpty(t, proxy.finalPath, "Expected finalPath to be set after stopping profiler")
	assert.FileExists(t, proxy.finalPath, "Expected merged profile file to exist")

	os.Remove(proxy.finalPath)
	resetProfilerSingleton()
}

func TestCpuStart(t *testing.T) {
	profiler := &cpuProfiler{
		fileManager: &fileManager{
			files: []*os.File{},
		},
	}

	err := profiler.Start()
	assert.NoError(t, err, "Expected no error starting profiler")
	assert.Len(t, profiler.getFiles(), 1, "Expected one file to be created after starting profiler")

	err = profiler.Start()
	assert.NoError(t, err, "Expected no error starting profiler again")
	assert.Len(t, profiler.getFiles(), 1, "Expected no additional file to be created when starting profiler again")

	profiler.Stop()
	profiler.cleanUp()
	assert.Len(t, profiler.getFiles(), 0, "Expected files to be cleaned up")
}

func TestMemoryStart(t *testing.T) {
	profiler := &memoryProfiler{
		fileManager: &fileManager{
			files: []*os.File{},
		},
	}

	err := profiler.Start()
	assert.NoError(t, err, "Expected no error starting profiler")
	assert.Len(t, profiler.getFiles(), 1, "Expected one file to be created after starting profiler")

	profiler.cleanUp()
	assert.Len(t, profiler.getFiles(), 0, "Expected files to be cleaned up")
}

func TestGoroutineStart(t *testing.T) {
	profiler := &goroutineProfiler{
		fileManager: &fileManager{
			files: []*os.File{},
		},
	}

	err := profiler.Start()
	assert.NoError(t, err, "Expected no error starting profiler")
	assert.Len(t, profiler.getFiles(), 1, "Expected one file to be created after starting profiler")

	profiler.cleanUp()
	assert.Len(t, profiler.getFiles(), 0, "Expected files to be cleaned up")
}
