// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
	"strings"
)

// ParseFlow converts a flow string to a Flow constant. If flowStr is empty,
// an empty Flow is returned (caller should auto-detect). handlerName is used
// to produce handler-specific error messages for unrecognised values.
func ParseFlow(flowStr, handlerName string) (Flow, error) {
	if flowStr == "" {
		return "", nil // Will be auto-detected per handler
	}
	switch strings.ToLower(flowStr) {
	case "device_code", "device-code", "devicecode":
		return FlowDeviceCode, nil
	case "interactive":
		return FlowInteractive, nil
	case "service_principal", "service-principal", "serviceprincipal", "sp":
		return FlowServicePrincipal, nil
	case "workload_identity", "workload-identity", "workloadidentity", "wi":
		return FlowWorkloadIdentity, nil
	case "pat":
		return FlowPAT, nil
	case "metadata":
		return FlowMetadata, nil
	case "gcloud_adc", "gcloud-adc", "gcloudadc", "adc":
		return FlowGcloudADC, nil
	case "github_app", "github-app", "githubapp", "app":
		return FlowGitHubApp, nil
	case "client_credentials", "client-credentials", "clientcredentials", "cc":
		return FlowClientCredentials, nil
	default:
		switch handlerName {
		case "github":
			return "", fmt.Errorf("unknown flow: %s (valid for github: interactive, device-code, pat, github-app)", flowStr)
		case "gcp":
			return "", fmt.Errorf("unknown flow: %s (valid for gcp: interactive, service-principal, workload-identity, metadata, gcloud-adc)", flowStr)
		case "entra":
			return "", fmt.Errorf("unknown flow: %s (valid for entra: interactive, device-code, service-principal, workload-identity)", flowStr)
		default:
			return "", fmt.Errorf("unknown flow: %s", flowStr)
		}
	}
}
