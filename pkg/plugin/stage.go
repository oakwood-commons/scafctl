// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import "errors"

// Stage identifies the step of plugin acquisition (resolve, fetch, verify,
// spawn) that produced an error. It turns an otherwise generic "upstream plugin
// failure" into something that can be grouped in traces and logs.
type Stage string

// Plugin acquisition stages.
const (
	// StageBind is binding a dependency to its catalog, including local-build
	// source policy.
	StageBind Stage = "bind"
	// StageLock is classifying the dependency's lock file entry.
	StageLock Stage = "lock"
	// StageResolve is resolving a version selector to a concrete version and
	// digest, from the catalog or the lock file.
	StageResolve Stage = "resolve"
	// StageCache is writing the downloaded binary to the local cache.
	StageCache Stage = "cache"
	// StageDownload is downloading the binary from the catalog.
	StageDownload Stage = "download"
	// StageDigest is verifying the downloaded binary's content digest.
	StageDigest Stage = "digest"
	// StageSignature is verifying the binary's signature.
	StageSignature Stage = "signature"
	// StageSpawn is starting the plugin process.
	StageSpawn Stage = "spawn"
	// StageHandshake is listing the providers exposed by a started plugin.
	StageHandshake Stage = "handshake"
)

// StageError tags an error with the acquisition stage that produced it. It is
// transparent to errors.Is and errors.As (via Unwrap), and its message is the
// wrapped error's message, so tagging never changes what callers see.
type StageError struct {
	Stage Stage
	Err   error
}

// Error returns the wrapped error's message.
func (e *StageError) Error() string {
	return e.Err.Error()
}

// Unwrap returns the wrapped error.
func (e *StageError) Unwrap() error {
	return e.Err
}

// withStage tags err with stage. It returns nil for a nil err, and leaves an
// error that already carries a stage unchanged so the innermost (most
// specific) stage wins.
func withStage(stage Stage, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := ErrorStage(err); ok {
		return err
	}
	return &StageError{Stage: stage, Err: err}
}

// ErrorStage reports the acquisition stage recorded anywhere in err's chain.
func ErrorStage(err error) (Stage, bool) {
	var se *StageError
	if errors.As(err, &se) {
		return se.Stage, true
	}
	return "", false
}
