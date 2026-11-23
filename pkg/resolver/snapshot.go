// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
)

// Snapshot represents a complete snapshot of resolver execution
type Snapshot struct {
	Metadata   SnapshotMetadata             `json:"metadata" yaml:"metadata" doc:"Snapshot metadata"`
	Parameters map[string]any               `json:"parameters" yaml:"parameters" doc:"Parameters used in execution"`
	Resolvers  map[string]*SnapshotResolver `json:"resolvers" yaml:"resolvers" doc:"Resolver execution results"`
	Phases     []SnapshotPhase              `json:"phases" yaml:"phases" doc:"Phase execution information" maxItems:"100"`
}

// SnapshotMetadata contains metadata about the snapshot
type SnapshotMetadata struct {
	Solution      string          `json:"solution" yaml:"solution" doc:"Solution name" maxLength:"512" example:"my-solution"`
	Version       string          `json:"version,omitempty" yaml:"version,omitempty" doc:"Solution version" maxLength:"64" example:"1.0.0"`
	Timestamp     time.Time       `json:"timestamp" yaml:"timestamp" doc:"When snapshot was captured"`
	Runtime       SnapshotRuntime `json:"runtime" yaml:"runtime" doc:"Execution provenance (engine vs invoking CLI)"`
	TotalDuration string          `json:"totalDuration" yaml:"totalDuration" doc:"Total execution duration" maxLength:"32" example:"1.5s"`
	Status        string          `json:"status" yaml:"status" doc:"Overall execution status (success, failed)" maxLength:"32" example:"success"`
}

// SnapshotRuntime records execution provenance, separating the engine (scafctl
// library) identity from the invoking CLI/frontend identity. When scafctl runs
// directly (not embedded), CLI mirrors Engine.
type SnapshotRuntime struct {
	Engine SnapshotRuntimeComponent `json:"engine" yaml:"engine" doc:"Execution engine (scafctl library) identity"`
	CLI    SnapshotRuntimeComponent `json:"cli" yaml:"cli" doc:"Invoking CLI/frontend identity"`
}

// SnapshotRuntimeComponent identifies a named, versioned runtime participant.
type SnapshotRuntimeComponent struct {
	Name    string `json:"name" yaml:"name" doc:"Component name" maxLength:"64" example:"scafctl"`
	Version string `json:"version" yaml:"version" doc:"Component version" maxLength:"64" example:"0.5.0"`
}

// SnapshotResolver contains execution result for a single resolver
type SnapshotResolver struct {
	Value          any                     `json:"value" yaml:"value" doc:"Resolved value (or <redacted> if sensitive)"`
	Status         string                  `json:"status" yaml:"status" doc:"Execution status (success, failed, skipped)" maxLength:"32" example:"success"`
	Phase          int                     `json:"phase" yaml:"phase" doc:"Execution phase number" maximum:"100" example:"1"`
	Duration       string                  `json:"duration" yaml:"duration" doc:"Execution duration" maxLength:"32" example:"250ms"`
	ProviderCalls  int                     `json:"providerCalls" yaml:"providerCalls" doc:"Number of provider calls made" maximum:"1000" example:"3"`
	ValueSizeBytes int64                   `json:"valueSizeBytes,omitempty" yaml:"valueSizeBytes,omitempty" doc:"Size of value in bytes" maximum:"1073741824" example:"1024"`
	Sensitive      bool                    `json:"sensitive,omitempty" yaml:"sensitive,omitempty" doc:"Whether value was redacted"`
	Error          string                  `json:"error,omitempty" yaml:"error,omitempty" doc:"Error message if failed" maxLength:"4096" example:"connection refused"`
	FailedAttempts []SnapshotFailedAttempt `json:"failedAttempts,omitempty" yaml:"failedAttempts,omitempty" doc:"Failed provider attempts (onError: continue)" maxItems:"100"`
}

// SnapshotFailedAttempt contains information about a failed provider attempt
type SnapshotFailedAttempt struct {
	Provider   string `json:"provider" yaml:"provider" doc:"Provider name" maxLength:"128" example:"http"`
	SourceStep int    `json:"sourceStep" yaml:"sourceStep" doc:"Source/step index in phase" maximum:"100" example:"0"`
	Error      string `json:"error" yaml:"error" doc:"Error message" maxLength:"4096" example:"timeout"`
	Duration   string `json:"duration" yaml:"duration" doc:"Time spent on this attempt" maxLength:"32" example:"5s"`
	Timestamp  string `json:"timestamp" yaml:"timestamp" doc:"When attempt occurred" maxLength:"64" example:"2026-01-29T12:00:00Z"`
}

// SnapshotPhase contains information about a phase execution
type SnapshotPhase struct {
	Phase     int      `json:"phase" yaml:"phase" doc:"Phase number (1-based)" maximum:"100" example:"1"`
	Duration  string   `json:"duration" yaml:"duration" doc:"Phase execution duration" maxLength:"32" example:"500ms"`
	Resolvers []string `json:"resolvers" yaml:"resolvers" doc:"Resolver names in this phase" maxItems:"500"`
}

// snapshotRuntime builds execution provenance for a snapshot from the shared
// settings primitive, applying the CLI-mirrors-engine fallback. The engine is
// always scafctl at buildVersion; the CLI is the configured binary name and
// embedder version (from context), defaulting to the engine when not embedded.
func snapshotRuntime(ctx context.Context, buildVersion string) SnapshotRuntime {
	p := settings.RuntimeProvenanceFromContext(ctx, buildVersion)
	return SnapshotRuntime{
		Engine: SnapshotRuntimeComponent{Name: p.EngineName, Version: p.EngineVersion},
		CLI:    SnapshotRuntimeComponent{Name: p.ResolvedCLIName(), Version: p.ResolvedCLIVersion()},
	}
}

// CaptureSnapshot creates a snapshot from execution context and results
func CaptureSnapshot(
	ctx context.Context,
	solutionName string,
	solutionVersion string,
	buildVersion string,
	parameters map[string]any,
	totalDuration time.Duration,
	overallStatus ExecutionStatus,
) (*Snapshot, error) {
	// Get resolver context from context
	resolverCtx, ok := FromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("resolver context not found in context")
	}

	results := resolverCtx.GetAllResults()
	tracker := redact.FromContext(ctx)

	scrubbedParams := parameters
	if tracker != nil && parameters != nil {
		scrubbedParams = tracker.RedactMap(parameters)
	}

	snapshot := &Snapshot{
		Metadata: SnapshotMetadata{
			Solution:      solutionName,
			Version:       solutionVersion,
			Timestamp:     time.Now().UTC(),
			Runtime:       snapshotRuntime(ctx, buildVersion),
			TotalDuration: format.Duration(totalDuration),
			Status:        string(overallStatus),
		},
		Parameters: scrubbedParams,
		Resolvers:  make(map[string]*SnapshotResolver),
		Phases:     []SnapshotPhase{},
	}

	// Group resolvers by phase
	phaseResolvers := make(map[int][]string)

	// Convert execution results to snapshot resolvers
	for name, result := range results {
		sr := &SnapshotResolver{
			Status:        string(result.Status),
			Phase:         result.Phase,
			Duration:      format.Duration(result.TotalDuration),
			ProviderCalls: result.ProviderCallCount,
		}

		// Unconditionally mask credential-backed entries and scrub any tracked
		// secret substrings so snapshots never persist live host credentials
		// even when captured without --redact.
		if result.CredentialBacked {
			sr.Value = "<redacted>"
			sr.Sensitive = true
			sr.ValueSizeBytes = 0
		} else {
			val := result.Value
			if tracker != nil {
				val = tracker.RedactAny(val)
			}
			sr.Value = val
			if result.ValueSizeBytes > 0 {
				sr.ValueSizeBytes = result.ValueSizeBytes
			}
		}

		// Error
		if result.Error != nil {
			errMsg := result.Error.Error()
			if tracker != nil {
				errMsg = tracker.RedactString(errMsg)
			}
			sr.Error = errMsg
		}

		// Failed attempts
		if len(result.FailedAttempts) > 0 {
			sr.FailedAttempts = make([]SnapshotFailedAttempt, len(result.FailedAttempts))
			for i, attempt := range result.FailedAttempts {
				attErr := attempt.Error
				if tracker != nil {
					attErr = tracker.RedactString(attErr)
				}
				sr.FailedAttempts[i] = SnapshotFailedAttempt{
					Provider:   attempt.Provider,
					SourceStep: attempt.SourceStep,
					Error:      attErr,
					Duration:   format.Duration(attempt.Duration),
					Timestamp:  attempt.Timestamp.Format(time.RFC3339),
				}
			}
		}

		snapshot.Resolvers[name] = sr

		// Track phase membership
		phaseResolvers[result.Phase] = append(phaseResolvers[result.Phase], name)
	}

	// Build phase information
	for phase := 1; phase <= len(phaseResolvers); phase++ {
		if resolvers, ok := phaseResolvers[phase]; ok {
			// Calculate phase duration (max of all resolvers in phase)
			var maxDuration time.Duration
			for _, name := range resolvers {
				if result, ok := results[name]; ok {
					if result.TotalDuration > maxDuration {
						maxDuration = result.TotalDuration
					}
				}
			}

			snapshot.Phases = append(snapshot.Phases, SnapshotPhase{
				Phase:     phase,
				Duration:  format.Duration(maxDuration),
				Resolvers: resolvers,
			})
		}
	}

	return snapshot, nil
}

// ResolverLike is an interface for objects that have a Name and Sensitive flag
// nolint:revive // ResolverLike name is intentional to indicate compatibility with Resolver type
type ResolverLike interface {
	GetName() string
	GetSensitive() bool
}

// RedactSensitiveValues redacts sensitive values in the snapshot based on resolver-like objects
func RedactSensitiveValues(snapshot *Snapshot, resolvers []ResolverLike) {
	// Build map of sensitive resolvers
	sensitiveMap := make(map[string]bool)
	for _, r := range resolvers {
		if r.GetSensitive() {
			sensitiveMap[r.GetName()] = true
		}
	}

	// Redact values for sensitive resolvers
	for name, sr := range snapshot.Resolvers {
		if sensitiveMap[name] {
			sr.Value = "<redacted>"
			sr.Sensitive = true
			// Clear value size for redacted values
			sr.ValueSizeBytes = 0
		}
	}
}

// SaveSnapshot saves a snapshot to a JSON file, enforcing owner-only (0600)
// permissions even when overwriting a pre-existing file with broader permissions.
func SaveSnapshot(snapshot *Snapshot, filePath string) error {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Open without O_TRUNC first so an existing 0644 file's permissions are
	// tightened to 0600 before new snapshot bytes are written to the inode.
	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE, 0o600) //nolint:gosec // filePath is caller-supplied CLI --snapshot-file destination
	if err != nil {
		return fmt.Errorf("failed to write snapshot file: %w", err)
	}

	writeErr := func() error {
		if runtime.GOOS != "windows" {
			if err := f.Chmod(0o600); err != nil {
				return err
			}
		}
		if err := f.Truncate(0); err != nil {
			return err
		}
		if _, err := f.Write(data); err != nil {
			return err
		}
		return nil
	}()
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("failed to write snapshot file: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to write snapshot file: %w", closeErr)
	}
	if runtime.GOOS == "windows" {
		if err := os.Chmod(filePath, 0o600); err != nil {
			return fmt.Errorf("failed to write snapshot file: %w", err)
		}
	}

	return nil
}

// LoadSnapshot loads a snapshot from a JSON file
func LoadSnapshot(filePath string) (*Snapshot, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot file: %w", err)
	}

	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	return &snapshot, nil
}
