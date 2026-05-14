#!/usr/bin/env bash
# Canonical implementation of the cosign tree referrer parser.
# Extracts non-signature, deduplicated referrer digests from cosign tree output.
#
# Usage:
#   parse_referrers "$TREE_OUTPUT" "$PRIMARY_DIGEST"
#
# The same awk+grep pipeline is inlined in sign-plugin.yml (which cannot source
# external scripts because it runs as a reusable workflow without a checkout step).
# Tests exercise this function AND verify the inline copy has not drifted.

# parse_referrers extracts deduplicated non-signature referrer digests from
# cosign tree output, excluding the primary image digest.
#   $1 - cosign tree output
#   $2 - primary image digest to exclude (e.g. sha256:abc...)
parse_referrers() {
  local tree_output="$1" primary_digest="$2"
  local referrers
  referrers=$(echo "$tree_output" \
    | awk '/^[└├]──.*Signatures/{skip=1; next} /^[└├]──/{skip=0} !skip && /sha256:[a-f0-9]{64}/{print}' \
    | grep -oE 'sha256:[a-f0-9]{64}' || true)
  referrers=$(echo "$referrers" | sort -u | grep -v "^${primary_digest}$" || true)
  echo "$referrers"
}
