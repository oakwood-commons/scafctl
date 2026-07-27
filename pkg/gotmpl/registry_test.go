// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"context"
	"errors"
	"sync"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withFactory temporarily installs a custom extension func map factory,
// bypassing the sync.Once guard, and restores the previous factory on cleanup.
// This lets registry tests exercise built-in collision detection.
func withFactory(t *testing.T, fm template.FuncMap) {
	t.Helper()
	extensionFuncMapMu.Lock()
	old := extensionFuncMapFactory
	extensionFuncMapFactory = func() template.FuncMap {
		// Return a fresh copy each call so callers that mutate (env stripping)
		// do not corrupt the shared map.
		cp := make(template.FuncMap, len(fm))
		for k, v := range fm {
			cp[k] = v
		}
		return cp
	}
	extensionFuncMapMu.Unlock()
	t.Cleanup(func() {
		extensionFuncMapMu.Lock()
		extensionFuncMapFactory = old
		extensionFuncMapMu.Unlock()
	})
}

// render is a small helper that renders content through a fresh nil-func
// service (which picks up the registry via getExtensionFuncMap).
func render(t *testing.T, content string) (string, error) {
	t.Helper()
	svc := NewService(nil)
	res, err := svc.Execute(context.Background(), TemplateOptions{
		Content:    content,
		Name:       "registry-test",
		MissingKey: MissingKeyDefault,
	})
	if err != nil {
		return "", err
	}
	return res.Output, nil
}

func TestRegisterFuncs_RenderThroughService(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	err := RegisterFuncs(template.FuncMap{
		"shout": func(s string) string { return s + "!" },
	})
	require.NoError(t, err)

	out, err := render(t, `{{ shout "hi" }}`)
	require.NoError(t, err)
	assert.Equal(t, "hi!", out)
}

func TestRegisterFuncs_EmptyInputIsNoop(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	assert.NoError(t, RegisterFuncs(nil))
	assert.NoError(t, RegisterFuncs(template.FuncMap{}))
	assert.Empty(t, RegisteredFuncs())
}

func TestRegisterFuncs_EmptyNameRejected(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	err := RegisterFuncs(template.FuncMap{"": func() string { return "" }})
	require.Error(t, err)
	assert.Empty(t, RegisteredFuncs(), "nothing should be registered on error")
}

func TestRegisterFuncs_InvalidValueRejected(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	cases := map[string]any{
		"nil value":         nil,
		"non-function":      "not a func",
		"nil function":      (func())(nil),
		"too many results":  func() (int, int, int) { return 0, 0, 0 },
		"bad second result": func() (int, int) { return 0, 0 },
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			err := RegisterFuncs(template.FuncMap{"bad": bad})
			require.Error(t, err, "invalid func value must fail fast")
			assert.Empty(t, RegisteredFuncs(), "nothing should be registered on error")

			err = RegisterFuncsOverride(template.FuncMap{"bad": bad})
			require.Error(t, err, "override must validate too")
			assert.Empty(t, RegisteredFuncs())
		})
	}
}

func TestRegisterFuncs_ValidTwoResultFuncAccepted(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	require.NoError(t, RegisterFuncs(template.FuncMap{
		"maybe": func(s string) (string, error) { return s, nil },
	}))

	out, err := render(t, `{{ maybe "ok" }}`)
	require.NoError(t, err)
	assert.Equal(t, "ok", out)
}

func TestRegisterFuncs_CollisionWithBuiltin(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	// Built-in 'upper' uppercases; the collision must be rejected and the
	// built-in must remain unchanged.
	withFactory(t, template.FuncMap{
		"upper": func(s string) string { return "BUILTIN:" + s },
	})

	err := RegisterFuncs(template.FuncMap{
		"upper": func(s string) string { return "embedder" },
	})
	require.ErrorIs(t, err, ErrFuncNameCollision)
	assert.Empty(t, RegisteredFuncs(), "collision leaves registry untouched")

	out, err := render(t, `{{ upper "x" }}`)
	require.NoError(t, err)
	assert.Equal(t, "BUILTIN:x", out, "built-in must be unchanged")
}

func TestRegisterFuncs_CollisionWithPriorRegistration(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	require.NoError(t, RegisterFuncs(template.FuncMap{"dup": func() string { return "a" }}))

	err := RegisterFuncs(template.FuncMap{"dup": func() string { return "b" }})
	require.ErrorIs(t, err, ErrFuncNameCollision)

	// First registration remains intact.
	out, err := render(t, `{{ dup }}`)
	require.NoError(t, err)
	assert.Equal(t, "a", out)
}

func TestRegisterFuncs_AllOrNothingOnCollision(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	require.NoError(t, RegisterFuncs(template.FuncMap{"taken": func() string { return "x" }}))

	// One good name + one colliding name: nothing from this call registers.
	err := RegisterFuncs(template.FuncMap{
		"fresh": func() string { return "y" },
		"taken": func() string { return "z" },
	})
	require.ErrorIs(t, err, ErrFuncNameCollision)

	names := make(map[string]struct{})
	for _, f := range RegisteredFuncs() {
		names[f.Name] = struct{}{}
	}
	assert.Contains(t, names, "taken")
	assert.NotContains(t, names, "fresh", "partial registration must not occur")
}

func TestRegisterFuncsOverride_OverwritesBuiltin(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	withFactory(t, template.FuncMap{
		"greet": func() string { return "builtin" },
	})

	require.NoError(t, RegisterFuncsOverride(template.FuncMap{
		"greet": func() string { return "override" },
	}))

	out, err := render(t, `{{ greet }}-marker-override`)
	require.NoError(t, err)
	assert.Equal(t, "override-marker-override", out)
}

func TestRegisterFuncsOverride_DuplicateRejected(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	require.NoError(t, RegisterFuncsOverride(template.FuncMap{"o": func() string { return "1" }}))
	err := RegisterFuncsOverride(template.FuncMap{"o": func() string { return "2" }})
	require.ErrorIs(t, err, ErrFuncNameCollision)
}

func TestRegisterFuncs_AdditiveEnvIsRejectedWhenStripped(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	// Factory exposes env (as sprig does). With env functions disallowed (the
	// default), the additive path must reject a colliding 'env' so it cannot be
	// smuggled back in via the always-stripped name.
	withFactory(t, template.FuncMap{
		"env": func(string) string { return "SECRET" },
	})
	SetAllowEnvFunctions(false)

	err := RegisterFuncs(template.FuncMap{"env": func(string) string { return "leak" }})
	require.ErrorIs(t, err, ErrFuncNameCollision)

	// And the effective map must not contain env.
	_, ok := getExtensionFuncMap()["env"]
	assert.False(t, ok, "env must remain stripped")
}

func TestRegisterFuncsOverride_CanReintroduceEnv(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	withFactory(t, template.FuncMap{
		"env": func(string) string { return "SECRET" },
	})
	SetAllowEnvFunctions(false)

	require.NoError(t, RegisterFuncsOverride(template.FuncMap{
		"env": func(string) string { return "embedder-env" },
	}))

	fm := getExtensionFuncMap()
	fn, ok := fm["env"]
	require.True(t, ok, "override must re-introduce env")
	casted, ok := fn.(func(string) string)
	require.True(t, ok)
	assert.Equal(t, "embedder-env", casted(""))
}

// TestRegisterFuncs_AdditiveEnvSkippedRegardlessOfFactory guards the
// defense-in-depth property that the additive merge never re-introduces a
// stripped env function, even when the additive 'env' was accepted at
// registration time because the factory had not yet been set (so built-in
// collision detection was empty).
func TestRegisterFuncs_AdditiveEnvSkippedRegardlessOfFactory(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	// No factory installed: factoryFuncMap() is empty, so 'env' registers
	// additively without a built-in collision.
	withFactory(t, template.FuncMap{})
	SetAllowEnvFunctions(false)

	require.NoError(t, RegisterFuncs(template.FuncMap{
		"env": func(string) string { return "leak" },
	}))

	// The merge must still refuse to expose env while stripping is in effect.
	_, ok := getExtensionFuncMap()["env"]
	assert.False(t, ok, "additive env must not be merged while stripped")
}

// TestRegisterFuncs_DuplicateNameIsCollision verifies register-once semantics:
// re-registering a name that is already present -- even with the identical
// function value -- is rejected as a collision (matching stdlib global
// registries such as sql.Register). The re-execution case is handled by the
// root command guarding its own wiring, not by an idempotency exception here.
func TestRegisterFuncs_DuplicateNameIsCollision(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	fn := func() string { return "v" }
	funcs := template.FuncMap{"reg": fn}

	require.NoError(t, RegisterFuncs(funcs))
	// The very same func value re-registered is still a collision.
	require.ErrorIs(t, RegisterFuncs(funcs), ErrFuncNameCollision)

	// Exactly one registered entry remains, still the original.
	list := RegisteredFuncs()
	require.Len(t, list, 1)
	assert.Equal(t, "reg", list[0].Name)

	out, err := render(t, `{{ reg }}`)
	require.NoError(t, err)
	assert.Equal(t, "v", out)
}

func TestCombinedFuncs_DedupesOverrideByName(t *testing.T) {
	builtins := ExtFunctionList{
		{Name: "upper", Source: SourceSprig},
		{Name: "custom1", Source: SourceCustom},
	}
	registered := ExtFunctionList{
		{Name: "upper", Source: SourceEmbedder}, // shadows the built-in
		{Name: "shout", Source: SourceEmbedder},
	}

	got := CombinedFuncs(builtins, registered)

	bySource := make(map[string]string, len(got))
	count := make(map[string]int, len(got))
	for _, f := range got {
		bySource[f.Name] = f.Source
		count[f.Name]++
	}
	assert.Equal(t, 1, count["upper"], "shadowed built-in must appear once")
	assert.Equal(t, SourceEmbedder, bySource["upper"], "embedder override must win")
	assert.Equal(t, SourceCustom, bySource["custom1"], "non-shadowed built-in retained")
	assert.Equal(t, SourceEmbedder, bySource["shout"], "embedder-only func retained")
	assert.Len(t, got, 3, "no duplicate entries")
}

func TestCombinedFuncs_EmptyRegisteredReturnsBuiltins(t *testing.T) {
	builtins := ExtFunctionList{{Name: "a", Source: SourceSprig}}
	got := CombinedFuncs(builtins, nil)
	assert.Equal(t, builtins, got)
}

func TestRegistry_Precedence(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	// Registry provides the base value.
	require.NoError(t, RegisterFuncs(template.FuncMap{
		"who": func() string { return "registry" },
	}))

	// A service-level func overrides the registry.
	svc := NewService(template.FuncMap{
		"who": func() string { return "service" },
	})

	// 1. registry only, via a fresh nil service.
	base, err := render(t, `{{ who }}-base`)
	require.NoError(t, err)
	assert.Equal(t, "registry-base", base)

	// 2. service-level override.
	res, err := svc.Execute(context.Background(), TemplateOptions{
		Content: `{{ who }}-svc`,
		Name:    "prec-svc",
	})
	require.NoError(t, err)
	assert.Equal(t, "service-svc", res.Output)

	// 3. per-call override wins over service and registry.
	res, err = svc.Execute(context.Background(), TemplateOptions{
		Content: `{{ who }}-call`,
		Name:    "prec-call",
		Funcs:   template.FuncMap{"who": func() string { return "call" }},
	})
	require.NoError(t, err)
	assert.Equal(t, "call-call", res.Output)
}

func TestRegisteredFuncs_SourceTagged(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	require.NoError(t, RegisterFuncs(template.FuncMap{"addFn": func() string { return "" }}))
	require.NoError(t, RegisterFuncsOverride(template.FuncMap{"ovrFn": func() string { return "" }}))

	list := RegisteredFuncs()
	require.Len(t, list, 2)
	// Sorted by name: addFn, ovrFn.
	assert.Equal(t, "addFn", list[0].Name)
	assert.Equal(t, "ovrFn", list[1].Name)
	for _, f := range list {
		assert.Equal(t, SourceEmbedder, f.Source)
		assert.NotEmpty(t, f.Func, "Func entry must be populated for discoverability")
	}
}

func TestResetRegistryForTesting_Isolation(t *testing.T) {
	ResetRegistryForTesting()
	require.NoError(t, RegisterFuncs(template.FuncMap{"tmp": func() string { return "" }}))
	assert.Len(t, RegisteredFuncs(), 1)

	ResetRegistryForTesting()
	assert.Empty(t, RegisteredFuncs())
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	var wg sync.WaitGroup
	// Writers registering distinct names. Each goroutine records its own
	// registration error in a dedicated slot (no shared slot => no lock needed)
	// so a failure surfaces with context instead of only as a length mismatch.
	writeErrs := make([]error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := "cf" + string(rune('a'+n))
			writeErrs[n] = RegisterFuncs(template.FuncMap{name: func() string { return name }})
		}(i)
	}
	// Readers hitting the merge path concurrently.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = getExtensionFuncMap()
			_ = RegisteredFuncs()
		}()
	}
	wg.Wait()

	// Every distinct writer must have registered successfully.
	for n, err := range writeErrs {
		assert.NoErrorf(t, err, "writer %d failed to register", n)
	}
	// All 8 distinct writers should have registered without error/race.
	assert.Len(t, RegisteredFuncs(), 8)
}

func TestErrFuncNameCollision_Is(t *testing.T) {
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)

	require.NoError(t, RegisterFuncs(template.FuncMap{"x": func() string { return "" }}))
	err := RegisterFuncs(template.FuncMap{"x": func() string { return "" }})
	assert.True(t, errors.Is(err, ErrFuncNameCollision))
}
