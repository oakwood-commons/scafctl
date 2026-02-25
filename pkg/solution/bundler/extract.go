// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	actionpkg "github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// TraceResolverDeps performs static analysis on a resolver to find referenced files,
// following rslvr: bindings transitively.
func TraceResolverDeps(name string, sol *solution.Solution, visited map[string]bool) []string {
	if visited[name] {
		return nil
	}
	visited[name] = true

	r, exists := sol.Spec.Resolvers[name]
	if !exists || r == nil {
		return nil
	}

	var files []string

	// Check resolve.with
	if r.Resolve != nil {
		for _, ps := range r.Resolve.With {
			files = append(files, ExtractProviderStepFiles(ps.Provider, ps.Inputs)...)
			// Follow rslvr: bindings in inputs
			for _, vr := range ps.Inputs {
				if vr != nil && vr.Resolver != nil {
					depName := ExtractResolverName(*vr.Resolver)
					files = append(files, TraceResolverDeps(depName, sol, visited)...)
				}
			}
		}
	}

	// Check transform.with
	if r.Transform != nil {
		for _, pt := range r.Transform.With {
			files = append(files, ExtractProviderStepFiles(pt.Provider, pt.Inputs)...)
			for _, vr := range pt.Inputs {
				if vr != nil && vr.Resolver != nil {
					depName := ExtractResolverName(*vr.Resolver)
					files = append(files, TraceResolverDeps(depName, sol, visited)...)
				}
			}
		}
	}

	// Check validate.with
	if r.Validate != nil {
		for _, pv := range r.Validate.With {
			files = append(files, ExtractProviderStepFiles(pv.Provider, pv.Inputs)...)
			for _, vr := range pv.Inputs {
				if vr != nil && vr.Resolver != nil {
					depName := ExtractResolverName(*vr.Resolver)
					files = append(files, TraceResolverDeps(depName, sol, visited)...)
				}
			}
		}
	}

	return files
}

// ExtractActionFiles traces files referenced by an action.
func ExtractActionFiles(a *actionpkg.Action) []string {
	if a == nil {
		return nil
	}
	return ExtractProviderStepFiles(a.Provider, a.Inputs)
}

// ExtractProviderStepFiles returns file paths referenced by a provider step's inputs.
func ExtractProviderStepFiles(provider string, inputs map[string]*spec.ValueRef) []string {
	var files []string
	switch provider {
	case "file":
		if path := ExtractLiteralFromInputs(inputs, "path"); path != "" && IsLocalFilePath(path) {
			files = append(files, path)
		}
	case "solution":
		if source := ExtractLiteralFromInputs(inputs, "source"); source != "" && IsLocalFilePath(source) {
			files = append(files, source)
		}
	}
	return files
}

// ExtractLiteralFromInputs returns the literal string value for the given key, or empty string.
func ExtractLiteralFromInputs(inputs map[string]*spec.ValueRef, key string) string {
	if inputs == nil {
		return ""
	}
	vr := inputs[key]
	if vr == nil {
		return ""
	}
	if vr.Expr != nil || vr.Tmpl != nil || vr.Resolver != nil {
		return ""
	}
	s, ok := vr.Literal.(string)
	if !ok {
		return ""
	}
	return s
}

// IsLocalFilePath returns true if path looks like a local relative file path
// (not a URL, registry reference, or absolute path).
func IsLocalFilePath(path string) bool {
	if path == "" {
		return false
	}
	if strings.Contains(path, "://") {
		return false
	}
	if strings.Contains(path, "@") {
		return false
	}
	if filepath.IsAbs(path) {
		return false
	}
	return true
}

// ExtractResolverName strips any selector from a resolver reference,
// e.g. "myResolver.result" → "myResolver".
func ExtractResolverName(name string) string {
	if idx := strings.Index(name, "."); idx != -1 {
		return name[:idx]
	}
	return name
}

// CopyFile copies a file from src to dst, creating parent directories as needed.
func CopyFile(src, dst string) error {
	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
