# CLI Commands Index (Proposal)

Generated CLI help will populate this folder with one Markdown file per command. Until that automation lands, the files here are hand-authored representations of the intended output.

## Available Commands

- [run](./run.md)
- [get](./get.md)
- [build](./build.md)
- [publish](./publish.md)
- [validate](./validate.md)
- [test](./test.md)
- [config](./config.md)
- [auth](./auth.md)
- [catalog](./catalog.md)
- [migrate](./migrate.md)
- [version](./version.md)

Each document mirrors what `scafctl <command> --help --format markdown` should eventually emit:

- **Usage** section shows the invocation pattern.
- **Flags** section lists command-specific options (global flags omitted for brevity).
- **Behavior** outlines what the command does.
- **Examples** demonstrate common use cases.

Once the CLI exporter is implemented, this index can be auto-generated or replaced with links produced by the tooling.
