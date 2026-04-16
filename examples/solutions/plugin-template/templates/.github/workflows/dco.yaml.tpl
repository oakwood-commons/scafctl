name: DCO

on:
  pull_request:
    branches: [main]

jobs:
  dco-check:
    name: DCO Check
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - name: Verify Signed-off-by lines
        run: |
          set -euo pipefail
          base_sha="${{ github.event.pull_request.base.sha }}"
          head_sha="${{ github.event.pull_request.head.sha }}"
          missing=0
          for sha in $(git rev-list --no-merges "${base_sha}..${head_sha}"); do
            if ! git show -s --format=%B "$sha" | grep -qi "^Signed-off-by:"; then
              echo "Missing Signed-off-by in commit $sha"
              missing=1
            fi
          done
          exit $missing
