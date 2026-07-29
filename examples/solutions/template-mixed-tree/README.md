# Mixed Template Tree Example (rendered + verbatim)

Demonstrates rendering a directory tree where **most files are Go templates**
but **some must be copied verbatim** -- all in a single
`directory -> render-tree -> write-tree` pipeline, without splitting into a
second copy pipeline or wrapping whole files in ignore markers.

## What It Does

1. **Reads** every file under `templates/` recursively (both `.tpl` and `.raw`)
   using the `directory` provider.
2. **Renders** the templates with shared variables using `go-template`
   `render-tree`, while **copying `**/*.raw` files byte-for-byte** via the
   `rawGlobs` input.
3. **Writes** all files under `./output/`, stripping the trailing extension
   using `file` `write-tree` with `outputPath`.

## Directory Layout

```
template-mixed-tree/
|-- solution.yaml
|-- README.md
`-- templates/
    |-- config/
    |   `-- app.yaml.tpl        # RENDERED with vars
    `-- ci/
        `-- workflow.yaml.raw   # COPIED VERBATIM (GitHub Actions ${{ }})
```

## Running

```bash
scafctl run solution -f examples/solutions/template-mixed-tree/solution.yaml
```

## Output

```
output/
|-- config/
|   `-- app.yaml               # rendered: {{ .appName }} substituted
`-- ci/
    `-- workflow.yaml          # verbatim: ${{ ... }} preserved exactly
```

## Key Concept: `rawGlobs`

`rawGlobs` is a list of [doublestar](https://github.com/bmatcuk/doublestar)
glob patterns matched against each entry's **full relative path**. A matching
entry is emitted verbatim: no template parsing, no delimiter handling, no
`ignoredBlocks` processing, and any per-entry `data` is ignored.

```yaml
rawGlobs:
  - "**/*.raw"        # matches nested files; use *.raw for top-level only
```

This mirrors Cookiecutter's `_copy_without_render`.

### Per-entry override

When entries are constructed in CEL, a per-entry `raw` boolean takes precedence
over `rawGlobs`:

- `raw: true` forces verbatim even without a glob match.
- `raw: false` forces rendering even when a glob matches.

```yaml
entries:
  expr: |
    [
      {"path": "keep.txt",  "content": "{{ literal }}", "raw": true},
      {"path": "render.raw", "content": "hi {{ .who }}", "raw": false}
    ]
```
