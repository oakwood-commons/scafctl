// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"text/template"
)

// ErrFuncNameCollision indicates that a template function name is already
// registered, either as a built-in extension function (sprig/custom) or by a
// prior registration. Callers can match it with errors.Is.
var ErrFuncNameCollision = errors.New("template function name already registered")

var (
	// registryMu guards the embedder function registries below. Registration
	// happens at startup; the maps are read on every template render, so a
	// RWMutex keeps the hot read path cheap.
	registryMu sync.RWMutex

	// registeredFuncs holds additively-registered embedder functions. Built-in
	// functions win on name collision, so an entry here is only added to a
	// service func map when the name is not already provided by the base set.
	registeredFuncs = template.FuncMap{}

	// overrideFuncs holds embedder functions that intentionally overwrite
	// built-ins at merge time. This is the explicit escape hatch for embedders
	// that need to replace a sprig/custom function.
	overrideFuncs = template.FuncMap{}
)

// RegisterFuncs registers embedder-supplied Go template functions additively.
//
// Built-ins win on collision: if any provided name already exists in the
// built-in extension set (sprig + custom, when the extension factory has been
// set via SetExtensionFuncMapFactory) or was registered by a prior call, the
// entire call is rejected with an error wrapping ErrFuncNameCollision and
// nothing is registered (all-or-nothing). Use RegisterFuncsOverride to
// deliberately replace a built-in.
//
// Registration is process-global and additive across calls; it is intended to
// be performed once at application startup, after RegisterDefaults has set the
// extension factory so built-in collisions can be detected. Functions
// registered here become available to the go-template provider and every other
// gotmpl consumer, and appear in list_go_template_functions and
// `get template functions`.
//
// Like the Go standard library's global registries (for example sql.Register),
// this is register-once: re-registering a name that is already present -- even
// with an identical function -- is rejected as a collision. Callers that run in
// a long-running process should register their functions a single time rather
// than on every command execution (the root command guards its own wiring so a
// command executed more than once does not self-collide).
//
// Each value is validated up front: a nil value, a non-function, or a function
// whose return signature text/template would reject is rejected here with a
// clear error, rather than panicking later when a template is constructed.
//
// This function is thread-safe.
func RegisterFuncs(funcs template.FuncMap) error {
	if len(funcs) == 0 {
		return nil
	}

	builtins := factoryFuncMap()
	names := sortedFuncNames(funcs)

	registryMu.Lock()
	defer registryMu.Unlock()

	// Validate every name before mutating so a collision leaves the registry
	// untouched (all-or-nothing).
	for _, name := range names {
		if name == "" {
			return fmt.Errorf("register template func: name must not be empty")
		}
		if err := validateFunc(name, funcs[name]); err != nil {
			return fmt.Errorf("register template func: %w", err)
		}
		if _, ok := builtins[name]; ok {
			return fmt.Errorf("register template func %q: %w (built-in)", name, ErrFuncNameCollision)
		}
		if _, ok := registeredFuncs[name]; ok {
			return fmt.Errorf("register template func %q: %w", name, ErrFuncNameCollision)
		}
		if _, ok := overrideFuncs[name]; ok {
			return fmt.Errorf("register template func %q: %w (override)", name, ErrFuncNameCollision)
		}
	}

	for name, fn := range funcs {
		registeredFuncs[name] = fn
	}
	return nil
}

// RegisterFuncsOverride registers embedder-supplied Go template functions that
// overwrite built-ins at merge time. This is the explicit escape hatch for
// embedders that need to replace a sprig/custom function (including re-enabling
// stripped functions such as env). Unlike RegisterFuncs it does not reject
// names that collide with built-ins; it errors only when the same name has
// already been registered as an override (all-or-nothing). Values are validated
// the same way as RegisterFuncs so an invalid function fails fast here.
//
// This function is thread-safe.
func RegisterFuncsOverride(funcs template.FuncMap) error {
	if len(funcs) == 0 {
		return nil
	}

	names := sortedFuncNames(funcs)

	registryMu.Lock()
	defer registryMu.Unlock()

	for _, name := range names {
		if name == "" {
			return fmt.Errorf("register template func override: name must not be empty")
		}
		if err := validateFunc(name, funcs[name]); err != nil {
			return fmt.Errorf("register template func override: %w", err)
		}
		if _, ok := overrideFuncs[name]; ok {
			return fmt.Errorf("register template func override %q: %w", name, ErrFuncNameCollision)
		}
	}

	for name, fn := range funcs {
		overrideFuncs[name] = fn
	}
	return nil
}

// RegisteredFuncs returns the embedder-registered functions (additive and
// override) as an ExtFunctionList tagged with Source "embedder" for
// discoverability tooling (MCP list_go_template_functions and the CLI
// `get template functions` command). The returned list is sorted by name.
func RegisteredFuncs() ExtFunctionList {
	registryMu.RLock()
	defer registryMu.RUnlock()

	seen := make(map[string]struct{}, len(registeredFuncs)+len(overrideFuncs))
	list := make(ExtFunctionList, 0, len(registeredFuncs)+len(overrideFuncs))

	appendFuncs := func(src template.FuncMap, description string) {
		for _, name := range sortedFuncNames(src) {
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			list = append(list, ExtFunction{
				Name:        name,
				Description: description,
				Source:      SourceEmbedder,
				Func:        template.FuncMap{name: src[name]},
			})
		}
	}

	// Override entries take precedence in the description if a name somehow
	// appears in both maps.
	appendFuncs(overrideFuncs, "Embedder-registered template function (overrides a built-in)")
	appendFuncs(registeredFuncs, "Embedder-registered template function")

	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// registeredExtensionFuncs returns copies of the additive and override
// registries so callers cannot mutate the shared maps. Consumed by
// getExtensionFuncMap during service construction.
func registeredExtensionFuncs() (additive, override template.FuncMap) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	additive = make(template.FuncMap, len(registeredFuncs))
	for k, v := range registeredFuncs {
		additive[k] = v
	}
	override = make(template.FuncMap, len(overrideFuncs))
	for k, v := range overrideFuncs {
		override[k] = v
	}
	return additive, override
}

// errorType is the reflect.Type of the error interface, used by validateFunc to
// match text/template's own good-function rule for two-result functions.
var errorType = reflect.TypeOf((*error)(nil)).Elem()

// validateFunc reports whether fn is usable as a text/template function,
// mirroring the checks text/template performs in (*Template).Funcs. Registering
// a nil value, a non-function, or a function with an unsupported return
// signature would otherwise panic later at template construction, far from the
// embedder call site. Failing fast here turns that latent panic into a clear
// error at registration time.
func validateFunc(name string, fn any) error {
	if fn == nil {
		return fmt.Errorf("value for %q must not be nil", name)
	}
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func {
		return fmt.Errorf("value for %q is %s, want a function", name, v.Kind())
	}
	if v.IsNil() {
		return fmt.Errorf("value for %q must not be a nil function", name)
	}
	t := v.Type()
	switch {
	case t.NumOut() == 1:
	case t.NumOut() == 2 && t.Out(1) == errorType:
	default:
		return fmt.Errorf("function %q must return 1 value, or 2 values where the second is error", name)
	}
	return nil
}

// sortedFuncNames returns the keys of a FuncMap in deterministic sorted order
// so validation errors are reproducible.
func sortedFuncNames(funcs template.FuncMap) []string {
	names := make([]string, 0, len(funcs))
	for name := range funcs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CombinedFuncs merges a built-in function list with embedder-registered
// functions into a single list de-duplicated by name. Embedder entries win on a
// name collision, so when an embedder shadows a built-in via
// RegisterFuncsOverride the listing shows only the effective (override)
// function rather than also showing the shadowed built-in. This keeps
// discoverability output (MCP list_go_template_functions and the CLI
// `get template functions` command) aligned with the function that actually
// runs. Order is preserved: surviving built-ins first, then the registered
// functions.
func CombinedFuncs(builtins, registered ExtFunctionList) ExtFunctionList {
	if len(registered) == 0 {
		return builtins
	}
	shadowed := make(map[string]struct{}, len(registered))
	for _, f := range registered {
		shadowed[f.Name] = struct{}{}
	}
	out := make(ExtFunctionList, 0, len(builtins)+len(registered))
	for _, f := range builtins {
		if _, ok := shadowed[f.Name]; ok {
			continue
		}
		out = append(out, f)
	}
	return append(out, registered...)
}

// resetRegistryForTesting clears both embedder registries. Tests only.
func resetRegistryForTesting() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registeredFuncs = template.FuncMap{}
	overrideFuncs = template.FuncMap{}
}
