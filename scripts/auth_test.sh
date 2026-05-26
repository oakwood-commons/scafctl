#!/usr/bin/env bash
# Auth Profile Integration Test Commands
# Copy/paste each command individually. Expected results in comments.

# --- 1. Default profile status ---
dist/scafctl auth status github
# => authenticated, abaker9@gmail.com, Profile=built-in

# --- 2. Work profile status ---
dist/scafctl auth status github --profile work
# => not authenticated, Profile=work

# --- 3. Login work profile (opens device code flow) ---
dist/scafctl auth login github --profile work
# => NEW device code flow, NOT "Already authenticated"

# --- 4. Both profiles authenticated ---
dist/scafctl auth status github
dist/scafctl auth status github --profile work
# => default=gmail, work=ford

# --- 5. Tokens are different ---
dist/scafctl auth token github --raw
dist/scafctl auth token github --profile work --raw
# => two different token values

# --- 6. Global flag ---
dist/scafctl --auth-profile work auth status github
# => Profile=work, ford account

# --- 7. Per-command overrides global ---
dist/scafctl --auth-profile work auth status github --profile default
# => Profile=built-in, gmail account

# --- 8. Env var ---
SCAFCTL_AUTH_PROFILE=work dist/scafctl auth status github
# => Profile=work, ford account

# --- 9. Validation ---
dist/scafctl auth status github --profile "has spaces"
# => error: invalid profile name

dist/scafctl auth status github --profile "-starts-with-dash"
# => error: invalid profile name

# --- 10. Status --all ---
dist/scafctl auth status --all
# => auth/catalog/registry rows with Kind column

# --- 11. Logout work only ---
dist/scafctl auth logout github --profile work
dist/scafctl auth status github
# => default still authenticated (gmail)

dist/scafctl auth status github --profile work
# => not authenticated

# --- 12. Credential helper format ---
dist/scafctl auth token github
# => formatted token output

# --- 13. Auth switch ---
dist/scafctl auth switch github
# => lists available profiles
