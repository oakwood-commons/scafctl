// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitcredential

import "strings"

const (
	// AliasPrefix is the executable basename prefix Git uses when invoking a
	// short-named credential helper ("credential.helper = <name>" runs
	// "git-credential-<name> <verb>").
	AliasPrefix = "git-credential-"

	// CommandName is the Cobra subcommand that handles Git credential verbs.
	CommandName = "git-credential"
)

// RewriteAliasArgs detects whether argv[0] was invoked as
// "git-credential-<binary>[.exe|.cmd]" and, if so, inserts "git-credential"
// ahead of the helper verb so Cobra routes into the git-credential subcommand.
func RewriteAliasArgs(argv []string) (rewritten []string, isHelperAlias bool) {
	if len(argv) == 0 {
		return argv, false
	}
	base := baseExecutableName(argv[0])
	if !strings.HasPrefix(strings.ToLower(base), AliasPrefix) || len(base) <= len(AliasPrefix) {
		return argv, false
	}

	return append([]string{argv[0], CommandName}, argv[1:]...), true
}

func baseExecutableName(arg0 string) string {
	idx := strings.LastIndexAny(arg0, `/\`)
	base := arg0
	if idx >= 0 {
		base = arg0[idx+1:]
	}
	lower := strings.ToLower(base)
	switch {
	case strings.HasSuffix(lower, ".exe"):
		base = base[:len(base)-4]
	case strings.HasSuffix(lower, ".cmd"):
		base = base[:len(base)-4]
	}
	return base
}
