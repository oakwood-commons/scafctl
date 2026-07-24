// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package diagnose provides reusable auth diagnostic checks.
// The types and pure-check functions are consumed by the CLI
// command (pkg/cmd/scafctl/auth/diagnose.go) and MCP tools.
package diagnose

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/secrets"
)

// CheckStatus represents the result of a single diagnostic check.
type CheckStatus string

const (
	StatusOK   CheckStatus = "ok"
	StatusWarn CheckStatus = "warn"
	StatusFail CheckStatus = "fail"
	StatusInfo CheckStatus = "info"
)

// Check represents one diagnostic check result.
type Check struct {
	Category string      `json:"category" yaml:"category" doc:"Diagnostic category (registry, config, env, clock, handler, cache, live)"`
	Name     string      `json:"check" yaml:"check" doc:"Human-readable check name"`
	Status   CheckStatus `json:"status" yaml:"status" doc:"Result status: ok, warn, fail, info"`
	Message  string      `json:"message" yaml:"message" doc:"Descriptive message about the check result"`
}

// RunEnvVarChecks checks common environment variables for all known auth handlers.
func RunEnvVarChecks() []Check {
	var checks []Check

	entraVars := []struct {
		name, desc string
	}{
		{"AZURE_CLIENT_ID", "Entra service principal client ID"},
		{"AZURE_TENANT_ID", "Entra tenant ID"},
		{"AZURE_CLIENT_SECRET", "Entra client secret (service principal)"},
		{"AZURE_FEDERATED_TOKEN_FILE", "Entra workload identity token file path"},
		{"AZURE_FEDERATED_TOKEN", "Entra workload identity token (raw)"},
	}
	for _, v := range entraVars {
		val := os.Getenv(v.name)
		if val != "" {
			checks = append(checks, Check{
				Category: "env",
				Name:     fmt.Sprintf("env %s", v.name),
				Status:   StatusOK,
				Message:  fmt.Sprintf("%s \u2014 set (%s)", v.desc, v.name),
			})
		}
	}
	if os.Getenv("AZURE_CLIENT_ID") != "" && os.Getenv("AZURE_TENANT_ID") != "" && os.Getenv("AZURE_CLIENT_SECRET") != "" {
		checks = append(checks, Check{
			Category: "env",
			Name:     "env entra: service-principal credentials",
			Status:   StatusOK,
			Message:  "AZURE_CLIENT_ID + AZURE_TENANT_ID + AZURE_CLIENT_SECRET are all set",
		})
	}
	if os.Getenv("AZURE_CLIENT_ID") != "" && os.Getenv("AZURE_TENANT_ID") != "" && (os.Getenv("AZURE_FEDERATED_TOKEN_FILE") != "" || os.Getenv("AZURE_FEDERATED_TOKEN") != "") {
		checks = append(checks, Check{
			Category: "env",
			Name:     "env entra: workload-identity credentials",
			Status:   StatusOK,
			Message:  "workload identity environment detected (AZURE_FEDERATED_TOKEN_FILE or AZURE_FEDERATED_TOKEN)",
		})
	}

	ghVars := []struct{ name, desc string }{
		{"GITHUB_TOKEN", "GitHub personal access token"},
		{"GH_TOKEN", "GitHub personal access token (alternate)"},
	}
	for _, v := range ghVars {
		if os.Getenv(v.name) != "" {
			checks = append(checks, Check{
				Category: "env",
				Name:     fmt.Sprintf("env %s", v.name),
				Status:   StatusOK,
				Message:  fmt.Sprintf("%s \u2014 set", v.desc),
			})
		}
	}
	if os.Getenv("GITHUB_TOKEN") != "" || os.Getenv("GH_TOKEN") != "" {
		checks = append(checks, Check{
			Category: "env",
			Name:     "env github: PAT credentials",
			Status:   StatusOK,
			Message:  "GITHUB_TOKEN or GH_TOKEN is set",
		})
	}

	gcpVars := []struct{ name, desc string }{
		{"GOOGLE_APPLICATION_CREDENTIALS", "GCP service account key file path"},
		{"GOOGLE_EXTERNAL_ACCOUNT", "GCP workload identity external account config"},
		{"GOOGLE_CLOUD_PROJECT", "GCP project ID"},
	}
	for _, v := range gcpVars {
		if os.Getenv(v.name) != "" {
			checks = append(checks, Check{
				Category: "env",
				Name:     fmt.Sprintf("env %s", v.name),
				Status:   StatusOK,
				Message:  fmt.Sprintf("%s \u2014 set", v.desc),
			})
		}
	}
	if credPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); credPath != "" {
		checks = append(checks, validateGCPCredentialsFile(credPath))
	}
	if extPath := os.Getenv("GOOGLE_EXTERNAL_ACCOUNT"); extPath != "" {
		checks = append(checks, validateGCPExternalAccountFile(extPath))
	}

	if len(checks) == 0 {
		checks = append(checks, Check{
			Category: "env",
			Name:     "env: credential variables",
			Status:   StatusInfo,
			Message:  "no auth-related environment variables detected (interactive login may still work)",
		})
	}

	return checks
}

// validateGCPExternalAccountFile checks whether the file pointed to by
// GOOGLE_EXTERNAL_ACCOUNT exists, is readable, and contains an
// external_account JSON type. Returns a diagnostic Check with the result.
func validateGCPExternalAccountFile(extPath string) Check {
	const checkName = "env gcp: workload-identity credentials"

	data, err := os.ReadFile(extPath) //nolint:gosec // path comes from the user's own env var
	if err != nil {
		return Check{
			Category: "env",
			Name:     checkName,
			Status:   StatusWarn,
			Message:  fmt.Sprintf("GOOGLE_EXTERNAL_ACCOUNT is set but file is not readable: %v", err),
		}
	}

	var creds struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return Check{
			Category: "env",
			Name:     checkName,
			Status:   StatusWarn,
			Message:  "GOOGLE_EXTERNAL_ACCOUNT is set but file is not valid JSON",
		}
	}

	if creds.Type != "external_account" {
		return Check{
			Category: "env",
			Name:     checkName,
			Status:   StatusWarn,
			Message:  fmt.Sprintf("GOOGLE_EXTERNAL_ACCOUNT is set but JSON type is %q, not \"external_account\"", creds.Type),
		}
	}

	return Check{
		Category: "env",
		Name:     checkName,
		Status:   StatusOK,
		Message:  "GCP workload identity environment detected",
	}
}

// validateGCPCredentialsFile checks whether the file pointed to by
// GOOGLE_APPLICATION_CREDENTIALS exists, is readable, and contains a
// service_account JSON type. Returns a diagnostic Check with the result.
func validateGCPCredentialsFile(credPath string) Check {
	const checkName = "env gcp: service-account credentials"

	data, err := os.ReadFile(credPath) //nolint:gosec // path comes from the user's own env var
	if err != nil {
		return Check{
			Category: "env",
			Name:     checkName,
			Status:   StatusWarn,
			Message:  fmt.Sprintf("GOOGLE_APPLICATION_CREDENTIALS is set but file is not readable: %v", err),
		}
	}

	var creds struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return Check{
			Category: "env",
			Name:     checkName,
			Status:   StatusWarn,
			Message:  "GOOGLE_APPLICATION_CREDENTIALS is set but file is not valid JSON",
		}
	}

	if creds.Type != "service_account" {
		return Check{
			Category: "env",
			Name:     checkName,
			Status:   StatusWarn,
			Message:  fmt.Sprintf("GOOGLE_APPLICATION_CREDENTIALS is set but JSON type is %q, not \"service_account\"", creds.Type),
		}
	}

	return Check{
		Category: "env",
		Name:     checkName,
		Status:   StatusOK,
		Message:  "GOOGLE_APPLICATION_CREDENTIALS is set and contains a valid service account key",
	}
}

// RunClockSkewCheck compares the local system clock against the Date header
// returned by a well-known HTTPS endpoint (cloudflare.com).
// A skew > 5 minutes can cause token validation failures.
func RunClockSkewCheck() Check {
	return runClockSkewCheck(&http.Client{Timeout: 4 * time.Second}, "https://cloudflare.com")
}

// runClockSkewCheck is the testable implementation of RunClockSkewCheck.
func runClockSkewCheck(client *http.Client, endpoint string) Check {
	const maxSkew = 5 * time.Minute

	before := time.Now()
	resp, err := client.Head(endpoint) //nolint:noctx // no context needed for a simple diagnostic probe
	if err != nil {
		return Check{
			Category: "clock",
			Name:     "clock skew",
			Status:   StatusWarn,
			Message:  fmt.Sprintf("could not reach %s to check clock skew: %v", endpoint, err),
		}
	}
	defer resp.Body.Close()
	after := time.Now()
	localMid := before.Add(after.Sub(before) / 2)

	dateHeader := resp.Header.Get("Date")
	if dateHeader == "" {
		return Check{
			Category: "clock",
			Name:     "clock skew",
			Status:   StatusInfo,
			Message:  fmt.Sprintf("no Date header returned by %s; cannot check clock skew", endpoint),
		}
	}

	serverTime, err := http.ParseTime(dateHeader)
	if err != nil {
		return Check{
			Category: "clock",
			Name:     "clock skew",
			Status:   StatusWarn,
			Message:  fmt.Sprintf("could not parse Date header %q: %v", dateHeader, err),
		}
	}

	skew := localMid.Sub(serverTime)
	if skew < 0 {
		skew = -skew
	}

	if skew > maxSkew {
		return Check{
			Category: "clock",
			Name:     "clock skew",
			Status:   StatusFail,
			Message:  fmt.Sprintf("clock skew is %s (local: %s, server: %s) \u2014 token validation may fail (JWT nbf/exp checks require skew < 5m)", skew.Round(time.Second), localMid.UTC().Format(time.RFC3339), serverTime.UTC().Format(time.RFC3339)),
		}
	}

	return Check{
		Category: "clock",
		Name:     "clock skew",
		Status:   StatusOK,
		Message:  fmt.Sprintf("clock skew is %s (within acceptable range)", skew.Round(time.Millisecond)),
	}
}

// secretsProbeFunc probes the master-key acquisition path in a read-only way.
// It is a package var so tests can substitute a deterministic probe instead of
// touching the real OS keyring / filesystem.
var secretsProbeFunc = probeSecretsStore

// secretsProbeResult captures the read-only outcome of a master-key probe.
type secretsProbeResult struct {
	// backend is the keyring backend that satisfied the read (os/env/file), or
	// empty when no master key exists yet.
	backend string
	// found is true when an existing master key was successfully read.
	found bool
	// err is a genuine hard access error (never ErrKeyNotFound); nil otherwise.
	err error
}

// probeSecretsStore attempts to read the existing master key from the default
// keyring chain WITHOUT mutating anything. Unlike secrets.New(), it never
// generates a key, never persists to a backend, and never touches encrypted
// secrets. A missing key (ErrKeyNotFound) is reported as found=false, err=nil --
// the store would create one on first real use. Any other error is a genuine
// keyring access failure that would break login.
func probeSecretsStore() secretsProbeResult {
	kr := secrets.NewDefaultKeyring()
	_, err := secrets.GetMasterKeyFromKeyring(kr)
	if err == nil {
		backend := ""
		if br, ok := kr.(secrets.BackendReporter); ok {
			backend = br.Backend()
		}
		return secretsProbeResult{backend: backend, found: true}
	}
	if errors.Is(err, secrets.ErrKeyNotFound) {
		return secretsProbeResult{found: false}
	}
	return secretsProbeResult{err: err}
}

// RunSecretsStoreCheck performs a read-only health check of the secrets store's
// master-key acquisition path. Auth depends on this store: login persists the
// OAuth token by encrypting it with the master key held in the OS keyring (or
// its env/file fallbacks). When the store cannot initialise -- e.g. no reachable
// OS keyring and, on some hosts, permission-denied errors -- every login and
// token cache operation fails, so a green auth diagnose without this check is a
// false all-clear (issue #684).
//
// The check is non-mutating: it reads the master key if present but never
// generates or persists one. requireSecureKeyring mirrors
// settings.requireSecureKeyring: when true, an insecure env/file backend is a
// failure because secrets.New() would refuse to proceed.
func RunSecretsStoreCheck(requireSecureKeyring bool) Check {
	const checkName = "secrets store"

	res := secretsProbeFunc()

	if res.err != nil {
		return Check{
			Category: "secrets",
			Name:     checkName,
			Status:   StatusFail,
			Message: fmt.Sprintf(
				"cannot access the master-key keyring: %v -- login and token caching will fail. "+
					"Ensure the OS keychain (Keychain/Credential Manager/Secret Service) is reachable, "+
					"set SCAFCTL_SECRET_KEY to supply the master key explicitly (e.g. on headless/CI hosts), "+
					"or check the file-backend master key under the data dir (master.key, expected mode 0600) is readable",
				res.err,
			),
		}
	}

	insecure := res.backend == secrets.KeyringBackendEnv || res.backend == secrets.KeyringBackendFile

	// A key already exists on an insecure backend while secure keyring is
	// required: secrets.New() would refuse to proceed, breaking auth.
	if res.found && insecure && requireSecureKeyring {
		return Check{
			Category: "secrets",
			Name:     checkName,
			Status:   StatusFail,
			Message: fmt.Sprintf(
				"master key resolved via the insecure %q backend but settings.requireSecureKeyring is enabled "+
					"(SCAFCTL_REQUIRE_SECURE_KEYRING) -- the store will refuse to initialise. "+
					"Make the OS keychain reachable or disable requireSecureKeyring",
				res.backend,
			),
		}
	}

	if res.found {
		msg := fmt.Sprintf("master key present (backend: %s)", res.backend)
		status := StatusOK
		if insecure {
			status = StatusWarn
			msg += " -- insecure backend; master key is not protected by the OS keychain"
		}
		return Check{
			Category: "secrets",
			Name:     checkName,
			Status:   status,
			Message:  msg,
		}
	}

	// No master key yet: the store would generate one on first use. Healthy,
	// but if requireSecureKeyring is set and no OS keyring is reachable, that
	// generation would land on an insecure backend and be refused -- we cannot
	// know the write backend without mutating, so surface an informational note.
	if requireSecureKeyring {
		return Check{
			Category: "secrets",
			Name:     checkName,
			Status:   StatusInfo,
			Message: "no master key stored yet; it will be created on first use. " +
				"settings.requireSecureKeyring is enabled, so a reachable OS keychain is required at that time",
		}
	}
	return Check{
		Category: "secrets",
		Name:     checkName,
		Status:   StatusOK,
		Message:  "no master key stored yet; it will be created on first use",
	}
}
