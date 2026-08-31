// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package celexp is a thin adapter over the standalone
// github.com/oakwood-commons/celexp library. It re-exports the library's public
// API under scafctl's original import path so that the ~31 consumer packages
// need no import or symbol changes.
//
// The only scafctl-specific behaviour lives in bridge.go, which wires scafctl's
// concrete logger, debug sink, and config-dir providers into the library's
// injectable seams and aligns the library's defaults with scafctl's settings.
//
// All consumer code continues to import
// "github.com/oakwood-commons/scafctl/pkg/celexp" with no changes required.
package celexp

import (
	"context"
	"fmt"

	upstream "github.com/oakwood-commons/celexp"
)

// Type aliases re-export upstream types so consumers need no import changes.
type (
	Expression      = upstream.Expression
	CompileResult   = upstream.CompileResult
	ExtFunction     = upstream.ExtFunction
	ExtFunctionList = upstream.ExtFunctionList
	Example         = upstream.Example
	ProgramCache    = upstream.ProgramCache
	CacheOption     = upstream.CacheOption
	CacheStats      = upstream.CacheStats
	Option          = upstream.Option
	VarDecl         = upstream.VarDecl
	VarInfo         = upstream.VarInfo
	VariableRef     = upstream.VariableRef
	ExpressionStat  = upstream.ExpressionStat
	CELConfigInput  = upstream.CELConfigInput
)

// Constant re-exports.
const (
	DefaultCacheSizeConst = upstream.DefaultCacheSizeConst
	DefaultCostLimitConst = upstream.DefaultCostLimitConst

	VarSelf      = upstream.VarSelf
	VarItem      = upstream.VarItem
	VarIndex     = upstream.VarIndex
	VarActions   = upstream.VarActions
	VarCwd       = upstream.VarCwd
	VarExecution = upstream.VarExecution
	VarPlan      = upstream.VarPlan
	VarError     = upstream.VarError
	VarParams    = upstream.VarParams
)

// GetDefaultCacheSize returns the library's current default cache size. It is a
// live read of the library's package var (which bridge.go seeds from scafctl's
// settings), so it stays consistent with SetDefaultCacheSize. A plain
// `var DefaultCacheSize = upstream.DefaultCacheSize` re-export would be a one-time
// value copy: it would go stale after SetDefaultCacheSize and lose writes, so a
// getter is used instead.
func GetDefaultCacheSize() int { return upstream.DefaultCacheSize }

// Function re-exports that have no scafctl-specific behaviour.
var (
	BuildCELContext      = upstream.BuildCELContext
	ClearDefaultCache    = upstream.ClearDefaultCache
	ContextWithCostLimit = upstream.ContextWithCostLimit
	CostLimitFromContext = upstream.CostLimitFromContext
	EvaluateExpression   = upstream.EvaluateExpression
	GetDefaultCostLimit  = upstream.GetDefaultCostLimit
	InitFromAppConfig    = upstream.InitFromAppConfig
	LoadDataFile         = upstream.LoadDataFile
	NewParseEnv          = upstream.NewParseEnv
	ParseVars            = upstream.ParseVars
	ResetDefaultCache    = upstream.ResetDefaultCache
	ResetForTesting      = upstream.ResetForTesting
	SetCacheFactory      = upstream.SetCacheFactory
	SetDefaultCacheSize  = upstream.SetDefaultCacheSize
	SetDefaultCostLimit  = upstream.SetDefaultCostLimit
	SetEnvFactory        = upstream.SetEnvFactory
	SetLoggerProvider    = upstream.SetLoggerProvider
	ValidateSyntax       = upstream.ValidateSyntax
	WithLogger           = upstream.WithLogger

	// Expression constructors.
	NewCoalesce            = upstream.NewCoalesce
	NewConditional         = upstream.NewConditional
	NewStringInterpolation = upstream.NewStringInterpolation

	// Cache accessors and options.
	GetDefaultCacheStats = upstream.GetDefaultCacheStats
	GetAppConfigCache    = upstream.GetAppConfigCache
	GetDefaultCache      = upstream.GetDefaultCache
	NewProgramCache      = upstream.NewProgramCache
	WithASTBasedCaching  = upstream.WithASTBasedCaching

	// Compile options.
	WithCache       = upstream.WithCache
	WithContext     = upstream.WithContext
	WithCostLimit   = upstream.WithCostLimit
	WithNoCostLimit = upstream.WithNoCostLimit

	// Var declarations.
	NewVarDecl = upstream.NewVarDecl
)

// BuildDataContext delegates to the library but restores scafctl's flag-accurate
// error wording. The scafctl CLI exposes these as the --data and --file flags
// (pkg/cmd/scafctl/eval), and the message surfaces verbatim to users, so the
// adapter preserves the "--data"/"--file" phrasing the library states in terms
// of its parameter names.
func BuildDataContext(data, file string) (any, error) {
	if data != "" && file != "" {
		return nil, fmt.Errorf("cannot use both --data and --file")
	}
	return upstream.BuildDataContext(data, file)
}

// EvalAs re-declares the library's generic helper. Generic functions cannot be
// aliased via var, so this is a thin pass-through wrapper.
func EvalAs[T any](r *CompileResult, vars map[string]any) (T, error) {
	return upstream.EvalAs[T](r, vars)
}

// EvalAsWithContext re-declares the library's generic helper. Generic functions
// cannot be aliased via var, so this is a thin pass-through wrapper.
func EvalAsWithContext[T any](ctx context.Context, r *CompileResult, vars map[string]any) (T, error) {
	return upstream.EvalAsWithContext[T](ctx, r, vars)
}
