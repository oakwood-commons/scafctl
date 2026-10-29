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
