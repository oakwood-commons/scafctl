// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solutionprovider

// ResolverError represents a single resolver failure in the output envelope.
type ResolverError struct {
	Resolver string `json:"resolver" yaml:"resolver" doc:"Name of the resolver that failed"`
	Message  string `json:"message" yaml:"message" doc:"Error description"`
}

// WorkflowResult is the aggregate workflow status in the output envelope.
type WorkflowResult struct {
	FinalStatus    string   `json:"finalStatus" yaml:"finalStatus" doc:"Aggregate workflow status: succeeded, failed, cancelled, or partial-success"`
	FailedActions  []string `json:"failedActions" yaml:"failedActions" doc:"Names of actions that failed"`
	SkippedActions []string `json:"skippedActions" yaml:"skippedActions" doc:"Names of actions that were skipped"`
}

// Envelope is the output structure returned by the solution provider.
// The shape varies by capability: `from` returns resolvers + status,
// `action` adds workflow results and a success boolean.
type Envelope struct {
	Resolvers map[string]any  `json:"resolvers" yaml:"resolvers" doc:"Resolver values from the sub-solution"`
	Workflow  *WorkflowResult `json:"workflow,omitempty" yaml:"workflow,omitempty" doc:"Aggregate workflow status (action capability only)"`
	Status    string          `json:"status" yaml:"status" doc:"Overall status: success or failed"`
	Errors    []ResolverError `json:"errors" yaml:"errors" doc:"Resolver errors encountered during execution"`
	Success   *bool           `json:"success,omitempty" yaml:"success,omitempty" doc:"Whether the solution succeeded (action capability only)"`
	DryRun    *bool           `json:"dryRun,omitempty" yaml:"dryRun,omitempty" doc:"Present and true when executed in dry-run mode"`
}

// BuildFromEnvelope constructs the output envelope for the `from` capability.
// It contains resolver values and any errors encountered.
func BuildFromEnvelope(resolverData map[string]any, errors []ResolverError) *Envelope {
	status := "success"
	if len(errors) > 0 {
		status = "failed"
	}

	if resolverData == nil {
		resolverData = map[string]any{}
	}

	if errors == nil {
		errors = []ResolverError{}
	}

	return &Envelope{
		Resolvers: resolverData,
		Status:    status,
		Errors:    errors,
	}
}

// BuildActionEnvelope constructs the output envelope for the `action` capability.
// It includes resolver values, workflow aggregate status, and a success boolean.
func BuildActionEnvelope(resolverData map[string]any, workflowResult *WorkflowResult, errors []ResolverError) *Envelope {
	status := "success"
	if len(errors) > 0 {
		status = "failed"
	}

	success := status == "success"
	if workflowResult != nil && workflowResult.FinalStatus != "succeeded" {
		status = "failed"
		success = false
	}

	if resolverData == nil {
		resolverData = map[string]any{}
	}

	if errors == nil {
		errors = []ResolverError{}
	}

	return &Envelope{
		Resolvers: resolverData,
		Workflow:  workflowResult,
		Status:    status,
		Errors:    errors,
		Success:   &success,
	}
}

// BuildDryRunEnvelope constructs a mock envelope for dry-run mode.
// When isAction is true, it includes workflow and success fields.
func BuildDryRunEnvelope(isAction bool) *Envelope {
	dryRun := true

	envelope := &Envelope{
		Resolvers: map[string]any{},
		Status:    "success",
		Errors:    []ResolverError{},
		DryRun:    &dryRun,
	}

	if isAction {
		success := true
		envelope.Workflow = &WorkflowResult{
			FinalStatus:    "succeeded",
			FailedActions:  []string{},
			SkippedActions: []string{},
		}
		envelope.Success = &success
	}

	return envelope
}

// ToMap converts an Envelope to a map[string]any for use as provider.Output.Data.
// This avoids requiring the consumer to know about the Envelope struct.
func (e *Envelope) ToMap() map[string]any {
	result := map[string]any{
		"resolvers": e.Resolvers,
		"status":    e.Status,
		"errors":    resolverErrorsToMaps(e.Errors),
	}

	if e.Workflow != nil {
		result["workflow"] = map[string]any{
			"finalStatus":    e.Workflow.FinalStatus,
			"failedActions":  e.Workflow.FailedActions,
			"skippedActions": e.Workflow.SkippedActions,
		}
	}

	if e.Success != nil {
		result["success"] = *e.Success
	}

	if e.DryRun != nil {
		result["dryRun"] = *e.DryRun
	}

	return result
}

// resolverErrorsToMaps converts a slice of ResolverError to a slice of maps
// for inclusion in the output envelope.
func resolverErrorsToMaps(errors []ResolverError) []map[string]any {
	result := make([]map[string]any, len(errors))
	for i, e := range errors {
		result[i] = map[string]any{
			"resolver": e.Resolver,
			"message":  e.Message,
		}
	}
	return result
}
