#!/usr/bin/env bash
# Fixture-based tests for the cosign tree referrer parsing logic
# used by sign-plugin.yml. Exercises representative cosign tree output
# (including signatures, duplicates, and edge cases).
#
# Sources the canonical parser from scripts/parse-referrers.sh and includes
# a drift-detection test to ensure the inline copy in sign-plugin.yml stays
# in sync.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source the canonical parser function.
# shellcheck source=../scripts/parse-referrers.sh
source "${SCRIPT_DIR}/../scripts/parse-referrers.sh"

PASS=0
FAIL=0

assert_eq() {
  local test_name="$1" expected="$2" actual="$3"
  if [ "$expected" = "$actual" ]; then
    echo "PASS: $test_name"
    PASS=$((PASS + 1))
  else
    echo "FAIL: $test_name"
    echo "  expected: $(echo "$expected" | tr '\n' ' ')"
    echo "  actual:   $(echo "$actual" | tr '\n' ' ')"
    FAIL=$((FAIL + 1))
  fi
}

# ---- Test 1: typical cosign tree output with all section types ----
TREE1=$(cat <<'EOF'
📦 Supply Chain Security Related artifacts for an image: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
└── 💼 Attestations for an image tag: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
   └── sha256:bbbb111111111111111111111111111111111111111111111111111111111111
└── 📦 SBOMs for an image tag: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
   └── sha256:cccc222222222222222222222222222222222222222222222222222222222222
└── 🔐 Signatures for an image tag: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
   └── sha256:dddd333333333333333333333333333333333333333333333333333333333333
EOF
)
RESULT=$(parse_referrers "$TREE1" "sha256:aaaa000000000000000000000000000000000000000000000000000000000000")
EXPECTED=$(printf 'sha256:bbbb111111111111111111111111111111111111111111111111111111111111\nsha256:cccc222222222222222222222222222222222222222222222222222222222222')
assert_eq "filters out signature digests" "$EXPECTED" "$RESULT"

# ---- Test 2: duplicate referrer digests are deduplicated ----
TREE2=$(cat <<'EOF'
📦 Supply Chain Security Related artifacts for an image: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
└── 💼 Attestations for an image tag: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
   └── sha256:bbbb111111111111111111111111111111111111111111111111111111111111
   └── sha256:bbbb111111111111111111111111111111111111111111111111111111111111
└── 📦 SBOMs for an image tag: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
   └── sha256:cccc222222222222222222222222222222222222222222222222222222222222
   └── sha256:cccc222222222222222222222222222222222222222222222222222222222222
EOF
)
RESULT=$(parse_referrers "$TREE2" "sha256:aaaa000000000000000000000000000000000000000000000000000000000000")
EXPECTED=$(printf 'sha256:bbbb111111111111111111111111111111111111111111111111111111111111\nsha256:cccc222222222222222222222222222222222222222222222222222222222222')
assert_eq "deduplicates repeated digests" "$EXPECTED" "$RESULT"

# ---- Test 3: primary digest appearing as referrer is excluded ----
TREE3=$(cat <<'EOF'
📦 Supply Chain Security Related artifacts for an image: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
└── 💼 Attestations for an image tag: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
   └── sha256:aaaa000000000000000000000000000000000000000000000000000000000000
   └── sha256:bbbb111111111111111111111111111111111111111111111111111111111111
EOF
)
RESULT=$(parse_referrers "$TREE3" "sha256:aaaa000000000000000000000000000000000000000000000000000000000000")
EXPECTED="sha256:bbbb111111111111111111111111111111111111111111111111111111111111"
assert_eq "excludes primary digest from referrers" "$EXPECTED" "$RESULT"

# ---- Test 4: no referrers at all (only signatures) ----
TREE4=$(cat <<'EOF'
📦 Supply Chain Security Related artifacts for an image: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
└── 🔐 Signatures for an image tag: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
   └── sha256:dddd333333333333333333333333333333333333333333333333333333333333
EOF
)
RESULT=$(parse_referrers "$TREE4" "sha256:aaaa000000000000000000000000000000000000000000000000000000000000")
assert_eq "no referrers when only signatures exist" "" "$RESULT"

# ---- Test 5: empty tree output ----
RESULT=$(parse_referrers "" "sha256:aaaa000000000000000000000000000000000000000000000000000000000000")
assert_eq "empty tree output returns nothing" "" "$RESULT"

# ---- Test 6: signatures section between two non-signature sections ----
TREE6=$(cat <<'EOF'
📦 Supply Chain Security Related artifacts for an image: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
└── 💼 Attestations for an image tag: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
   └── sha256:bbbb111111111111111111111111111111111111111111111111111111111111
└── 🔐 Signatures for an image tag: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
   └── sha256:dddd333333333333333333333333333333333333333333333333333333333333
   └── sha256:eeee444444444444444444444444444444444444444444444444444444444444
└── 📦 SBOMs for an image tag: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
   └── sha256:cccc222222222222222222222222222222222222222222222222222222222222
EOF
)
RESULT=$(parse_referrers "$TREE6" "sha256:aaaa000000000000000000000000000000000000000000000000000000000000")
EXPECTED=$(printf 'sha256:bbbb111111111111111111111111111111111111111111111111111111111111\nsha256:cccc222222222222222222222222222222222222222222222222222222222222')
assert_eq "signatures between two sections are skipped" "$EXPECTED" "$RESULT"

# ---- Test 7: ├── branch markers for non-final sections ----
TREE7=$(cat <<'EOF'
📦 Supply Chain Security Related artifacts for an image: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
├── 💼 Attestations for an image tag: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
   └── sha256:bbbb111111111111111111111111111111111111111111111111111111111111
├── 🔐 Signatures for an image tag: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
   └── sha256:dddd333333333333333333333333333333333333333333333333333333333333
   └── sha256:eeee444444444444444444444444444444444444444444444444444444444444
└── 📦 SBOMs for an image tag: ghcr.io/org/repo@sha256:aaaa000000000000000000000000000000000000000000000000000000000000
   └── sha256:cccc222222222222222222222222222222222222222222222222222222222222
EOF
)
RESULT=$(parse_referrers "$TREE7" "sha256:aaaa000000000000000000000000000000000000000000000000000000000000")
EXPECTED=$(printf 'sha256:bbbb111111111111111111111111111111111111111111111111111111111111\nsha256:cccc222222222222222222222222222222222222222222222222222222222222')
assert_eq "├── branch markers: signatures skipped on non-final sections" "$EXPECTED" "$RESULT"

# ---- Test 8: drift detection — full inline pipeline in sign-plugin.yml matches canonical script ----
WORKFLOW="${SCRIPT_DIR}/../sign-plugin.yml"
CANONICAL="${SCRIPT_DIR}/../scripts/parse-referrers.sh"
# Compare the three pipeline components between the workflow and canonical script:
#   1) awk program   2) grep digest pattern   3) sort -u | grep -v dedup pattern
# We strip variable names (which differ: TREE_OUTPUT vs tree_output, etc.) and
# compare only the pipeline operations themselves.

# 1) awk program
WORKFLOW_AWK=$(grep -oE "awk '[^']+'" "$WORKFLOW" | head -1)
CANONICAL_AWK=$(grep -oE "awk '[^']+'" "$CANONICAL" | head -1)
assert_eq "drift: awk pattern matches" "$CANONICAL_AWK" "$WORKFLOW_AWK"

# 2) grep digest extraction (the grep on the line after awk, not the PRIMARY_DIGEST grep)
WORKFLOW_GREP=$(grep -A1 'awk' "$WORKFLOW" | grep -oE "grep -oE 'sha256:\[a-f0-9\]\{64\}'" | head -1)
CANONICAL_GREP=$(grep -A1 'awk' "$CANONICAL" | grep -oE "grep -oE 'sha256:\[a-f0-9\]\{64\}'" | head -1)
assert_eq "drift: grep digest pattern matches" "$CANONICAL_GREP" "$WORKFLOW_GREP"

# 3) dedup pipeline (sort -u | grep -v exclusion)
# Normalize variable references (${VAR}, $VAR) to a placeholder before comparing.
WORKFLOW_DEDUP=$(grep 'sort -u' "$WORKFLOW" | head -1 | sed 's/.*\(sort -u.*\)/\1/' | sed 's/\${[^}]*}/DIGEST/g; s/\$[A-Za-z_][A-Za-z_0-9]*/DIGEST/g; s/ *|| true.*/|| true/')
CANONICAL_DEDUP=$(grep 'sort -u' "$CANONICAL" | head -1 | sed 's/.*\(sort -u.*\)/\1/' | sed 's/\${[^}]*}/DIGEST/g; s/\$[A-Za-z_][A-Za-z_0-9]*/DIGEST/g; s/ *|| true.*/|| true/')
assert_eq "drift: sort/dedup pipeline matches" "$CANONICAL_DEDUP" "$WORKFLOW_DEDUP"

# ---- Summary ----
echo ""
echo "Results: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
