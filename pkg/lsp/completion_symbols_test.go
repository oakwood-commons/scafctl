// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// replaceOnce substitutes the first occurrence of old with repl in s, used to
// drop a partial token into a fixture at one place.
func replaceOnce(s, old, repl string) string {
	return strings.Replace(s, old, repl, 1)
}

// symbolCELFixture defines two resolvers and a CEL expr value being typed.
const symbolCELFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sym
spec:
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
          - provider: static
            inputs:
              value:
                expr: _.PARTIAL
`

func TestSymbolCompletion_CELResolverPrefix(t *testing.T) {
	// After "_." (empty partial) all resolver names are offered.
	content := replaceOnce(symbolCELFixture, "_.PARTIAL", "_.")
	labels := completeAt(t, content, "expr: _.", "_.", 2)
	assert.True(t, contains(labels, "environment"), "offers environment: %v", labels)
	assert.True(t, contains(labels, "appName"), "offers appName: %v", labels)
}

func TestSymbolCompletion_CELResolverPrefixFiltered(t *testing.T) {
	// "_.env" narrows to resolvers starting with "env".
	content := replaceOnce(symbolCELFixture, "_.PARTIAL", "_.env")
	labels := completeAt(t, content, "expr: _.env", "_.env", 5)
	assert.Equal(t, []string{"environment"}, labels, "only environment matches the env prefix")
}

func TestSymbolCompletion_TemplateResolverPrefix(t *testing.T) {
	content := replaceOnce(symbolCELFixture, "expr: _.PARTIAL", `tmpl: "{{ ._. }}"`)
	labels := completeAt(t, content, `tmpl: "{{ ._. }}"`, "._.", 3)
	assert.True(t, contains(labels, "environment"), "offers environment: %v", labels)
	assert.True(t, contains(labels, "appName"), "offers appName: %v", labels)
}

// symbolCallFixture defines a call and a resolver step that references one.
const symbolCallFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sym
spec:
  calls:
    fetch:
      provider: message
      inputs:
        message: hi
  resolvers:
    appName:
      resolve:
        with:
          - call: PARTIAL
`

func TestSymbolCompletion_CallValue(t *testing.T) {
	content := replaceOnce(symbolCallFixture, "call: PARTIAL", "call: fet")
	labels := completeAt(t, content, "call: fet", "fet", 3)
	assert.Equal(t, []string{"fetch"}, labels, "call value offers call names: %v", labels)
}

// symbolRslvrFixture references a resolver via a rslvr ValueRef.
const symbolRslvrFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sym
spec:
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
          - provider: static
            inputs:
              value:
                rslvr: PARTIAL
`

func TestSymbolCompletion_RslvrValue(t *testing.T) {
	content := replaceOnce(symbolRslvrFixture, "rslvr: PARTIAL", "rslvr: env")
	labels := completeAt(t, content, "rslvr: env", "env", 3)
	assert.Equal(t, []string{"environment"}, labels, "rslvr value offers resolver names: %v", labels)
}

// symbolDependsOnFixture has a resolver dependsOn list item being typed.
const symbolDependsOnFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sym
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    appName:
      dependsOn:
        - PARTIAL
      resolve:
        with:
          - provider: static
            inputs:
              value: x
`

func TestSymbolCompletion_ResolverDependsOn(t *testing.T) {
	content := replaceOnce(symbolDependsOnFixture, "- PARTIAL", "- env")
	labels := completeAt(t, content, "- env", "env", 3)
	assert.Equal(t, []string{"environment"}, labels, "resolver dependsOn offers resolver names: %v", labels)
}

// symbolActionDependsOnFixture has an action dependsOn list item being typed.
const symbolActionDependsOnFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sym
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
  workflow:
    actions:
      build:
        provider: message
        inputs:
          message: building
      deploy:
        dependsOn:
          - PARTIAL
        provider: message
        inputs:
          message: deploying
`

func TestSymbolCompletion_ActionDependsOn(t *testing.T) {
	content := replaceOnce(symbolActionDependsOnFixture, "- PARTIAL", "- bui")
	labels := completeAt(t, content, "- bui", "bui", 3)
	assert.Equal(t, []string{"build"}, labels, "action dependsOn offers action names: %v", labels)
}

func TestSymbolCompletion_UnknownScopeOffersNothing(t *testing.T) {
	// A plain scalar value that is not a symbol position -> no symbol completion.
	labels := completeAt(t, symbolCELFixture, "value: dev", "dev", 1)
	assert.Empty(t, labels, "a normal value offers no symbol names: %v", labels)
}

func TestSymbolCompletion_ParseErrorNoPanic(t *testing.T) {
	// A malformed document must not panic; symbol completion returns nothing.
	broken := "apiVersion: scafctl.io/v1\nkind: Solution\nspec:\n  resolvers:\n  - : : :\n    expr: _.\n"
	assert.NotPanics(t, func() {
		_ = completeAt(t, broken, "expr: _.", "_.", 2)
	})
}
