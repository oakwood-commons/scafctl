// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package credentialhelper

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

const (
	// symlinkName is the name of the Docker credential helper symlink.
	symlinkName = "docker-credential-" + settings.CliBinaryName

	// defaultBinDir is the default directory for the symlink.
	defaultBinDir = "~/.local/bin"
)

func commandInstall(ioStreams *terminal.IOStreams) *cobra.Command {
	var (
		binDir   string
		docker   bool
		podman   bool
		registry string
	)

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install scafctl as a Docker/Podman credential helper",
		Long: `Creates a docker-credential-scafctl symlink and optionally configures
Docker or Podman to use scafctl as the credential store.

The symlink is placed in --bin-dir (default ~/.local/bin) and must be on
your PATH for Docker/Podman to discover it.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)

			resolvedBinDir := expandHome(binDir)

			// Find the scafctl binary
			scafctlPath, err := findScafctlBinary()
			if err != nil {
				return fmt.Errorf("locate scafctl binary: %w", err)
			}

			// Create bin dir if needed
			if err := os.MkdirAll(resolvedBinDir, 0o755); err != nil {
				return fmt.Errorf("create bin directory %s: %w", resolvedBinDir, err)
			}

			// Create (or replace) the symlink
			linkPath := filepath.Join(resolvedBinDir, symlinkName)
			if err := createSymlink(scafctlPath, linkPath); err != nil {
				return fmt.Errorf("create symlink: %w", err)
			}
			w.Successf("Created symlink %s -> %s\n", linkPath, scafctlPath)

			// Optionally configure Docker
			if docker {
				dockerConfig := dockerConfigPath()
				if err := updateContainerConfig(dockerConfig, registry, ioStreams); err != nil {
					return fmt.Errorf("update Docker config %s: %w", dockerConfig, err)
				}
				w.Successf("Updated %s\n", dockerConfig)
			}

			// Optionally configure Podman
			if podman {
				podmanConfig := podmanConfigPath()
				if err := updateContainerConfig(podmanConfig, registry, ioStreams); err != nil {
					return fmt.Errorf("update Podman config %s: %w", podmanConfig, err)
				}
				w.Successf("Updated %s\n", podmanConfig)
			}

			w.Infof("\nVerify with: %s list\n", symlinkName)
			return nil
		},
	}

	cmd.Flags().StringVar(&binDir, "bin-dir", defaultBinDir, "Directory for the credential helper symlink")
	cmd.Flags().BoolVar(&docker, "docker", false, "Update ~/.docker/config.json")
	cmd.Flags().BoolVar(&podman, "podman", false, "Update Podman containers/auth.json")
	cmd.Flags().StringVar(&registry, "registry", "", "Configure per-registry credHelper instead of global credsStore")

	return cmd
}

func commandUninstall(ioStreams *terminal.IOStreams) *cobra.Command {
	var (
		binDir   string
		docker   bool
		podman   bool
		registry string
	)

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove scafctl credential helper integration",
		Long: `Removes the docker-credential-scafctl symlink and optionally removes
scafctl entries from Docker or Podman configuration.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)

			resolvedBinDir := expandHome(binDir)
			linkPath := filepath.Join(resolvedBinDir, symlinkName)

			// Remove symlink only when the path is actually a symlink.
			// Refusing to remove regular files mirrors the safety checks in createSymlink.
			info, err := os.Lstat(linkPath)
			if os.IsNotExist(err) {
				w.Successf("Removed symlink %s\n", linkPath)
			} else {
				if err != nil {
					return fmt.Errorf("stat symlink %s: %w", linkPath, err)
				}
				if info.Mode()&os.ModeSymlink == 0 {
					return fmt.Errorf("refusing to remove non-symlink path %s", linkPath)
				}
				if err := os.Remove(linkPath); err != nil {
					return fmt.Errorf("remove symlink %s: %w", linkPath, err)
				}
				w.Successf("Removed symlink %s\n", linkPath)
			}

			// Optionally clean Docker config
			if docker {
				dockerConfig := dockerConfigPath()
				if err := removeFromContainerConfig(dockerConfig, registry, ioStreams); err != nil {
					return fmt.Errorf("update Docker config %s: %w", dockerConfig, err)
				}
				w.Successf("Cleaned %s\n", dockerConfig)
			}

			// Optionally clean Podman config
			if podman {
				podmanConfig := podmanConfigPath()
				if err := removeFromContainerConfig(podmanConfig, registry, ioStreams); err != nil {
					return fmt.Errorf("update Podman config %s: %w", podmanConfig, err)
				}
				w.Successf("Cleaned %s\n", podmanConfig)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&binDir, "bin-dir", defaultBinDir, "Directory where the symlink was installed")
	cmd.Flags().BoolVar(&docker, "docker", false, "Remove scafctl entries from ~/.docker/config.json")
	cmd.Flags().BoolVar(&podman, "podman", false, "Remove scafctl entries from Podman containers/auth.json")
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

// updateContainerConfig updates a Docker/Podman config file to use scafctl.
func updateContainerConfig(configPath, registry string, _ *terminal.IOStreams) error {
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
		credHelpers[registry] = settings.CliBinaryName
		cfg["credHelpers"] = credHelpers
	} else {
		// Global credsStore
		cfg["credsStore"] = settings.CliBinaryName
	}

	return writeContainerConfig(configPath, cfg)
}

// removeFromContainerConfig removes scafctl entries from a Docker/Podman config.
func removeFromContainerConfig(configPath, registry string, _ *terminal.IOStreams) error {
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
		if store, ok := cfg["credsStore"].(string); ok && store == settings.CliBinaryName {
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
