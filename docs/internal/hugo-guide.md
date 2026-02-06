# Hugo Guide for scafctl

This guide explains how to set up and use Hugo for the scafctl documentation site.

## Prerequisites

- Go 1.21+ installed (Hugo extended is recommended)

## Installation

### Option 1: Using Homebrew (macOS)

```bash
brew install hugo
```

### Option 2: Using Go

```bash
go install github.com/gohugoio/hugo@latest
```

### Option 3: Download Binary

Download from [Hugo Releases](https://github.com/gohugoio/hugo/releases) and add to your PATH.

### Verify Installation

```bash
hugo version
```

## Initial Setup (One-time)

After cloning the repository, initialize Hugo modules and download the theme:

```bash
# Download the theme module
hugo mod get -u
```

That's it! Hugo modules are managed automatically (similar to Go modules).

## Common Commands

### Local Development Server

```bash
# Start the development server with live reload
hugo server

# Serve with drafts included
hugo server -D

# Serve on a specific port
hugo server -p 8080
```

The site will be available at `http://localhost:1313`. Changes to documentation files will automatically reload the browser.

### Build Static Site

```bash
# Build the documentation to the 'public/' directory
hugo

# Build with minification
hugo --minify

# Build for production
hugo --environment production --minify
```

### Create New Content

```bash
# Create a new tutorial
hugo new docs/tutorials/my-new-tutorial.md

# Create a new design doc
hugo new docs/design/my-design.md
```

## Project Structure

```
scafctl/
├── hugo.yaml           # Hugo configuration
├── docs/               # Documentation source files (content)
│   ├── _index.md       # Home page
│   ├── tutorials/      # User tutorials
│   ├── design/         # Architecture docs
│   └── internal/       # Developer docs
└── public/             # Generated site (git-ignored)
```

## Content Organization

Hugo uses front matter for metadata. Each markdown file should start with:

```yaml
---
title: "Page Title"
weight: 10
---
```

The `weight` parameter controls the order in the navigation (lower = higher).

### Section Index Files

Each folder should have an `_index.md` file:

```yaml
---
title: "Tutorials"
weight: 1
bookCollapseSection: true
---

Introduction text for this section.
```

## Configuration Overview

The `hugo.yaml` file configures:

| Section | Purpose |
|---------|---------|
| `baseURL` | Production URL for the site |
| `module.imports` | Theme via Hugo Modules |
| `params` | Theme-specific settings |
| `markup` | Syntax highlighting options |
| `menu` | Navigation menu items |

## Markdown Features

### Code Blocks with Syntax Highlighting

```go
// Go code with syntax highlighting
func main() {
    fmt.Println("Hello, scafctl!")
}
```

### Hints/Callouts (hugo-book theme)

```markdown
{{</* hint info */>}}
This is an info hint.
{{</* /hint */>}}

{{</* hint warning */>}}
This is a warning hint.
{{</* /hint */>}}

{{</* hint danger */>}}
This is a danger hint.
{{</* /hint */>}}
```

### Tabs (hugo-book theme)

```markdown
{{</* tabs "uniqueid" */>}}
{{</* tab "Go" */>}}
```go
fmt.Println("Hello")
```
{{</* /tab */>}}
{{</* tab "Python" */>}}
```python
print("Hello")
```
{{</* /tab */>}}
{{</* /tabs */>}}
```

### Mermaid Diagrams

```markdown
{{</* mermaid */>}}
graph LR
    A[Start] --> B[Process]
    B --> C[End]
{{</* /mermaid */>}}
```

### Expand/Collapse

```markdown
{{</* expand "Click to expand" */>}}
Hidden content here.
{{</* /expand */>}}
```

## GitHub Pages Deployment

### Option 1: GitHub Actions (Recommended)

Create `.github/workflows/hugo.yml`:

```yaml
name: Deploy Hugo site to Pages

on:
  push:
    branches: [main]
    paths:
      - 'docs/**'
      - 'hugo.yaml'
      - '.github/workflows/hugo.yml'

permissions:
  contents: read
  pages: write
  id-token: write

concurrency:
  group: "pages"
  cancel-in-progress: false

defaults:
  run:
    shell: bash

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
        with:
          submodules: recursive
          fetch-depth: 0

      - name: Setup Hugo
        uses: peaceiris/actions-hugo@v3
        with:
          hugo-version: 'latest'
          extended: true

      - name: Setup Pages
        id: pages
        uses: actions/configure-pages@v4

      - name: Build
        run: |
          hugo \
            --minify \
            --baseURL "${{ steps.pages.outputs.base_url }}/"

      - name: Upload artifact
        uses: actions/upload-pages-artifact@v3
        with:
          path: ./public

  deploy:
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    runs-on: ubuntu-latest
    needs: build
    steps:
      - name: Deploy to GitHub Pages
        id: deployment
        uses: actions/deploy-pages@v4
```

### Option 2: Manual Deploy

```bash
# Build the site
hugo --minify

# The 'public/' folder contains the static site
# Deploy it to any static hosting service
```

## GitHub Pages Setup

1. Go to repository Settings → Pages
2. Set Source to "GitHub Actions"
3. Push changes to trigger the workflow

## Troubleshooting

### "Error: module not found"
```bash
hugo mod get -u
```

### Theme not rendering
Ensure the theme module is downloaded:
```bash
hugo mod get -u
```

### "Page not found" on GitHub Pages
Check that `baseURL` in `hugo.yaml` matches your GitHub Pages URL.

## Resources

- [Hugo Documentation](https://gohugo.io/documentation/)
- [Hugo Book Theme](https://github.com/alex-shpak/hugo-book)
- [Hugo Quick Start](https://gohugo.io/getting-started/quick-start/)
