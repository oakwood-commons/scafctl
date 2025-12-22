# CLI Documentation (Proposal)

This folder will host the **scafctl CLI reference** once the tool supports exporting help text directly to Markdown. The planned structure mirrors the CLI help surface so the generated content can drop straight into this tree without additional tooling.

## Goals

1. **Single source of truth** – Every `scafctl --help` output is mirrored here so users can browse CLI documentation without running the command.
2. **Automation friendly** – The CLI will emit Markdown directly (for example `scafctl help --format markdown`), letting us regenerate docs as part of release builds.
3. **Version aware** – Each release can ship the generated CLI docs, ensuring historical accuracy.

## Proposed Structure

```
docs/
  cli/
    README.md                 # This file
    index.md                  # Root CLI help (scafctl --help)
    commands/
      run.md                  # scafctl run --help
      get.md                  # scafctl get --help
      build.md                # scafctl build --help
      publish.md              # scafctl publish --help
      validate.md             # scafctl validate --help
      test.md                 # scafctl test --help (planned)
      config.md               # scafctl config --help (planned)
      auth.md                 # scafctl auth --help (planned)
      catalog.md              # scafctl catalog --help (planned)
      migrate.md              # scafctl migrate --help (planned)
      version.md              # scafctl version --help
    flags/
      global-flags.md         # Global CLI flags (pulled from index)
      run-flags.md            # Command-specific flags (optional split)
```

- `index.md` will contain the top-level `scafctl --help` output.
- Each command gets its own Markdown file under `commands/`.
- Optional subfolders (like `flags/`) can capture reusable flag descriptions if the generator supports splitting them out.

## Regeneration Workflow (Future)

Once the CLI supports Markdown export, the documentation can be regenerated with a script similar to:

```bash
rm -rf docs/cli/commands
mkdir -p docs/cli/commands

scafctl help --format markdown > docs/cli/index.md
scafctl run --help --format markdown > docs/cli/commands/run.md
scafctl get --help --format markdown > docs/cli/commands/get.md
# Repeat for all commands
```

We can wire this into a release task (`make docs-cli` or a `task` entry) so every release publishes fresh CLI help.

## Current Status

- **Proposal only** – The CLI does not yet emit Markdown help. This folder documents the intended layout so we can align code changes with documentation needs.
- **Manual updates allowed** – While automation is pending, we can add handcrafted Markdown files here if needed, clearly labeling them as temporary.

## Next Steps

1. Extend `scafctl` to emit Markdown for `--help` output.
2. Add a build step that generates the files into `docs/cli/`.
3. Update release documentation to reference `docs/cli/index.md` for CLI usage.
