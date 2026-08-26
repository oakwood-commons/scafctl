// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package examplefiles embeds the scafctl example tree in place so that the
// examples ship inside the compiled binary AND inside the published Go module,
// with no build-time copy step.
//
// This package lives in the examples/ directory precisely so that its
// //go:embed directive can reach the example files: a directive can only match
// files in its own package directory or subdirectories, so the embedding
// package must sit at the root of the tree it embeds. The pkg/examples package
// consumes FS to read and list examples.
package examplefiles

import "embed"

// FS holds every example file under examples/ (recursively). The "*" pattern
// matches all top-level entries and descends into subdirectories; files whose
// names begin with "." or "_" are excluded per go:embed rules, which is fine --
// examples are ordinary YAML/Markdown/Go files. This package's own Go source
// (embed.go) is also embedded but is inert: the example scanner only reads
// .yaml/.yml files.
//
//go:embed *
var FS embed.FS
