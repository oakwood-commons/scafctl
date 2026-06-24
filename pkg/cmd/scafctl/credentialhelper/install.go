// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package credentialhelper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

const (
	// defaultBinDir is the default directory for the symlink.
	defaultBinDir = "~/.local/bin"
)

// shimSignature marks forwarding shims that this command generated,
// so uninstall can safely distinguish them from unrelated user files.
const shimSignature = "scafctl-managed forwarding shim"

// shimSignatureLinePOSIX and shimSignatureLineWindows are the exact comment
// lines embedded in generated shims. isManagedShim matches one of these lines
// exactly so an unrelated file cannot be mistaken for a managed shim.
const (
	shimSignatureLinePOSIX   = "# " + shimSignature
	shimSignatureLineWindows = "REM " + shimSignature
)

// shimHeaderScanBytes bounds how much of a candidate file isManagedShim reads
// when searching for the signature line. Generated shims carry the signature on
// the second line, so a small header is always sufficient.
const shimHeaderScanBytes = 512

// Install methods reported by installHelper.
const (
	methodSymlink = "symlink"
	methodShim    = "shim"
)

// credHelperName returns the Docker credential helper symlink name for the given binary.
func credHelperName(binaryName string) string {
	return "docker-credential-" + binaryName
}

func commandInstall(_ *terminal.IOStreams, path string) *cobra.Command {
	var (
		binDir   string
		docker   bool
		podman   bool
		registry string
	)

	cmd := &cobra.Command{
		Use:   "install",
		Short: fmt.Sprintf("Install %s as a Docker/Podman credential helper", path),
		Long: fmt.Sprintf(`Installs a %s credential helper and optionally configures
Docker or Podman to use %s as the credential store.

On Unix a symlink is created in --bin-dir (default ~/.local/bin). On Windows,
or when a symlink cannot be created without elevation, an elevation-free
forwarding shim is written instead. Either way --bin-dir must be on your PATH
for Docker/Podman to discover the helper.`, credHelperName(path), path),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)

			helperName := credHelperName(path)
			resolvedBinDir := expandHome(binDir)

			// Find the scafctl binary
			scafctlPath, err := findScafctlBinary()
			if err != nil {
				return fmt.Errorf("locate %s binary: %w", path, err)
			}

			// Create bin dir if needed
			if err := os.MkdirAll(resolvedBinDir, 0o755); err != nil {
				return fmt.Errorf("create bin directory %s: %w", resolvedBinDir, err)
			}

			// Install the helper: a symlink on Unix, or an elevation-free forwarding
			// shim on Windows (or as a fallback when symlinking is not permitted).
			linkPath := filepath.Join(resolvedBinDir, credHelperFileName(path, runtime.GOOS))
			method, err := installHelper(scafctlPath, linkPath, runtime.GOOS)
			if err != nil {
				return fmt.Errorf("install credential helper: %w", err)
			}
			w.Successf("Created %s %s -> %s\n", method, linkPath, scafctlPath)

			// Optionally configure Docker
			if docker {
				dockerConfig := dockerConfigPath()
				if err := updateContainerConfig(dockerConfig, registry, path); err != nil {
					return fmt.Errorf("update Docker config %s: %w", dockerConfig, err)
				}
				w.Successf("Updated %s\n", dockerConfig)
			}

			// Optionally configure Podman
			if podman {
				podmanConfig := podmanConfigPath()
				if err := updateContainerConfig(podmanConfig, registry, path); err != nil {
					return fmt.Errorf("update Podman config %s: %w", podmanConfig, err)
				}
				w.Successf("Updated %s\n", podmanConfig)
			}

			w.Infof("\nVerify with: %s list\n", helperName)
			return nil
		},
	}

	cmd.Flags().StringVar(&binDir, "bin-dir", defaultBinDir, "Directory for the credential helper symlink or shim")
	cmd.Flags().BoolVar(&docker, "docker", false, "Update ~/.docker/config.json")
	cmd.Flags().BoolVar(&podman, "podman", false, "Update Podman containers/auth.json")
	cmd.Flags().StringVar(&registry, "registry", "", "Configure per-registry credHelper instead of global credsStore")

	return cmd
}

func commandUninstall(_ *terminal.IOStreams, path string) *cobra.Command {
	var (
		binDir   string
		docker   bool
		podman   bool
		registry string
	)

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: fmt.Sprintf("Remove %s credential helper integration", path),
		Long: fmt.Sprintf(`Removes the %s symlink or managed shim and optionally removes
%s entries from Docker or Podman configuration.`, credHelperName(path), path),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)

			resolvedBinDir := expandHome(binDir)
			linkPath := filepath.Join(resolvedBinDir, credHelperFileName(path, runtime.GOOS))

			// Remove the installed helper. Symlinks (Unix) are removed directly;
			// regular files are removed only when they are a managed forwarding shim,
			// so an unrelated file the user placed here is never deleted.
			info, err := os.Lstat(linkPath)
			switch {
			case os.IsNotExist(err):
				w.Successf("Nothing to remove at %s\n", linkPath)
			case err != nil:
				return fmt.Errorf("stat %s: %w", linkPath, err)
			case info.Mode()&os.ModeSymlink != 0:
				if err := os.Remove(linkPath); err != nil {
					return fmt.Errorf("remove symlink %s: %w", linkPath, err)
				}
				w.Successf("Removed symlink %s\n", linkPath)
			default:
				managed, shimErr := isManagedShim(linkPath)
				if shimErr != nil {
					return fmt.Errorf("inspect %s: %w", linkPath, shimErr)
				}
				if !managed {
					return fmt.Errorf("refusing to remove non-symlink path %s", linkPath)
				}
				if err := os.Remove(linkPath); err != nil {
					return fmt.Errorf("remove shim %s: %w", linkPath, err)
				}
				w.Successf("Removed shim %s\n", linkPath)
			}

			// Optionally clean Docker config
			if docker {
				dockerConfig := dockerConfigPath()
				if err := removeFromContainerConfig(dockerConfig, registry, path); err != nil {
					return fmt.Errorf("update Docker config %s: %w", dockerConfig, err)
				}
				w.Successf("Cleaned %s\n", dockerConfig)
			}

			// Optionally clean Podman config
			if podman {
				podmanConfig := podmanConfigPath()
				if err := removeFromContainerConfig(podmanConfig, registry, path); err != nil {
					return fmt.Errorf("update Podman config %s: %w", podmanConfig, err)
				}
				w.Successf("Cleaned %s\n", podmanConfig)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&binDir, "bin-dir", defaultBinDir, "Directory where the symlink or shim was installed")
	cmd.Flags().BoolVar(&docker, "docker", false, fmt.Sprintf("Remove %s entries from ~/.docker/config.json", path))
	cmd.Flags().BoolVar(&podman, "podman", false, fmt.Sprintf("Remove %s entries from Podman containers/auth.json", path))
	cmd.Flags().StringVar(&registry, "registry", "", "Remove per-registry credHelper instead of global credsStore")

	return cmd
}

// findScafctlBinary resolves the absolute path of the running scafctl binary.
func findScafctlBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("get executable path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks: %w", err)
	}
	return resolved, nil
}

// createSymlink creates or replaces a symlink at linkPath pointing to target.
func createSymlink(target, linkPath string) error {
	// Check that the target resolves to an executable available on PATH.
	if _, err := exec.LookPath(target); err != nil {
		return fmt.Errorf("target %s is not executable: %w", target, err)
	}

	// Remove existing symlink if present
	if fi, err := os.Lstat(linkPath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(linkPath); err != nil {
				return fmt.Errorf("remove existing symlink: %w", err)
			}
		} else {
			return fmt.Errorf("%s exists and is not a symlink", linkPath)
		}
	}

	return os.Symlink(target, linkPath)
}

// credHelperFileName returns the on-disk file name for the credential helper.
// On Windows the helper is a ".cmd" shim so that Go's exec.LookPath (honoring
// PATHEXT) can discover it; elsewhere it is the bare alias name used by the
// symlink.
func credHelperFileName(binaryName, goos string) string {
	name := credHelperName(binaryName)
	if goos == "windows" {
		return name + ".cmd"
	}
	return name
}

// installHelper installs the credential helper at linkPath, returning the method
// used (methodSymlink or methodShim). On Windows it always writes a forwarding
// shim (symlinks require elevation). On Unix it prefers a symlink and falls back
// to a shim when symlink creation is not permitted.
func installHelper(target, linkPath, goos string) (string, error) {
	if goos == "windows" {
		if err := writeShim(target, linkPath, goos); err != nil {
			return "", err
		}
		return methodShim, nil
	}
	if err := createSymlink(target, linkPath); err != nil {
		if shimErr := writeShim(target, linkPath, goos); shimErr != nil {
			return "", fmt.Errorf("symlink failed (%w) and shim fallback failed: %w", err, shimErr)
		}
		return methodShim, nil
	}
	return methodSymlink, nil
}

// shimContent returns the forwarding-shim script body for the given OS. The shim
// delegates to the resolved binary's "credential-helper" subcommand and carries
// a signature line so uninstall can recognize files it generated.
func shimContent(target, goos string) string {
	if goos == "windows" {
		return "@echo off\r\n" + shimSignatureLineWindows + "\r\n\"" + target + "\" credential-helper %*\r\n"
	}
	return "#!/bin/sh\n" + shimSignatureLinePOSIX + "\nexec \"" + target + "\" credential-helper \"$@\"\n"
}

// writeShim writes a forwarding shim at shimPath pointing to target. An existing
// symlink or a previously generated managed shim is replaced; any other regular
// file is left untouched and an error is returned, so an unrelated helper the
// user placed at this path is never clobbered.
func writeShim(target, shimPath, goos string) error {
	if _, err := exec.LookPath(target); err != nil {
		return fmt.Errorf("target %s is not executable: %w", target, err)
	}
	if fi, err := os.Lstat(shimPath); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			managed, mErr := isManagedShim(shimPath)
			if mErr != nil {
				return fmt.Errorf("inspect existing %s: %w", shimPath, mErr)
			}
			if !managed {
				return fmt.Errorf("%s exists and is not a managed shim", shimPath)
			}
		}
		if err := os.Remove(shimPath); err != nil {
			return fmt.Errorf("remove existing %s: %w", shimPath, err)
		}
	}
	mode := os.FileMode(0o755)
	if goos == "windows" {
		mode = 0o644
	}
	if err := os.WriteFile(shimPath, []byte(shimContent(target, goos)), mode); err != nil {
		return fmt.Errorf("write shim %s: %w", shimPath, err)
	}
	return nil
}

// isManagedShim reports whether path is a forwarding shim generated by this
// command, identified by an exact signature line within the file header. Only a
// bounded header is read, so large unrelated files are cheap to reject and
// cannot be mis-identified by an incidental substring match deeper in the file.
func isManagedShim(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	header := make([]byte, shimHeaderScanBytes)
	n, err := io.ReadFull(f, header)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	for _, line := range strings.Split(string(header[:n]), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == shimSignatureLinePOSIX || line == shimSignatureLineWindows {
			return true, nil
		}
	}
	return false, nil
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}

func dockerConfigPath() string {
	if v := os.Getenv("DOCKER_CONFIG"); v != "" {
		return filepath.Join(v, "config.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".docker", "config.json")
}

func podmanConfigPath() string {
	// Podman auth file locations, in order:
	// 1. $XDG_RUNTIME_DIR/containers/auth.json (Linux)
	// 2. ~/.config/containers/auth.json
	if runtime.GOOS == "linux" {
		if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
			return filepath.Join(xdg, "containers", "auth.json")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "containers", "auth.json")
}

// updateContainerConfig updates a Docker/Podman config file to use the given binary as credential helper.
func updateContainerConfig(configPath, registry, binaryName string) error {
	cfg, err := readContainerConfig(configPath)
	if err != nil {
		return err
	}

	if registry != "" {
		// Per-registry credHelper
		credHelpers, ok := cfg["credHelpers"].(map[string]interface{})
		if !ok {
			credHelpers = make(map[string]interface{})
		}
		credHelpers[registry] = binaryName
		cfg["credHelpers"] = credHelpers
	} else {
		// Global credsStore
		cfg["credsStore"] = binaryName
	}

	return writeContainerConfig(configPath, cfg)
}

// removeFromContainerConfig removes credential helper entries from a Docker/Podman config.
func removeFromContainerConfig(configPath, registry, binaryName string) error {
	cfg, err := readContainerConfig(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if registry != "" {
		if credHelpers, ok := cfg["credHelpers"].(map[string]interface{}); ok {
			delete(credHelpers, registry)
			if len(credHelpers) == 0 {
				delete(cfg, "credHelpers")
			}
		}
	} else {
		if store, ok := cfg["credsStore"].(string); ok && store == binaryName {
			delete(cfg, "credsStore")
		}
	}

	return writeContainerConfig(configPath, cfg)
}

func readContainerConfig(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]interface{}), nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

func writeContainerConfig(path string, cfg map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "\t")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Write atomically via temp file
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := filepath.Clean(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName) //nolint:gosec // tmpName from os.CreateTemp is safe
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName) //nolint:gosec // tmpName from os.CreateTemp is safe
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil { //nolint:gosec // tmpName from os.CreateTemp is safe
		os.Remove(tmpName) //nolint:gosec // tmpName from os.CreateTemp is safe
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil { //nolint:gosec // tmpName from os.CreateTemp is safe
		os.Remove(tmpName) //nolint:gosec // tmpName from os.CreateTemp is safe
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
