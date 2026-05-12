// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package fingerprint provides file-based fingerprinting for action up-to-date checks.
// It computes SHA-256 hashes over glob-expanded file sets and compares them against
// stored state to determine whether an action needs re-execution.
package fingerprint

import (
	"context"
	"errors"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/state"
)

// Reason categorizes why an action is stale or up-to-date.
type Reason string

const (
	// ReasonFirstRun indicates no prior fingerprint exists in state.
	ReasonFirstRun Reason = "first run"

	// ReasonSourcesChanged indicates source files have changed since last run.
	ReasonSourcesChanged Reason = "sources changed"

	// ReasonGeneratesModified indicates generated files were modified externally.
	ReasonGeneratesModified Reason = "generates modified"

	// ReasonGeneratesMissing indicates one or more generated files don't exist.
	ReasonGeneratesMissing Reason = "generates missing"

	// ReasonInputsChanged indicates resolved action inputs have changed since last run.
	ReasonInputsChanged Reason = "inputs changed"

	// ReasonUpToDate indicates all fingerprints match -- action can be skipped.
	ReasonUpToDate Reason = "up-to-date"
)

// Result holds the comparison outcome for an action's sources and generates.
type Result struct {
	// Stale is true when the action should be re-executed.
	Stale bool

	// CurrentHash is the SHA-256 hash of current source files.
	CurrentHash string

	// PreviousHash is the stored hash of sources from last successful run (empty on first run).
	PreviousHash string

	// GeneratesHash is the SHA-256 hash of current generated files.
	GeneratesHash string

	// PreviousGeneratesHash is the stored hash of generates from last successful run.
	PreviousGeneratesHash string

	// InputsHash is the SHA-256 hash of current resolved inputs.
	InputsHash string

	// PreviousInputsHash is the stored hash of inputs from last successful run.
	PreviousInputsHash string

	// Reason provides a human-readable explanation.
	Reason Reason
}

// Checker evaluates whether an action's sources/generates have changed since the last run.
type Checker struct {
	stateData *state.Data
}

// NewChecker creates a fingerprint checker backed by the given state data.
// If stateData is nil, all checks will report stale (first-run behavior).
func NewChecker(stateData *state.Data) *Checker {
	return &Checker{stateData: stateData}
}

// CheckFiles performs Phase 1 of the two-phase fingerprint check: it compares
// source/generates file hashes against stored state. If the returned Result has
// Stale==true, the caller should execute the action without calling CheckInputs.
// If Stale==false, the caller should resolve inputs and call CheckInputs.
//
// Staleness logic:
//  1. No previous state -> stale (first run)
//  2. Sources hash mismatch -> stale (sources changed)
//  3. If generates declared: generated files missing -> stale
//  4. If generates declared: generates hash mismatch -> stale (externally modified)
//  5. All file hashes match -> not stale (proceed to CheckInputs)
func (c *Checker) CheckFiles(_ context.Context, actionName string, sources, generates []string, baseDir string) (*Result, error) {
	result := &Result{}

	// Compute current sources hash
	currentHash, err := HashFiles(baseDir, sources)
	if err != nil {
		return nil, err
	}
	result.CurrentHash = currentHash

	// Load previous sources hash from state
	result.PreviousHash = LoadSourcesHash(c.stateData, actionName)

	// First run: no previous hash
	if result.PreviousHash == "" {
		result.Stale = true
		result.Reason = ReasonFirstRun
		return result, nil
	}

	// Sources changed
	if currentHash != result.PreviousHash {
		result.Stale = true
		result.Reason = ReasonSourcesChanged
		return result, nil
	}

	// If generates are declared, check them too
	if len(generates) > 0 {
		genHash, genErr := HashFiles(baseDir, generates)
		if genErr != nil {
			// If generates don't exist (ErrNoMatches), they're missing
			if isNoMatchError(genErr) {
				result.Stale = true
				result.Reason = ReasonGeneratesMissing
				return result, nil
			}
			return nil, genErr
		}
		result.GeneratesHash = genHash

		// Load previous generates hash
		result.PreviousGeneratesHash = LoadGeneratesHash(c.stateData, actionName)

		// No previous generates hash (first run with generates)
		if result.PreviousGeneratesHash == "" {
			result.Stale = true
			result.Reason = ReasonFirstRun
			return result, nil
		}

		// Generates modified externally
		if genHash != result.PreviousGeneratesHash {
			result.Stale = true
			result.Reason = ReasonGeneratesModified
			return result, nil
		}
	}

	// File hashes all match -- caller should proceed to CheckInputs
	result.Stale = false
	result.Reason = ReasonUpToDate
	return result, nil
}

// CheckInputs performs Phase 2 of the two-phase fingerprint check: it compares
// resolved input hashes against stored state. Only call this when CheckFiles
// returned Stale==false (files match). resolvedInputs is the full map returned
// by the executor's resolveInputs().
func (c *Checker) CheckInputs(_ context.Context, actionName string, resolvedInputs map[string]any) (*Result, error) {
	result := &Result{}

	inputsHash, err := HashInputs(resolvedInputs)
	if err != nil {
		return nil, fmt.Errorf("hashing inputs for action %q: %w", actionName, err)
	}
	result.InputsHash = inputsHash

	// Load previous inputs hash from state
	result.PreviousInputsHash = LoadInputsHash(c.stateData, actionName)

	// Both empty means no inputs were ever involved -- up-to-date
	if inputsHash == "" && result.PreviousInputsHash == "" {
		result.Stale = false
		result.Reason = ReasonUpToDate
		return result, nil
	}

	// No previous inputs hash -- treat as first run (handles state migration)
	if result.PreviousInputsHash == "" {
		result.Stale = true
		result.Reason = ReasonFirstRun
		return result, nil
	}

	// Inputs changed
	if inputsHash != result.PreviousInputsHash {
		result.Stale = true
		result.Reason = ReasonInputsChanged
		return result, nil
	}

	// Everything matches -- up-to-date
	result.Stale = false
	result.Reason = ReasonUpToDate
	return result, nil
}

// Check compares current file fingerprints and resolved inputs against stored state.
// This is a convenience method that calls CheckFiles and CheckInputs sequentially.
// For two-phase usage in the executor, prefer calling CheckFiles and CheckInputs separately.
func (c *Checker) Check(ctx context.Context, actionName string, sources, generates []string, baseDir string, resolvedInputs map[string]any) (*Result, error) {
	fileResult, err := c.CheckFiles(ctx, actionName, sources, generates, baseDir)
	if err != nil {
		return nil, err
	}
	if fileResult.Stale {
		return fileResult, nil
	}

	inputResult, err := c.CheckInputs(ctx, actionName, resolvedInputs)
	if err != nil {
		return nil, err
	}

	// Merge file hashes into input result for completeness
	inputResult.CurrentHash = fileResult.CurrentHash
	inputResult.PreviousHash = fileResult.PreviousHash
	inputResult.GeneratesHash = fileResult.GeneratesHash
	inputResult.PreviousGeneratesHash = fileResult.PreviousGeneratesHash

	return inputResult, nil
}

// Record stores the current fingerprints (sources + generates + inputs) after a
// successful action. Call this after action execution to persist the new baseline.
func (c *Checker) Record(_ context.Context, actionName string, sources, generates []string, baseDir string, resolvedInputs map[string]any) error {
	sourcesHash, err := HashFiles(baseDir, sources)
	if err != nil {
		return err
	}

	var generatesHash string
	if len(generates) > 0 {
		generatesHash, err = HashFiles(baseDir, generates)
		if err != nil {
			// Generates might not exist yet if action failed to produce them.
			// Still store sources hash for comparison on next run.
			if !isNoMatchError(err) {
				return err
			}
		}
	}

	var inputsHash string
	if len(resolvedInputs) > 0 {
		inputsHash, err = HashInputs(resolvedInputs)
		if err != nil {
			return fmt.Errorf("hashing inputs for action %q: %w", actionName, err)
		}
	}

	SaveHashes(c.stateData, actionName, sourcesHash, generatesHash, inputsHash)
	return nil
}

// isNoMatchError checks if an error is due to glob pattern matching no files.
func isNoMatchError(err error) bool {
	if err == nil {
		return false
	}
	// Use string check since the error may be wrapped
	return err.Error() == ErrNoMatches.Error() || containsError(err, ErrNoMatches)
}

// containsError checks if target is in the error chain.
func containsError(err, target error) bool {
	return errors.Is(err, target)
}
