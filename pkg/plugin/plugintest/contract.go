// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugintest

import (
	"strings"
	"time"
)

const (
	// ProviderName is the provider the fixture serves unless Config.Providers
	// names others.
	ProviderName = "lifecycle-fixture"

	// ControlFile is the name of the JSON file, inside the sandbox directory,
	// from which the fixture reads its Config. A missing file means the zero
	// Config, i.e. a well-behaved plugin.
	ControlFile = "fixture.json"

	// StateDir is the sandbox subdirectory in which the fixture records, for
	// each process it runs as, a PID file (<pid>.pid), a call log (<pid>.calls)
	// and, on a clean shutdown, an exit record (<pid>.exit).
	StateDir = "fixture"

	// DefaultMaxLifetime is how long a fixture process lives before it
	// terminates itself. It is a safety net so that a test which dies before
	// its cleanup runs cannot leave a plugin running indefinitely.
	DefaultMaxLifetime = 3 * time.Minute
)

// Exit reasons recorded in <pid>.exit. A process that is killed, crashes, or
// calls os.Exit leaves no exit record at all.
const (
	// ExitGraceful means the go-plugin server shut down on request (the host
	// called Kill) and the fixture's main function ran to completion.
	ExitGraceful = "graceful"

	// ExitWatchdog means the fixture reached its maximum lifetime and
	// terminated itself.
	ExitWatchdog = "watchdog"
)

// Startup selects what the fixture does before the go-plugin handshake.
type Startup string

const (
	// StartupNormal serves the plugin normally.
	StartupNormal Startup = ""

	// StartupExit exits with Config.ExitCode before the handshake, as a
	// plugin that fails on startup would.
	StartupExit Startup = "exit"

	// StartupHang blocks forever before the handshake, as a plugin that never
	// finishes starting would.
	StartupHang Startup = "hang"

	// StartupBadCookie serves with a magic cookie the host does not accept, so
	// the go-plugin server refuses to start.
	StartupBadCookie Startup = "bad-cookie"
)

// ConfigureBehavior selects how the fixture answers ConfigureProvider.
type ConfigureBehavior string

const (
	// ConfigureOK answers normally, echoing the configured protocol version.
	ConfigureOK ConfigureBehavior = ""

	// ConfigureUnimplemented answers with gRPC Unimplemented, as a plugin
	// built before ConfigureProvider existed would.
	ConfigureUnimplemented ConfigureBehavior = "unimplemented"

	// ConfigureError answers with an application-level error in the response.
	ConfigureError ConfigureBehavior = "error"
)

// Config is the process-level behavior of the fixture. It is serialized as
// JSON to ControlFile in the sandbox directory before the plugin starts. The
// zero value is a well-behaved plugin.
//
// Behavior that varies per call is selected with the "action" key of the
// provider input instead; see the Action constants.
type Config struct {
	// Startup selects behavior before the go-plugin handshake.
	Startup Startup `json:"startup,omitempty"`

	// ExitCode is the process exit code used by StartupExit. Zero means 1.
	ExitCode int `json:"exitCode,omitempty"`

	// HandshakeVersion overrides the go-plugin handshake ProtocolVersion.
	// Zero keeps the SDK value. A value the host does not accept makes the
	// handshake fail.
	HandshakeVersion uint `json:"handshakeVersion,omitempty"`

	// ProtocolVersion overrides the protocol_version the fixture reports in
	// its ConfigureProvider response. Zero keeps the SDK value.
	ProtocolVersion int32 `json:"protocolVersion,omitempty"`

	// Configure selects the ConfigureProvider behavior.
	Configure ConfigureBehavior `json:"configure,omitempty"`

	// UnknownFields makes every response carry protobuf fields that are not
	// part of the published schema, as a newer plugin would send.
	UnknownFields bool `json:"unknownFields,omitempty"`

	// Providers lists the provider names served from this one process.
	// Empty means just ProviderName.
	Providers []string `json:"providers,omitempty"`

	// MaxLifetimeMillis overrides DefaultMaxLifetime. Zero keeps the default.
	MaxLifetimeMillis int `json:"maxLifetimeMillis,omitempty"`
}

// MaxLifetime returns the effective maximum lifetime of a fixture process.
func (c Config) MaxLifetime() time.Duration {
	if c.MaxLifetimeMillis > 0 {
		return time.Duration(c.MaxLifetimeMillis) * time.Millisecond
	}
	return DefaultMaxLifetime
}

// Actions understood in the "action" key of the provider input. Unless noted,
// an action works through both ExecuteProvider and ExecuteProviderStream; an
// absent or empty action behaves as ActionEcho.
const (
	// ActionEcho returns the provider name, the plugin process ID, a
	// process-wide call sequence number, and the input it was given.
	ActionEcho = "echo"

	// ActionEnv returns the values of the environment variables named in the
	// "keys" input, and the whole environment when "all" is true.
	ActionEnv = "env"

	// ActionSleep waits "ms" milliseconds, or until the call is cancelled,
	// and then echoes.
	ActionSleep = "sleep"

	// ActionHang blocks forever and ignores cancellation, as a wedged
	// provider would.
	ActionHang = "hang"

	// ActionCrash terminates the whole process with "exitCode" (default 3)
	// in the middle of the call.
	ActionCrash = "crash"

	// ActionPanic panics inside the RPC handler, which terminates the
	// process.
	ActionPanic = "panic"

	// ActionError fails the call with the "message" input and "exitCode".
	ActionError = "error"

	// ActionMalformed returns an undecodable response. The "kind" input is
	// one of the Malformed constants.
	ActionMalformed = "malformed"

	// ActionStream streams two stdout chunks, one stderr chunk and a result.
	// Streaming only; ExecuteProvider treats it as ActionEcho.
	ActionStream = "stream"

	// ActionPartialStream streams one stdout chunk and then ends the stream
	// without the mandatory result. The "kind" input is one of the Partial
	// constants. Streaming only; ExecuteProvider treats it as ActionEcho.
	ActionPartialStream = "partial-stream"

	// ActionAuthToken asks the host for an access token through the
	// HostService callback. Inputs: "handler", "scope".
	ActionAuthToken = "get-auth-token" //nolint:gosec // G101: an action name, not a credential

	// ActionAuthHandlers asks the host to list its auth handlers through the
	// HostService callback.
	ActionAuthHandlers = "list-auth-handlers"

	// ActionSecret reads a secret through the HostService callback.
	// Input: "name".
	ActionSecret = "get-secret"

	// ActionSecrets lists secret names through the HostService callback.
	// Input: "pattern".
	ActionSecrets = "list-secrets"
)

// Kinds of undecodable response produced by ActionMalformed.
const (
	// MalformedInvalid returns bytes that are not JSON.
	MalformedInvalid = "invalid"

	// MalformedArray returns valid JSON of the wrong shape (an array).
	MalformedArray = "array"

	// MalformedEmpty returns an empty response body.
	MalformedEmpty = "empty"

	// MalformedTruncated returns JSON cut off part-way through.
	MalformedTruncated = "truncated"
)

// Ways ActionPartialStream ends the stream early.
const (
	// PartialNoResult closes the stream cleanly without a result message.
	PartialNoResult = "no-result"

	// PartialAbort fails the stream with a gRPC error after the first chunk.
	PartialAbort = "abort"

	// PartialCrash terminates the process after the first chunk.
	PartialCrash = "crash"
)

// Call is one RPC the fixture served, as recorded in its call log.
type Call struct {
	// RPC is the gRPC method name, for example "ExecuteProvider".
	RPC string

	// Detail is a short, non-sensitive summary such as the action name.
	Detail string
}

// FormatCall renders a call log line, without the trailing newline.
func FormatCall(c Call) string {
	return c.RPC + "\t" + c.Detail
}

// ParseCall is the inverse of FormatCall.
func ParseCall(line string) Call {
	rpc, detail, _ := strings.Cut(line, "\t")
	return Call{RPC: rpc, Detail: detail}
}
