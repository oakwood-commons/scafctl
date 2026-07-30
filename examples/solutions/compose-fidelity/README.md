# Compose Fidelity Demo

Demonstrates golden-file fidelity checking of a **composed** solution using
`scafctl render solution --effective`.

This solution is deliberately split across `compose:` partials:

- `solution.yaml` -- metadata plus the `compose:` list
- `resolvers.yaml` -- `spec.resolvers`
- `workflow.yaml` -- `spec.workflow`

At load time scafctl merges the partials into one effective document. The
`--effective` renderer prints that merged document **deterministically** and
**without executing any resolvers or providers** -- the scafctl analogue of
`docker compose config`, `helm template`, or `kustomize build`.

## Render the effective document

~~~bash
# Whole composed document (YAML, default)
scafctl render solution -f examples/solutions/compose-fidelity/solution.yaml --effective

# JSON
scafctl render solution -f examples/solutions/compose-fidelity/solution.yaml --effective -o json

# Only the effective workflow
scafctl render solution -f examples/solutions/compose-fidelity/solution.yaml \
  --effective --section workflow

# Only the effective resolvers
scafctl render solution -f examples/solutions/compose-fidelity/solution.yaml \
  --effective --section resolvers
~~~

## Golden-file workflow (CI fidelity check)

`golden.effective.yaml` in this directory is a committed golden artifact captured
with:

~~~bash
scafctl render solution -f examples/solutions/compose-fidelity/solution.yaml \
  --effective -o yaml > examples/solutions/compose-fidelity/golden.effective.yaml
~~~

Because the output is byte-stable for a given input, CI can regenerate it and
fail on any difference -- catching unintended changes introduced while editing a
`compose:` partial:

~~~bash
scafctl render solution -f examples/solutions/compose-fidelity/solution.yaml \
  --effective -o yaml \
  | diff -u examples/solutions/compose-fidelity/golden.effective.yaml -
~~~

An empty diff means the effective solution is unchanged. A non-empty diff means
composition changed and should be reviewed (and the golden file updated
intentionally).

## Notes

- `--effective` never runs resolvers or providers; expressions are kept verbatim,
  so the view is a source-fidelity snapshot, not an evaluated one.
- `--section` scopes the output to `all` (default), `workflow`, or `resolvers`.
- `-o test` is not valid with `--effective`; use `yaml` or `json`.
