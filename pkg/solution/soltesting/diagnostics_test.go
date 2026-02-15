// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting_test

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/stretchr/testify/assert"
)

func TestDiagnoseExpression_EqualityComparison(t *testing.T) {
	celCtx := soltesting.BuildAssertionContext(&soltesting.CommandOutput{
		ExitCode: 1,
		Stdout:   "hello",
		Files:    map[string]soltesting.FileInfo{},
	})

	result := soltesting.DiagnoseExpression(context.Background(), "__exitCode == 0", celCtx)
	assert.Contains(t, result, "__exitCode = 1")
	assert.Contains(t, result, "0 = 0")
	assert.Contains(t, result, `Comparison "==" failed`)
}

func TestDiagnoseExpression_NotEqualComparison(t *testing.T) {
	celCtx := soltesting.BuildAssertionContext(&soltesting.CommandOutput{
		ExitCode: 0,
		Files:    map[string]soltesting.FileInfo{},
	})

	result := soltesting.DiagnoseExpression(context.Background(), "__exitCode != 0", celCtx)
	assert.Contains(t, result, "__exitCode = 0")
	assert.Contains(t, result, `Comparison "!=" failed`)
}

func TestDiagnoseExpression_GreaterThan(t *testing.T) {
	celCtx := soltesting.BuildAssertionContext(&soltesting.CommandOutput{
		ExitCode: 0,
		Files:    map[string]soltesting.FileInfo{},
	})

	result := soltesting.DiagnoseExpression(context.Background(), "__exitCode > 1", celCtx)
	assert.Contains(t, result, "__exitCode = 0")
	assert.Contains(t, result, "1 = 1")
	assert.Contains(t, result, `Comparison ">" failed`)
}

func TestDiagnoseExpression_StringComparison(t *testing.T) {
	celCtx := soltesting.BuildAssertionContext(&soltesting.CommandOutput{
		Stdout: "actual",
		Files:  map[string]soltesting.FileInfo{},
	})

	result := soltesting.DiagnoseExpression(context.Background(), `__stdout == "expected"`, celCtx)
	assert.Contains(t, result, "__stdout = actual")
	assert.Contains(t, result, `"expected" = expected`)
	assert.Contains(t, result, `Comparison "==" failed`)
}

func TestDiagnoseExpression_NonComparison(t *testing.T) {
	celCtx := soltesting.BuildAssertionContext(&soltesting.CommandOutput{
		Stdout: "hello",
		Files:  map[string]soltesting.FileInfo{},
	})

	result := soltesting.DiagnoseExpression(context.Background(), `__stdout.contains("world")`, celCtx)
	assert.Contains(t, result, "expected true")
	assert.Contains(t, result, "false")
}

func TestDiagnoseExpression_SizeComparison(t *testing.T) {
	celCtx := soltesting.BuildAssertionContext(&soltesting.CommandOutput{
		Stdout: "abc",
		Files:  map[string]soltesting.FileInfo{},
	})

	result := soltesting.DiagnoseExpression(context.Background(), "size(__stdout) == 10", celCtx)
	assert.Contains(t, result, "size(__stdout) = 3")
	assert.Contains(t, result, "10 = 10")
	assert.Contains(t, result, `Comparison "==" failed`)
}
