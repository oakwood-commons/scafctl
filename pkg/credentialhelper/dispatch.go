// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package credentialhelper

import (
	"path/filepath"
	"strings"
)

const (
	// AliasPrefix is the basename prefix Docker/Podman use when invoking a
	// credential helper binary (e.g. "docker-credential-scafctl").
	AliasPrefix = "docker-credential-"

	// CommandName is the cobra subcommand path that implements the helper
	// protocol verbs (get/store/erase/list).
	CommandName = "credential-helper"
)

// RewriteAliasArgs detects whether the process was invoked under a
// docker-credential-<name> alias and, if so, returns a copy of argv rewritten
// so the helper verb routes into the "credential-helper <verb>" command tree.
//
// Docker/Podman invoke a credential helper as:
//
//	docker-credential-<name> get      # server URL on stdin, JSON cred on stdout
//	docker-credential-<name> store
//	docker-credential-<name> erase
//	docker-credential-<name> list
//
// The binary's argv therefore looks like {"docker-credential-<name>", "get"},
// which a normal cobra root command cannot route (there is no top-level "get").
// RewriteAliasArgs turns that into {"docker-credential-<name>", "credential-helper", "get"}.
//
// The returned slice preserves the input shape: element 0 is the original
// program name (argv[0]), and the helper subcommand is inserted ahead of the
// verb. Callers wiring this into cobra should pass rewritten[1:] to
// (*cobra.Command).SetArgs, since cobra expects args without the program name.
//
// When the process was not invoked under an alias, RewriteAliasArgs returns the
// original argv unchanged and isHelperAlias=false. The function is pure and has
// no side effects, making it safe to call at the very top of main.
func RewriteAliasArgs(argv []string) (rewritten []string, isHelperAlias bool) {
	if len(argv) == 0 {
		return argv, false
	}
	base := filepath.Base(argv[0])
	// Strip a Windows executable suffix so docker-credential-mybin.exe matches.
	base = strings.TrimSuffix(base, ".exe")
	if !strings.HasPrefix(base, AliasPrefix) {
		return argv, false
	}

	out := make([]string, 0, len(argv)+1)
	out = append(out, argv[0])
	out = append(out, CommandName)
	out = append(out, argv[1:]...)
	return out, true
}
