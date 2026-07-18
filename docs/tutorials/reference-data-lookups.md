---
title: "Reference-Data Lookups"
weight: 92
---

# Reference-Data Lookups

This tutorial shows how to resolve values from curated reference data --
taxonomies such as region tables, quota maps, classification codes, or option
lists -- by composing providers scafctl already ships. You supply the data with
`static`, `file`, or `http`, and query it with CEL.

There is deliberately **no dedicated `dataset` provider**. A lookup is just
"get the data" + "parse it" + "index it", and each of those is already a
first-class capability:

- **Get the data**: `static` (inline), `file` (`read`, local), `http` (remote),
  or `directory` (a folder of tables).
- **Parse it**: CEL `json.unmarshal` / `yaml.unmarshal`.
- **Index it**: CEL map indexing (`m[key]`), plus `map.select`, `map.omit`,
  and `map.merge`.

A single dedicated provider would only re-wrap those primitives, so this recipe
keeps the data-loading strategy explicit and swappable.

## Prerequisites

- scafctl installed and available in your PATH
- Basic familiarity with resolvers and CEL expressions

## Table of Contents

1. [Pattern 1: Inline Table](#pattern-1-inline-table)
2. [Pattern 2: Local Dataset File](#pattern-2-local-dataset-file)
3. [Pattern 3: Remote Dataset](#pattern-3-remote-dataset)
4. [Safe Lookups and Defaults](#safe-lookups-and-defaults)
5. [Choosing a Pattern](#choosing-a-pattern)
6. [Runnable Example](#runnable-example)

---

## Pattern 1: Inline Table

Embed the taxonomy directly in the solution with `static`. Best for small,
rarely-changing tables that should live with the solution.

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: reference-data-inline
  version: 1.0.0

spec:
  resolvers:
    # Overridable lookup key with a default: -r region=<name>
    region:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: region
          - provider: static
            inputs:
              value: us-east

    regionTable:
      type: any
      resolve:
        with:
          - provider: static
            inputs:
              value:
                us-east: { code: use1, quota: 40, tier: primary }
                us-west: { code: usw2, quota: 30, tier: primary }
                eu-west: { code: euw1, quota: 25, tier: secondary }

    # Keyed lookup, guarded with `in` so a missing key does not error.
    regionCode:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: >
                _.region in _.regionTable
                  ? _.regionTable[_.region].code
                  : "unknown"
~~~

The DAG infers `regionCode`'s dependency on `region` and `regionTable` from the
`_.` references -- no explicit `dependsOn` is needed.

## Pattern 2: Local Dataset File

Keep the taxonomy in its own JSON (or YAML) file and read it with the `file`
provider. Best when the table is large, shared across solutions, or maintained
separately from solution logic.

Given `regions.json`:

~~~json
{
  "us-east": { "code": "use1", "quota": 40, "tier": "primary" },
  "us-west": { "code": "usw2", "quota": 30, "tier": "primary" },
  "eu-west": { "code": "euw1", "quota": 25, "tier": "secondary" }
}
~~~

Read and parse it in one step, then query with CEL:

~~~yaml
spec:
  resolvers:
    region:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: region
          - provider: static
            inputs:
              value: us-east

    # `file` read with `parse: json` returns the parsed value in `.object`.
    regionsFile:
      type: any
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: regions.json
              parse: json

    # Bind the parsed object once, then look up the key with a safe default.
    quota:
      type: number
      resolve:
        with:
          - provider: cel
            inputs:
              expression: >
                cel.bind(t, _.regionsFile.object,
                  _.region in t ? t[_.region].quota : 0
                )
~~~

For a YAML dataset, use `parse: yaml` instead. The `file` read returns
`{ content, path, size }`, plus `object` (the parsed value) when `parse` is set;
the raw text is always available at `.content`.

## Pattern 3: Remote Dataset

Fetch the table from an HTTP endpoint with the `http` provider, then parse the
response body. Best when the taxonomy is owned by another service or updated
independently of your solution.

~~~yaml
spec:
  resolvers:
    region:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: region
          - provider: static
            inputs:
              value: us-east

    # `http` returns { success, statusCode, body, headers } -- do not set `type: string`.
    regionsResponse:
      resolve:
        with:
          - provider: http
            inputs:
              url: https://config.example.com/regions.json
              method: GET

    quota:
      type: number
      resolve:
        with:
          - provider: cel
            inputs:
              expression: >
                cel.bind(t, json.unmarshal(_.regionsResponse.body),
                  _.region in t ? t[_.region].quota : 0
                )
~~~

Authentication, headers, and retries are configured on the `http` provider
itself; the lookup half is identical to the local-file pattern. See the
[Provider Reference](provider-reference.md) for `http` inputs.

## Safe Lookups and Defaults

Indexing a key that does not exist raises a CEL error. Always guard a lookup
with `in` and supply a fallback:

~~~cel
_.region in _.regionTable
  ? _.regionTable[_.region]
  : {"code": "unknown", "quota": 0, "tier": "unknown"}
~~~

When the source is an already-parsed file object or an HTTP body, bind the
value once with `cel.bind` so you do not unmarshal or re-reference it twice:

~~~cel
cel.bind(t, _.regionsFile.object,
  _.region in t ? t[_.region].quota : 0
)
~~~

To reshape a looked-up record, use the map helpers: `map.select` to keep an
allow-list of keys, `map.omit` to drop sensitive fields, and `map.merge` to
overlay computed values. See the [CEL Expressions Tutorial](cel-tutorial.md).

## Choosing a Pattern

| Source | Provider | Use when |
|--------|----------|----------|
| Inline | `static` | Small, stable table that belongs with the solution |
| Local file | `file` (`read`) | Larger or shared table maintained as a data file |
| Remote | `http` | Table is owned/updated by another service |
| Folder of tables | `directory` | Many dataset files loaded together |

In every case the lookup half is the same: `json.unmarshal` (or
`yaml.unmarshal`) followed by a guarded index.

## Runnable Example

A complete, runnable version covering the inline and local-file patterns lives
at `examples/solutions/reference-data-lookup/`:

{{< tabs "reference-data-lookups-run" >}}
{{% tab "Bash" %}}
~~~bash
# Default key (us-east)
scafctl run resolver -f examples/solutions/reference-data-lookup/solution.yaml -o json

# Override the key
scafctl run resolver -f examples/solutions/reference-data-lookup/solution.yaml -r region=eu-west -o json
~~~
{{% /tab %}}
{{% tab "PowerShell" %}}
~~~powershell
# Default key (us-east)
scafctl run resolver -f examples/solutions/reference-data-lookup/solution.yaml -o json

# Override the key
scafctl run resolver -f examples/solutions/reference-data-lookup/solution.yaml -r region=eu-west -o json
~~~
{{% /tab %}}
{{< /tabs >}}

Selected output for the default key:

~~~json
{
  "quota": 40,
  "region": "us-east",
  "regionCode": "use1"
}
~~~

A missing key (`-r region=antarctica`) falls back to the safe defaults --
`regionCode` becomes `unknown` and `quota` becomes `0`.

## Related

- [Resolver Tutorial](resolver-tutorial.md) -- dynamic value resolution
- [File Provider Tutorial](file-provider-tutorial.md) -- reading files
- [CEL Expressions Tutorial](cel-tutorial.md) -- `json.unmarshal`, `cel.bind`,
  and the `map.*` helpers
