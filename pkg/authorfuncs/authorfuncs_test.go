// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authorfuncs

import (
	"context"
	"os"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	gotmplext "github.com/oakwood-commons/scafctl/pkg/gotmpl/ext"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain installs the extension (sprig + custom) function factory so template
// bodies that call built-in helpers (e.g. upper) resolve during tests. In
// production this is done during application initialization.
func TestMain(m *testing.M) {
	gotmpl.SetExtensionFuncMapFactory(gotmplext.AllFuncMap)
	os.Exit(m.Run())
}

func TestCompile_Empty(t *testing.T) {
	lib, err := Compile(nil)
	require.NoError(t, err)
	assert.Nil(t, lib)

	lib, err = Compile(map[string]*spec.Function{})
	require.NoError(t, err)
	assert.Nil(t, lib)
}

func TestCompile_Names(t *testing.T) {
	lib, err := Compile(map[string]*spec.Function{
		"beta":  {Cel: "1"},
		"alpha": {Cel: "2"},
	})
	require.NoError(t, err)
	require.NotNil(t, lib)
	assert.Equal(t, []string{"alpha", "beta"}, lib.Names())
}

func TestBind_CelBody(t *testing.T) {
	lib, err := Compile(map[string]*spec.Function{
		"doubled": {
			Params: []*spec.ParamDef{{Name: "n", Type: spec.TypeInt, Required: true}},
			Cel:    "_.args.n * 2",
		},
	})
	require.NoError(t, err)

	fm := lib.Bind(context.Background())
	require.Contains(t, fm, "doubled")

	fn, ok := fm["doubled"].(func(...any) (any, error))
	require.True(t, ok)

	got, err := fn(21)
	require.NoError(t, err)
	assert.Equal(t, int64(42), got)
}

func TestBind_TemplateBody(t *testing.T) {
	lib, err := Compile(map[string]*spec.Function{
		"greet": {
			Params:   []*spec.ParamDef{{Name: "who", Type: spec.TypeString, Required: true}},
			Template: "HELLO {{ .args.who | upper }}!",
		},
	})
	require.NoError(t, err)

	fm := lib.Bind(context.Background())
	fn := fm["greet"].(func(...any) (any, error))

	got, err := fn("scaf")
	require.NoError(t, err)
	assert.Equal(t, "HELLO SCAF!", got)
}

func TestBind_TemplateCallsSibling(t *testing.T) {
	lib, err := Compile(map[string]*spec.Function{
		"greet": {
			Params:   []*spec.ParamDef{{Name: "who", Type: spec.TypeString, Required: true}},
			Template: "HELLO {{ .args.who | upper }}!",
		},
		"doubled": {
			Params: []*spec.ParamDef{{Name: "n", Type: spec.TypeInt, Required: true}},
			Cel:    "_.args.n * 2",
		},
		"shout": {
			Params:   []*spec.ParamDef{{Name: "name", Type: spec.TypeString, Default: "world"}},
			Template: `{{ greet .args.name }} ({{ doubled 21 }})`,
		},
	})
	require.NoError(t, err)

	fm := lib.Bind(context.Background())
	fn := fm["shout"].(func(...any) (any, error))

	got, err := fn("scaf")
	require.NoError(t, err)
	assert.Equal(t, "HELLO SCAF! (42)", got)
}

func TestBind_DefaultApplied(t *testing.T) {
	lib, err := Compile(map[string]*spec.Function{
		"greet": {
			Params:   []*spec.ParamDef{{Name: "name", Type: spec.TypeString, Default: "world"}},
			Template: "hi {{ .args.name }}",
		},
	})
	require.NoError(t, err)

	fn := lib.Bind(context.Background())["greet"].(func(...any) (any, error))

	got, err := fn() // omit optional arg -> default
	require.NoError(t, err)
	assert.Equal(t, "hi world", got)
}

func TestBind_TypeCoercion(t *testing.T) {
	lib, err := Compile(map[string]*spec.Function{
		"asInt": {
			Params: []*spec.ParamDef{{Name: "v", Type: spec.TypeInt}},
			Cel:    "_.args.v + 1",
		},
	})
	require.NoError(t, err)

	fn := lib.Bind(context.Background())["asInt"].(func(...any) (any, error))

	got, err := fn("41") // string coerced to int
	require.NoError(t, err)
	assert.Equal(t, int64(42), got)
}

func TestBind_MissingRequiredArg(t *testing.T) {
	lib, err := Compile(map[string]*spec.Function{
		"need": {
			Params: []*spec.ParamDef{{Name: "x", Type: spec.TypeString, Required: true}},
			Cel:    "_.args.x",
		},
	})
	require.NoError(t, err)

	fn := lib.Bind(context.Background())["need"].(func(...any) (any, error))

	_, err = fn()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument")
}

func TestBind_TooManyArgs(t *testing.T) {
	lib, err := Compile(map[string]*spec.Function{
		"one": {
			Params: []*spec.ParamDef{{Name: "x", Type: spec.TypeString}},
			Cel:    "_.args.x",
		},
	})
	require.NoError(t, err)

	fn := lib.Bind(context.Background())["one"].(func(...any) (any, error))

	_, err = fn("a", "b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at most 1 argument")
}

func TestBind_NilAndEmptyLibrary(t *testing.T) {
	var nilLib *Library
	assert.Nil(t, nilLib.Bind(context.Background()))
	assert.Equal(t, "", nilLib.Fingerprint())
	assert.Nil(t, nilLib.Names())
}

func TestFingerprint_Stability(t *testing.T) {
	defs := map[string]*spec.Function{
		"greet": {
			Params:   []*spec.ParamDef{{Name: "who", Type: spec.TypeString}},
			Template: "hi {{ .args.who }}",
		},
	}
	a, err := Compile(defs)
	require.NoError(t, err)
	b, err := Compile(defs)
	require.NoError(t, err)
	assert.Equal(t, a.Fingerprint(), b.Fingerprint())
	assert.NotEmpty(t, a.Fingerprint())
}

func TestFingerprint_ChangesWithBody(t *testing.T) {
	a, err := Compile(map[string]*spec.Function{
		"f": {Params: []*spec.ParamDef{{Name: "x"}}, Template: "a {{ .args.x }}"},
	})
	require.NoError(t, err)
	b, err := Compile(map[string]*spec.Function{
		"f": {Params: []*spec.ParamDef{{Name: "x"}}, Template: "b {{ .args.x }}"},
	})
	require.NoError(t, err)
	assert.NotEqual(t, a.Fingerprint(), b.Fingerprint())
}
