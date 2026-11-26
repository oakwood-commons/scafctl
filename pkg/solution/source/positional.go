// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package source

import (
	"errors"
	"fmt"
)

// ErrFileAndPositional is returned when a command receives both -f/--file and
// a positional solution reference.
var ErrFileAndPositional = errors.New("cannot use both -f/--file and a positional argument")

// ErrFileAndSolution is returned when a command receives both -f/--file and
// -s/--solution.
var ErrFileAndSolution = errors.New("cannot use both -f/--file and -s/--solution")

// ErrSolutionAndPositional is returned when a command receives both
// -s/--solution and a positional solution reference.
var ErrSolutionAndPositional = errors.New("cannot use both -s/--solution and a positional solution reference")

// IsReference reports whether k is fetched by reference (URL, registry, or
// catalog) rather than read from the local filesystem or stdin.
func (k Kind) IsReference() bool {
	switch k {
	case KindURL, KindRemoteRef, KindCatalogQualified, KindBareName:
		return true
	case KindUnknown, KindAutoDiscover, KindStdin, KindLocalFile:
		return false
	default:
		return false
	}
}

// IsUnversionedName reports whether c is a bare catalog name with no version
// (e.g. "deploy"), which is indistinguishable from an action or resolver name.
func (c Classification) IsUnversionedName() bool {
	return c.Kind == KindBareName && !hasPinnedVersion(c)
}

// ValidatePositionalRef classifies a positional CLI argument that must be a
// solution reference (URL, registry, or catalog name). It returns
// ErrFileAndPositional when fileFlag is also set, ErrInvalidRef when arg is a
// malformed reference, and an error suggesting "<cmdUsage> -f <arg>" when arg
// is a local path or stdin.
func ValidatePositionalRef(arg, fileFlag, cmdUsage string) (Classification, error) {
	if fileFlag != "" {
		return Classification{}, ErrFileAndPositional
	}
	c, err := Classify(arg)
	if err != nil {
		return Classification{}, err
	}
	if !c.Kind.IsReference() {
		return Classification{}, fmt.Errorf("local file paths must use -f/--file flag: %s -f %s", cmdUsage, arg)
	}
	return c, nil
}

// ValidateFileFlagPositional classifies a -f/--file flag value on commands
// whose positional argument is a solution reference. An empty fileFlag is a
// no-op. Malformed reference syntax returns ErrInvalidRef; catalog, registry,
// or URL references are rejected with a hint to pass the reference positionally.
func ValidateFileFlagPositional(fileFlag, cmdUsage string) (Classification, error) {
	if fileFlag == "" {
		return Classification{}, nil
	}
	c, err := Classify(fileFlag)
	if err != nil {
		return Classification{}, err
	}
	if c.Kind.IsReference() {
		return Classification{}, fmt.Errorf("-f/--file accepts only local file paths or \"-\" for stdin (got %s %q); pass references positionally: %s %s", c.Kind, fileFlag, cmdUsage, fileFlag)
	}
	return c, nil
}

// ValidateFileFlagSolution classifies a -f/--file flag value on commands whose
// positional arguments name actions or resolvers and that expose -s/--solution
// for solution references. An empty fileFlag is a no-op. Malformed reference
// syntax returns ErrInvalidRef; catalog, registry, or URL references are
// rejected with a hint to pass -s/--solution.
func ValidateFileFlagSolution(fileFlag, cmdUsage string) (Classification, error) {
	if fileFlag == "" {
		return Classification{}, nil
	}
	c, err := Classify(fileFlag)
	if err != nil {
		return Classification{}, err
	}
	if c.Kind.IsReference() {
		return Classification{}, fmt.Errorf("-f/--file accepts only local file paths or \"-\" for stdin (got %s %q); use -s/--solution: %s -s %s", c.Kind, fileFlag, cmdUsage, fileFlag)
	}
	return c, nil
}

// ValidateSolutionFlag classifies a -s/--solution flag value that must be a
// solution reference (catalog name, registry reference, or URL). It returns
// ErrFileAndSolution when fileFlag is also set, ErrInvalidRef when solutionFlag
// is malformed, and an error suggesting "<cmdUsage> -f <val>" when solutionFlag
// is a local file path or stdin.
func ValidateSolutionFlag(solutionFlag, fileFlag, cmdUsage string) (Classification, error) {
	if fileFlag != "" {
		return Classification{}, ErrFileAndSolution
	}
	c, err := Classify(solutionFlag)
	if err != nil {
		return Classification{}, err
	}
	if !c.Kind.IsReference() {
		return Classification{}, fmt.Errorf("-s/--solution accepts catalog names, registry references, or URLs; for local files use -f/--file: %s -f %s", cmdUsage, solutionFlag)
	}
	return c, nil
}
