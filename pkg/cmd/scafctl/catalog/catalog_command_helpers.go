package catalog

import (
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	catversion "github.com/oakwood-commons/scafctl/pkg/catalog/version"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/shellexec"
)

// looksLikeRemoteReference returns true if the reference appears to be a remote registry URL.
// Remote references contain a registry host with a dot (e.g., "ghcr.io", "docker.io")
// or start with "oci://", "localhost:", or contain a port.
func looksLikeRemoteReference(ref string) bool {
	return catalog.LooksLikeRemoteReference(ref)
}

// validateTargetName validates a target artifact name and ensures it is a concrete
// name@version, not a bare name, latest alias, or digest pin.
func validateTargetName(ref catalog.Reference, targetName string) error {
	if targetName == "" {
		return nil
	}

	_, err := catalog.ResolveLocalTarget(ref, targetName)
	return err
}

// validateVersionConstraint checks for conflicts between an explicit @version
// in the reference name and a --version constraint flag. Returns an error if
// both are set, which would be ambiguous.
func validateVersionConstraint(nameOrRef, versionConstraint string) error {
	if versionConstraint == "" {
		return nil
	}

	// TODO: only "@" is detected, so a tag-pinned remote ref such as
	// "registry/name:1.0.0" with --version "^2" slips through and the
	// constraint is silently ignored or conflicts with the tag. Parse the
	// reference (version, tag, or digest) instead of string-scanning for "@".
	if idx := strings.LastIndex(nameOrRef, "@"); idx > 0 {
		return fmt.Errorf("%w in reference %q; use one or the other", ErrVersionConstraintConflict, nameOrRef)
	}

	return catversion.ValidateConstraint(versionConstraint)
}

// cliBinaryName returns the CLI binary name for rendered commands, falling
// back to the default when the embedder did not configure one.
func cliBinaryName(cliParams *settings.Run) string {
	if cliParams != nil && cliParams.BinaryName != "" {
		return cliParams.BinaryName
	}
	return settings.CliBinaryName
}

// shellUnsafeChars are the characters that make a value need quoting before it
// can be pasted into a POSIX shell as a single word.
const shellUnsafeChars = " \t\n\"'`$&|;<>(){}[]*?!#~\\"

// originHint tells the user how to pick one of the origins an ambiguous query
// matched. It names the flag and its values rather than rebuilding the whole
// command, so the user re-runs their own command with every other flag intact.
func originHint(ambiguous *catalog.AmbiguousArtifactError) string {
	seen := make(map[string]struct{})
	var options []string
	for _, c := range ambiguous.Candidates() {
		if _, ok := seen[c.Origin]; ok {
			continue
		}
		seen[c.Origin] = struct{}{}
		options = append(options, "--origin "+quoteIfNeeded(c.Origin))
	}
	return "re-run the same command with one of: " + strings.Join(options, ", ")
}

// quoteIfNeeded shell-quotes s only when it is empty or contains characters a
// shell would split or interpret, so ordinary values stay readable.
func quoteIfNeeded(s string) string {
	if s != "" && !strings.ContainsAny(s, shellUnsafeChars) {
		return s
	}
	return shellexec.ShellQuote(s)
}
