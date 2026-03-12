// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package diagnose provides reusable auth diagnostic checks.
// The types and pure-check functions are consumed by the CLI
// command (pkg/cmd/scafctl/auth/diagnose.go) and MCP tools.
package diagnose

import (
	"fmt"
	"net/http"
	"os"
	"time"

	entraauth "github.com/oakwood-commons/scafctl/pkg/auth/entra"
	gcpauth "github.com/oakwood-commons/scafctl/pkg/auth/gcp"
	ghauth "github.com/oakwood-commons/scafctl/pkg/auth/github"
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
	if entraauth.HasServicePrincipalCredentials() {
		checks = append(checks, Check{
			Category: "env",
			Name:     "env entra: service-principal credentials",
			Status:   StatusOK,
			Message:  "AZURE_CLIENT_ID + AZURE_TENANT_ID + AZURE_CLIENT_SECRET are all set",
		})
	}
	if entraauth.HasWorkloadIdentityCredentials() {
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
	if ghauth.HasPATCredentials() {
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
	if gcpauth.HasServiceAccountCredentials() {
		checks = append(checks, Check{
			Category: "env",
			Name:     "env gcp: service-account credentials",
			Status:   StatusOK,
			Message:  "GOOGLE_APPLICATION_CREDENTIALS is set and points to a service account key",
		})
	}
	if gcpauth.HasWorkloadIdentityCredentials() {
		checks = append(checks, Check{
			Category: "env",
			Name:     "env gcp: workload-identity credentials",
			Status:   StatusOK,
			Message:  "GCP workload identity environment detected",
		})
	}
	if gcpauth.HasGcloudADCCredentials() {
		checks = append(checks, Check{
			Category: "env",
			Name:     "env gcp: gcloud ADC",
			Status:   StatusOK,
			Message:  "gcloud Application Default Credentials file found",
		})
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

// RunClockSkewCheck compares the local system clock against the Date header
// returned by a well-known HTTPS endpoint (cloudflare.com).
// A skew > 5 minutes can cause token validation failures.
func RunClockSkewCheck() Check {
	const endpoint = "https://cloudflare.com"
	const maxSkew = 5 * time.Minute
	const timeout = 4 * time.Second

	client := &http.Client{Timeout: timeout}
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
