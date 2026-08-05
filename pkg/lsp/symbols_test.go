// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// symbolsFixture exercises all four symbol kinds: two resolvers, a call, an
// author function, and a workflow action.
const symbolsFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: symbols
spec:
  calls:
    fetch:
      provider: message
      inputs:
        message: fetching
  functions:
    greet:
      params:
        - name: who
      template: "hello {{ .args.who }}"
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    appName:
      resolve:
        with:
          - call: fetch
  workflow:
    actions:
      deploy:
        provider: message
        inputs:
          message: deploying
`

// childByName returns the child symbol with the given name, failing the test if
// absent.
func childByName(t *testing.T, syms []protocol.DocumentSymbol, name string) protocol.DocumentSymbol {
	t.Helper()
	for _, s := range syms {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no symbol named %q in %v", name, symbolNames(syms))
	return protocol.DocumentSymbol{}
}

func symbolNames(syms []protocol.DocumentSymbol) []string {
	names := make([]string, 0, len(syms))
	for _, s := range syms {
		names = append(names, s.Name)
	}
	return names
}

func TestDocumentSymbols_AllKindsAndHierarchy(t *testing.T) {
	syms := DocumentSymbols([]byte(symbolsFixture))
	require.Len(t, syms, 1, "a single spec root")

	spec := syms[0]
	assert.Equal(t, "spec", spec.Name)
	assert.Equal(t, protocol.SymbolKindNamespace, spec.Kind)

	// Groups are present in the fixed order and only when non-empty.
	assert.Equal(t, []string{"resolvers", "actions", "calls", "functions"}, symbolNames(spec.Children))

	resolvers := childByName(t, spec.Children, "resolvers")
	assert.Equal(t, protocol.SymbolKindNamespace, resolvers.Kind)
	// Leaves are in source order: environment (line 15) before appName (line 20).
	assert.Equal(t, []string{"environment", "appName"}, symbolNames(resolvers.Children))
	for _, leaf := range resolvers.Children {
		assert.Equal(t, protocol.SymbolKindField, leaf.Kind, "resolver leaf kind")
	}

	actions := childByName(t, spec.Children, "actions")
	assert.Equal(t, []string{"deploy"}, symbolNames(actions.Children))
	assert.Equal(t, protocol.SymbolKindFunction, actions.Children[0].Kind)

	calls := childByName(t, spec.Children, "calls")
	assert.Equal(t, []string{"fetch"}, symbolNames(calls.Children))
	assert.Equal(t, protocol.SymbolKindMethod, calls.Children[0].Kind)

	functions := childByName(t, spec.Children, "functions")
	assert.Equal(t, []string{"greet"}, symbolNames(functions.Children))
	assert.Equal(t, protocol.SymbolKindFunction, functions.Children[0].Kind)
}

func TestDocumentSymbols_RangesMatchDefinitions(t *testing.T) {
	content := []byte(symbolsFixture)
	_, idx, err := loadIndex(content)
	require.NoError(t, err)

	syms := DocumentSymbols(content)
	require.Len(t, syms, 1)
	resolvers := childByName(t, syms[0].Children, "resolvers")

	// Each leaf's selection range equals the definition's positioned range.
	env := childByName(t, resolvers.Children, "environment")
	def, ok := idx.Definition(refindex.SymbolResolver, "environment")
	require.True(t, ok)
	assert.Equal(t, toLSPRange(def.Range), env.SelectionRange)
	assert.Equal(t, toLSPRange(def.Range), env.Range)

	// A group range encloses all of its children.
	for _, leaf := range resolvers.Children {
		assert.True(t, leaf.Range.Start.Line >= resolvers.Range.Start.Line,
			"child starts at/after group start")
		assert.True(t, leaf.Range.End.Line <= resolvers.Range.End.Line,
			"child ends at/before group end")
	}
	// The spec root encloses every group.
	spec := syms[0]
	assert.True(t, resolvers.Range.Start.Line >= spec.Range.Start.Line)
	assert.True(t, resolvers.Range.End.Line <= spec.Range.End.Line)
}

func TestDocumentSymbols_OmitsEmptyGroups(t *testing.T) {
	// Only resolvers -- the other three groups must be absent.
	const onlyResolvers = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: only-resolvers
spec:
  resolvers:
    a:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
`
	syms := DocumentSymbols([]byte(onlyResolvers))
	require.Len(t, syms, 1)
	assert.Equal(t, []string{"resolvers"}, symbolNames(syms[0].Children))
}

func TestDocumentSymbols_EmptyAndParseError(t *testing.T) {
	tests := map[string]string{
		"empty":              "",
		"no spec":            "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: x\n",
		"no symbols in spec": "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: x\nspec: {}\n",
		"invalid yaml":       "apiVersion: scafctl.io/v1\nkind: Solution\nspec:\n  resolvers:\n  - : : :\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				syms := DocumentSymbols([]byte(content))
				assert.Empty(t, syms)
			})
		})
	}
}

func TestDocumentSymbolsFromIndex_NilIndex(t *testing.T) {
	assert.Nil(t, documentSymbolsFromIndex(nil))
}

func TestSymbolsFeature_Registered(t *testing.T) {
	var found bool
	for _, f := range defaultFeatures() {
		if f.name == "symbols" {
			found = true
			require.NotNil(t, f.wire, "symbols feature must wire a handler")
		}
	}
	assert.True(t, found, "symbolsFeature must be in defaultFeatures")

	// Wiring the feature sets the documentSymbol handler and glsp will then
	// advertise the provider.
	h := &protocol.Handler{}
	s := NewServer("scafctl", "test", nil)
	symbolsFeature().wire(h, s)
	assert.NotNil(t, h.TextDocumentDocumentSymbol, "documentSymbol handler wired")
}
