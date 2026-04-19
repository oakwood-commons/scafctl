// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"fmt"
	"strings"
)

// aadstsHint returns a human-readable hint for well-known AADSTS error codes
// embedded in an Azure error description string.  The returned string is empty
// when no specific guidance is available for the code found in desc.
//
// The hints are intentionally actionable: they name the environment variable or
// portal action the user should check rather than just restating the error.
func aadstsHint(desc string) string {
	switch {
	// AADSTS700016: Application with identifier '…' was not found in the
	// directory '…'.  Almost always a wrong client ID or wrong tenant.
	case strings.Contains(desc, "AADSTS700016"):
		return fmt.Sprintf(
			"the application was not found in the directory; verify that %s contains "+
				"the correct app registration client ID and that %s matches the tenant "+
				"where that app is registered",
			EnvAzureClientID, EnvAzureTenantID,
		)

	// AADSTS90002: Tenant not found.  The tenant GUID or domain is invalid
	// or the tenant has no active subscriptions.
	case strings.Contains(desc, "AADSTS90002"):
		return fmt.Sprintf(
			"the tenant was not found; verify that %s contains a valid tenant ID or "+
				"verified domain name and that the tenant has an active Azure subscription",
			EnvAzureTenantID,
		)

	// AADSTS7000215: Invalid client secret — the secret value is wrong, has
	// been rotated, or has expired.  The AADSTS code appears literally in the
	// description for this sub-error.
	case strings.Contains(desc, "AADSTS7000215"):
		return fmt.Sprintf(
			"the client secret is invalid or has expired; regenerate the secret in the "+
				"Azure portal (app registrations → Certificates & secrets) and update %s",
			EnvAzureClientSecret,
		)

	// AADSTS70011: The provided request must include a 'scope' input parameter.
	// Also returned when the scope value is invalid.
	case strings.Contains(desc, "AADSTS70011"):
		return "the requested scope is invalid or not registered on the application; " +
			"verify the scope value and ensure it is exposed on the API app registration"

	// AADSTS50194: Application is not configured as a multi-tenant application.
	// Returned when a personal/consumer account tries to authenticate against a
	// single-tenant app, or when the wrong endpoint is used.
	case strings.Contains(desc, "AADSTS50194"):
		return fmt.Sprintf(
			"the application is not configured for the account type being used; "+
				"check the 'Supported account types' setting in the app registration and "+
				"verify %s is correct",
			EnvAzureTenantID,
		)

	// AADSTS500011: The resource principal named '…' was not found in the tenant.
	// Typically means the target API application has not been registered / consented.
	case strings.Contains(desc, "AADSTS500011"):
		return "the target API resource was not found in this tenant; ensure the API " +
			"application is registered in the same tenant and that admin consent has been granted"

	// AADSTS500113: No reply address is registered for the application.
	// The app registration does not have a redirect URI matching the one sent
	// in the authorization request.  For the interactive (browser OAuth) flow,
	// register http://localhost under 'Mobile and desktop applications' platform
	// in the Azure portal (App registrations → Authentication → Add a platform).
	case strings.Contains(desc, "AADSTS500113"):
		return "no redirect URI is registered for this application; in the Azure portal, " +
			"go to App registrations → your app → Authentication → Add a platform → " +
			"'Mobile and desktop applications', then add http://localhost as a redirect URI. " +
			"Alternatively, use '--flow device-code' which does not require a redirect URI"

	// AADSTS53003: Access has been blocked by Conditional Access policies.
	// A Conditional Access policy requires additional claims (MFA, compliant
	// device, etc.).  The token endpoint returns a claims challenge that must
	// be passed back in a new interactive authentication request.
	case strings.Contains(desc, "AADSTS53003"):
		return "a Conditional Access policy blocked this request; " +
			"re-authenticate interactively so the required claims (e.g. MFA, device compliance) can be satisfied"
	}

	return ""
}

// formatAADSTSError builds a complete error message for a token endpoint error
// response, appending an actionable hint when one is available.
func formatAADSTSError(prefix string, errResp TokenErrorResponse) error {
	hint := aadstsHint(errResp.ErrorDescription)
	if hint != "" {
		return fmt.Errorf("%s: %s: %s\nHint: %s", prefix, errResp.Error, errResp.ErrorDescription, hint)
	}
	return fmt.Errorf("%s: %s: %s", prefix, errResp.Error, errResp.ErrorDescription)
}
