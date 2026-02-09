// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solutionprovider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildFromEnvelope_Success(t *testing.T) {
	resolverData := map[string]any{
		"db_host": "db.prod.internal",
		"db_port": 5432,
	}

	envelope := BuildFromEnvelope(resolverData, nil)

	assert.Equal(t, "success", envelope.Status)
	assert.Equal(t, resolverData, envelope.Resolvers)
	assert.Empty(t, envelope.Errors)
	assert.Nil(t, envelope.Workflow, "from envelope should not have workflow")
	assert.Nil(t, envelope.Success, "from envelope should not have success bool")
	assert.Nil(t, envelope.DryRun)
}

func TestBuildFromEnvelope_WithErrors(t *testing.T) {
	resolverData := map[string]any{
		"db_host": "db.prod.internal",
	}
	errors := []ResolverError{
		{Resolver: "db_port", Message: "connection refused"},
	}

	envelope := BuildFromEnvelope(resolverData, errors)

	assert.Equal(t, "failed", envelope.Status)
	assert.Equal(t, resolverData, envelope.Resolvers)
	assert.Len(t, envelope.Errors, 1)
	assert.Equal(t, "db_port", envelope.Errors[0].Resolver)
	assert.Equal(t, "connection refused", envelope.Errors[0].Message)
}

func TestBuildFromEnvelope_NilResolverData(t *testing.T) {
	envelope := BuildFromEnvelope(nil, nil)

	assert.Equal(t, "success", envelope.Status)
	assert.NotNil(t, envelope.Resolvers, "should default to empty map")
	assert.Empty(t, envelope.Resolvers)
	assert.NotNil(t, envelope.Errors, "should default to empty slice")
	assert.Empty(t, envelope.Errors)
}

func TestBuildActionEnvelope_Success(t *testing.T) {
	resolverData := map[string]any{
		"region": "us-east-1",
	}
	workflow := &WorkflowResult{
		FinalStatus:    "succeeded",
		FailedActions:  []string{},
		SkippedActions: []string{},
	}

	envelope := BuildActionEnvelope(resolverData, workflow, nil)

	assert.Equal(t, "success", envelope.Status)
	assert.Equal(t, resolverData, envelope.Resolvers)
	require.NotNil(t, envelope.Workflow)
	assert.Equal(t, "succeeded", envelope.Workflow.FinalStatus)
	require.NotNil(t, envelope.Success)
	assert.True(t, *envelope.Success)
	assert.Nil(t, envelope.DryRun)
}

func TestBuildActionEnvelope_WorkflowFailed(t *testing.T) {
	resolverData := map[string]any{
		"region": "us-east-1",
	}
	workflow := &WorkflowResult{
		FinalStatus:    "failed",
		FailedActions:  []string{"deploy"},
		SkippedActions: []string{"verify"},
	}

	envelope := BuildActionEnvelope(resolverData, workflow, nil)

	assert.Equal(t, "failed", envelope.Status)
	require.NotNil(t, envelope.Success)
	assert.False(t, *envelope.Success)
	assert.Equal(t, []string{"deploy"}, envelope.Workflow.FailedActions)
	assert.Equal(t, []string{"verify"}, envelope.Workflow.SkippedActions)
}

func TestBuildActionEnvelope_ResolverErrors(t *testing.T) {
	errors := []ResolverError{
		{Resolver: "config", Message: "timeout"},
	}

	envelope := BuildActionEnvelope(nil, nil, errors)

	assert.Equal(t, "failed", envelope.Status)
	require.NotNil(t, envelope.Success)
	assert.False(t, *envelope.Success)
	assert.Len(t, envelope.Errors, 1)
}

func TestBuildActionEnvelope_NilWorkflow(t *testing.T) {
	envelope := BuildActionEnvelope(map[string]any{"key": "val"}, nil, nil)

	assert.Equal(t, "success", envelope.Status)
	assert.Nil(t, envelope.Workflow)
	require.NotNil(t, envelope.Success)
	assert.True(t, *envelope.Success)
}

func TestBuildDryRunEnvelope_FromCapability(t *testing.T) {
	envelope := BuildDryRunEnvelope(false)

	assert.Equal(t, "success", envelope.Status)
	assert.NotNil(t, envelope.Resolvers)
	assert.Empty(t, envelope.Resolvers)
	assert.Empty(t, envelope.Errors)
	require.NotNil(t, envelope.DryRun)
	assert.True(t, *envelope.DryRun)
	assert.Nil(t, envelope.Workflow, "from dry-run should not have workflow")
	assert.Nil(t, envelope.Success, "from dry-run should not have success bool")
}

func TestBuildDryRunEnvelope_ActionCapability(t *testing.T) {
	envelope := BuildDryRunEnvelope(true)

	assert.Equal(t, "success", envelope.Status)
	assert.NotNil(t, envelope.Resolvers)
	assert.Empty(t, envelope.Resolvers)
	assert.Empty(t, envelope.Errors)
	require.NotNil(t, envelope.DryRun)
	assert.True(t, *envelope.DryRun)

	require.NotNil(t, envelope.Workflow)
	assert.Equal(t, "succeeded", envelope.Workflow.FinalStatus)
	assert.Empty(t, envelope.Workflow.FailedActions)
	assert.Empty(t, envelope.Workflow.SkippedActions)

	require.NotNil(t, envelope.Success)
	assert.True(t, *envelope.Success)
}

func TestEnvelope_ToMap_FromCapability(t *testing.T) {
	envelope := BuildFromEnvelope(
		map[string]any{"key": "value"},
		nil,
	)

	m := envelope.ToMap()

	assert.Equal(t, "success", m["status"])
	assert.Equal(t, map[string]any{"key": "value"}, m["resolvers"])
	assert.NotNil(t, m["errors"])

	// from capability should not include workflow or success
	_, hasWorkflow := m["workflow"]
	assert.False(t, hasWorkflow)
	_, hasSuccess := m["success"]
	assert.False(t, hasSuccess)
	_, hasDryRun := m["dryRun"]
	assert.False(t, hasDryRun)
}

func TestEnvelope_ToMap_ActionCapability(t *testing.T) {
	workflow := &WorkflowResult{
		FinalStatus:    "succeeded",
		FailedActions:  []string{},
		SkippedActions: []string{},
	}
	envelope := BuildActionEnvelope(
		map[string]any{"region": "us-east-1"},
		workflow,
		nil,
	)

	m := envelope.ToMap()

	assert.Equal(t, "success", m["status"])
	assert.Equal(t, true, m["success"])
	assert.Equal(t, map[string]any{"region": "us-east-1"}, m["resolvers"])

	wf, ok := m["workflow"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "succeeded", wf["finalStatus"])
}

func TestEnvelope_ToMap_DryRun(t *testing.T) {
	envelope := BuildDryRunEnvelope(true)

	m := envelope.ToMap()

	assert.Equal(t, true, m["dryRun"])
	assert.Equal(t, true, m["success"])
	assert.Equal(t, "success", m["status"])
}

func TestEnvelope_ToMap_WithErrors(t *testing.T) {
	errors := []ResolverError{
		{Resolver: "db-host", Message: "connection refused"},
		{Resolver: "api-key", Message: "not found"},
	}
	envelope := BuildFromEnvelope(map[string]any{}, errors)

	m := envelope.ToMap()

	errSlice, ok := m["errors"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, errSlice, 2)
	assert.Equal(t, "db-host", errSlice[0]["resolver"])
	assert.Equal(t, "connection refused", errSlice[0]["message"])
	assert.Equal(t, "api-key", errSlice[1]["resolver"])
}
