# scafctl migrate (Proposal)

Assist users in upgrading legacy scafctl artifacts to the current schema. Migration utilities help bridge older solutions, catalogs, and configuration into the open-source model.

> **Status:** Proposal. Migration tooling is not yet implemented; this document outlines the planned CLI surface and capabilities.

## Usage

```
scafctl migrate <command> [flags]
```

## Available Subcommands

- `solution <path>` — Convert an outdated solution file or folder to the current schema
- `catalog <path>` — Update catalog manifests or metadata to the new format (planned)
- `config <path>` — Transform legacy CLI config to profiles and auth references (planned)
- `auth <path>` — Migrate stored credentials/sessions (planned)

## Shared Flags

```
      --output <path>     Write converted files to destination (defaults to in-place with backup)
      --check             Report required changes without writing
      --overwrite         Overwrite existing files without prompting
      --backup            Create `.bak` files before modifying in place (default true)
      --profile <name>    Use configuration profile when resolving defaults
      --quiet             Suppress informational output
      --debug             Emit debug logs
```

## Solution Migration Workflow (Planned)

1. **Analyze** legacy solution: detect deprecated fields (`transform` structure, action templates, expression locations).
2. **Plan** conversion steps: detail required changes (e.g., `transform` → `transform.into`, action templates removed).
3. **Apply** conversions when `--check` not set:
   - Rewrite resolver `transform` arrays into `transform.into` blocks.
   - Normalize `when` / `until` semantics.
   - Move template references out of actions into resolvers.
   - Update expression language fields (Go template ↔ CEL) per new conventions.
4. **Validate** converted solution against the new schema.
5. **Report** summary with before/after highlights and next steps.

## Integrated Upgrade Flow

Migration shares code with `scafctl validate` so the CLI can guide users through upgrades:

1. **User runs `scafctl build` or `scafctl validate`.**
2. Validator detects legacy schema features and surfaces a structured error with guidance:
      - `error: legacy schema detected`
      - `hint: run scafctl migrate solution ./solution.yaml`
3. User runs `scafctl migrate solution ./solution.yaml` (optionally with `--check` first).
4. Migration applies conversions, writes backups, and automatically re-validates.
5. User re-runs `scafctl build` / `scafctl validate`; success once schema is current.

This keeps `validate`, `migrate`, and `build` on a common pipeline: you always call migrate in response to a validator hint, and migrate confirms the result by invoking the validator again.

## Examples (Planned Behavior)

### Dry-run migration report

```
scafctl migrate solution ./legacy/solution.yaml --check
```

### Convert solution in place (with backup)

```
scafctl migrate solution ./legacy/solution.yaml
```

Creates `solution.yaml.bak` before writing the new schema.

### Convert solution directory to new location

```
scafctl migrate solution ./legacy-solution --output ./converted-solution
```

### Migrate legacy config

```
scafctl migrate config ~/.config/scafctl/config.legacy.yaml --output ~/.config/scafctl/config.yaml
```

## Reporting Output

Migration commands produce structured output:

- Summary of conversions performed
- List of manual follow-ups (fields that could not be auto-converted)
- Validation status (pass/fail)
- Suggested next steps (e.g., run `scafctl validate`, review tests)

Outputs can be exported as JSON/YAML for tooling integration (`--output-format json`, planned).

## Roadmap Notes

- **Plug-in aware** migration: detect custom providers/actions and ensure compatibility.
- **Interactive prompts**: offer guided migration with user confirmation.
- **Batch mode**: migrate entire directories of solutions.
- **Testing integration**: optionally run `scafctl test` after migration.
- **Generated Docs**: Once the CLI exporter exists, `scafctl migrate --help --format markdown` will populate this file.

### Related Documentation

- [Solution Schema](../../schemas/solution-schema.md)
- [Resolver Schema](../../schemas/resolver-schema.md)
- [Transform Phase Guide](../guides/03-transform-phase.md)
- [Authentication Reference](../../reference/auth.md)
