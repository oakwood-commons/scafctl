// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import "errors"

// Sentinels for pull reference validation failures. They let callers and
// tests match on identity via errors.Is instead of error message text.
// Message-embedding call sites wrap them with %w so the full user-facing
// message is preserved.

// ErrRemoteReferenceRequired is returned when a root catalog command (pull,
// push, delete) is given a short reference: it only accepts full remote
// references, while short references belong to the kind subcommands.
var ErrRemoteReferenceRequired = errors.New("root catalog command only accepts full remote references")

// ErrUnknownKind is returned when a root catalog command (pull, push, delete)
// is given a first argument that is not a registered artifact kind alongside
// further arguments, which is almost always a mistyped kind subcommand.
var ErrUnknownKind = errors.New("unknown artifact kind")

// ErrVersionConstraintConflict is returned when a --version constraint is
// combined with an explicit version in the reference itself; the two version
// sources are ambiguous.
var ErrVersionConstraintConflict = errors.New("cannot use --version with an explicit version")

// ErrCatalogWithRemoteReference is returned when --catalog is combined with a
// full remote reference; the reference already names its registry, so the
// flag has nothing to override.
var ErrCatalogWithRemoteReference = errors.New("cannot use --catalog with a full remote reference")

// ErrOriginWithCatalog is returned when --origin (local artifact selector) is
// combined with --catalog (remote target); the two address different stores.
var ErrOriginWithCatalog = errors.New("cannot use --origin with --catalog: --origin selects a local artifact, --catalog targets a remote registry")
