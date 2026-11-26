// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugintest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

const (
	// fixturePackage is the import path of the fixture plugin's main package.
	// The go command builds a package under testdata when it is named
	// explicitly, and an import path works from any directory in the module.
	fixturePackage = "github.com/oakwood-commons/scafctl/pkg/plugin/testdata/lifecycleplugin"

	fixtureBinaryName = "lifecycleplugin"

	buildTimeout = 2 * time.Minute
)

// The fixture is built once per test process and shared by every test, as the
// other plugin fixtures in this repository are.
var (
	buildOnce sync.Once
	buildPath string
	buildErr  error
)

// BuildFixture compiles the fixture plugin, once per test process, and returns
// the path of the executable. It skips the test in short mode because it needs
// the go toolchain, and fails the test if the build fails.
func BuildFixture(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping plugin fixture test in short mode (requires go build)")
	}
	buildOnce.Do(func() {
		buildPath, buildErr = buildFixture()
	})
	if buildErr != nil {
		t.Fatalf("building the lifecycle fixture plugin: %v", buildErr)
	}
	return buildPath
}

func buildFixture() (string, error) {
	dir, err := os.MkdirTemp("", "scafctl-plugintest-*")
	if err != nil {
		return "", fmt.Errorf("create build dir: %w", err)
	}
	name := fixtureBinaryName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(dir, name)

	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, fixturePackage) //nolint:gosec // fixed package path
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build %s: %w\n%s", fixturePackage, err, output)
	}
	return out, nil
}
