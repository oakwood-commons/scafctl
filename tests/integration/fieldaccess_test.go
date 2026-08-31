// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// This test performs a type-aware static analysis of the whole module to guard
// how solution.PluginDependency.Name is accessed. Name is deliberately
// overloaded -- it is EITHER a short catalog name OR a full OCI reference -- so
// callers are expected to derive meaning through the accessor methods
// (ArtifactName, Registry, HasRegistry) rather than reading the raw field.
//
// The analysis resolves the exact *types.Var for PluginDependency.Name and
// walks every selector expression in the module, using go/types selection info
// so that a `.Name` on any OTHER struct is never mistaken for this field.
const (
	fieldGuardModulePattern = "github.com/oakwood-commons/scafctl/..."
	fieldGuardPkgPath       = "github.com/oakwood-commons/scafctl/pkg/solution"
	fieldGuardTypeName      = "PluginDependency"
	fieldGuardFieldName     = "Name"
)

// fieldGuardAllowedPkgs are packages permitted to read
// PluginDependency.Name directly. Only the defining package is allowed, because
// the LocalName/ArtifactName/Registry/HasRegistry/DisplayName accessors
// legitimately read the raw field.
//
// The migration is complete: every other package reaches the name through an
// accessor, so any NEW package that starts reading the raw field directly fails
// the guard and is steered toward the accessor methods instead.
var fieldGuardAllowedPkgs = map[string]bool{
	"github.com/oakwood-commons/scafctl/pkg/solution": true,
}

// fieldAccess is a resolved read of the guarded field at a source position.
type fieldAccess struct {
	Pkg string
	Pos token.Position
}

// TestPluginDependencyName_DirectAccessGuard fails when a package outside the
// allowlist reads solution.PluginDependency.Name directly instead of using the
// LocalName()/ArtifactName()/Registry()/HasRegistry()/DisplayName() accessors.
func TestPluginDependencyName_DirectAccessGuard(t *testing.T) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedSyntax | packages.NeedImports | packages.NeedDeps,
		Tests: false, // production code only; test files may assert on the field
	}
	pkgs, err := packages.Load(cfg, fieldGuardModulePattern)
	require.NoError(t, err)
	require.Zero(t, packages.PrintErrors(pkgs), "packages failed to load cleanly")

	target := findStructField(pkgs, fieldGuardPkgPath, fieldGuardTypeName, fieldGuardFieldName)
	require.NotNil(t, target, "could not resolve %s.%s.%s",
		fieldGuardPkgPath, fieldGuardTypeName, fieldGuardFieldName)

	var violations []fieldAccess
	for _, p := range pkgs {
		if fieldGuardAllowedPkgs[p.PkgPath] || p.TypesInfo == nil {
			continue
		}
		for _, file := range p.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				s, ok := p.TypesInfo.Selections[sel]
				if !ok || s.Kind() != types.FieldVal {
					return true
				}
				// Pointer identity: exact field of the exact struct type.
				if s.Obj() == target {
					violations = append(violations, fieldAccess{
						Pkg: p.PkgPath,
						Pos: p.Fset.Position(sel.Sel.Pos()),
					})
				}
				return true
			})
		}
	}

	assert.Empty(t, violations,
		"%s.%s.%s is read directly outside the allowlist; use ArtifactName()/Registry()/HasRegistry() instead:\n%s",
		fieldGuardTypeName, "", fieldGuardFieldName, formatFieldAccesses(violations))
}

// findStructField resolves typeName.fieldName within pkgPath to its *types.Var.
func findStructField(pkgs []*packages.Package, pkgPath, typeName, fieldName string) *types.Var {
	for _, p := range pkgs {
		if p.PkgPath != pkgPath || p.Types == nil {
			continue
		}
		obj := p.Types.Scope().Lookup(typeName)
		if obj == nil {
			continue
		}
		named, ok := obj.Type().(*types.Named)
		if !ok {
			return nil
		}
		st, ok := named.Underlying().(*types.Struct)
		if !ok {
			return nil
		}
		for i := range st.NumFields() {
			if f := st.Field(i); f.Name() == fieldName {
				return f
			}
		}
	}
	return nil
}

// formatFieldAccesses renders a stable, sorted list of offending accesses.
func formatFieldAccesses(hits []fieldAccess) string {
	lines := make([]string, 0, len(hits))
	for _, h := range hits {
		lines = append(lines, "  "+h.Pos.String()+"  ("+h.Pkg+")")
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
