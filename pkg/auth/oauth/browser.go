// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser opens a URL in the default system browser.
// Returns an error if the platform is unsupported or the command fails to start.
// The browser process runs asynchronously — this function returns as soon as
// the command is launched.
func OpenBrowser(ctx context.Context, url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return exec.CommandContext(ctx, cmd, args...).Start() //nolint:gosec // URL is from trusted internal config
}
