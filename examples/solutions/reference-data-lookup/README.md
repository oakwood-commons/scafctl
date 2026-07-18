# Reference-Data Lookup Example

Resolve values from curated reference data (a region/quota taxonomy) by
composing existing providers. There is no dedicated "dataset" provider -- and
you do not need one. `static`/`file`/`http` supply the data, CEL queries it.

## What It Does

1. **Reads a lookup key** (`region`) from `-r region=<name>`, defaulting to
   `us-east` via the `parameter` -> `static` fallback chain.
2. **Pattern 1 -- inline table**: embeds the taxonomy with `static`
   (`regionTable`) and looks up the record with a safe default (`regionRecord`),
   then derives a scalar (`regionCode`).
3. **Pattern 2 -- local dataset file**: reads and parses `regions.json` with the
   `file` provider (`regionsFile`, using `parse: json`), then looks up the
   `quota` for the key (`quota`) directly from `_.regionsFile.object`.

A remote dataset uses the same shape with `provider: http` in place of `file`;
that snippet lives in the tutorial so this example stays hermetic.

## Running

```bash
# Default key (us-east)
scafctl run resolver -f examples/solutions/reference-data-lookup/solution.yaml -o json

# Override the key
scafctl run resolver -f examples/solutions/reference-data-lookup/solution.yaml -r region=eu-west -o json

# Missing key falls back to the safe default
scafctl run resolver -f examples/solutions/reference-data-lookup/solution.yaml -r region=antarctica -o json
```

## Output

| `-r region=` | `regionCode` | `quota` |
|--------------|--------------|---------|
| (default) `us-east` | `use1` | `40` |
| `eu-west` | `euw1` | `25` |
| `antarctica` (absent) | `unknown` | `0` |

## Key Concepts

| Step | Provider | Purpose |
|------|----------|---------|
| `region` | `parameter` -> `static` | Overridable lookup key with a default |
| `regionTable` | `static` | Inline taxonomy (name -> attributes) |
| `regionRecord` | `cel` | Keyed lookup with `in` guard + fallback record |
| `regionCode` | `cel` | Derive a scalar from the record |
| `regionsFile` | `file` (`read`, `parse: json`) | Load and parse an on-disk dataset |
| `quota` | `cel` | `cel.bind` + guarded lookup on the parsed object |

## Safe Lookups

Indexing a missing key errors in CEL, so guard every lookup:

```cel
_.region in _.regionTable
  ? _.regionTable[_.region]
  : {"code": "unknown", "quota": 0, "tier": "unknown"}
```

For a parsed file, bind the parsed object once for a clean guarded lookup:

```cel
cel.bind(t, _.regionsFile.object,
  _.region in t ? t[_.region].quota : 0
)
```
