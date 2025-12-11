# GitHub Actions Workflows

This directory contains GitHub Actions workflows for automated CI/CD pipelines.

## Workflows

### 1. **`pr-checks.yml`** - Pull Request Checks

- **Triggers**: On pull requests to `main` or `develop` branches
- **Jobs**:
  - ✅ Runs `task lint` to check code quality
  - ✅ Runs `task test` to run the test suite
- **Features**:
  - Skips runs when only documentation files change
  - Uses Go version from `go.mod`
  - Caches Go modules for faster builds
  - Full git history for proper versioning

### 2. **`test.yml`** - Tests

- **Triggers**: On pull requests and pushes to `main`
- **Jobs**:
  - Runs tests on multiple OS: Ubuntu, macOS, and Windows
  - Runs `task test` on all platforms
  - Runs `task test-cover` on Ubuntu and uploads coverage
- **Features**:
  - Matrix strategy for cross-platform testing
  - Optional Codecov integration (requires `CODECOV_TOKEN` secret)

### 3. **`release.yml`** - Release

- **Triggers**: On version tags (e.g., `v1.2.3`)
- **Jobs**:
  - Runs `task release` to create releases
  - Optional GPG signing support
- **Features**:
  - Uses goreleaser for multi-platform builds
  - Supports GPG signing if secrets are configured

## Next Steps

### 1. Commit the workflows

```bash
git add .github/workflows/
git commit -s -m "ci: add GitHub Actions workflows for PR checks, tests, and releases"
git push
```

### 2. Optional: Add secrets

Configure the following secrets in GitHub repo settings → Secrets and variables → Actions:

- **`CODECOV_TOKEN`** - For test coverage reporting (optional)
  - Get from [codecov.io](https://codecov.io) after setting up your repository

- **`GPG_PRIVATE_KEY`** - For signing releases (optional)
  - Export your GPG private key: `gpg --armor --export-secret-keys YOUR_KEY_ID`

- **`GPG_PASSPHRASE`** - GPG key passphrase (optional)
  - The passphrase for your GPG key

- **`GPG_FINGERPRINT`** - GPG key fingerprint (optional)
  - Get with: `gpg --list-secret-keys --keyid-format LONG`

### 3. Test the workflows

1. Create a new branch:

   ```bash
   git checkout -b test-workflows
   ```

2. Make a small change and push:

   ```bash
   echo "# Test" >> README.md
   git add README.md
   git commit -s -m "test: trigger workflows"
   git push -u origin test-workflows
   ```

3. Create a pull request on GitHub and watch the workflows run!

4. Check the Actions tab in your GitHub repository to see the workflow results

## Workflow Permissions

The workflows use the following permissions:

- **`pr-checks.yml`**: Default (read repository contents)
- **`test.yml`**: Default (read repository contents)
- **`release.yml`**:
  - `contents: write` - To create releases and upload assets
  - `packages: write` - To publish container images (if needed)

## Troubleshooting

### Workflow not triggering

- Ensure the branch names match your repository's default branch
- Check that the file paths are not in the `paths-ignore` list
- Verify that GitHub Actions are enabled in repository settings

### Build failures

- Check the workflow logs in the Actions tab
- Ensure all required secrets are configured
- Verify that `go.mod` and `taskfile.yaml` are present in the repository

### Linting failures

- Run `task lint` locally to see the same errors
- Fix issues with `task lint:fix`
- Commit and push the fixes

## Customization

### Modify trigger branches

Edit the `on.pull_request.branches` section in each workflow:

```yaml
on:
  pull_request:
    branches:
      - main
      - develop
      - feature/*  # Add custom branch patterns
```

### Add more build targets

Edit the matrix strategy in `test.yml`:

```yaml
strategy:
  matrix:
    os: [ubuntu-latest, macos-latest, windows-latest]
    go-version: ['1.21', '1.22', '1.23']  # Test multiple Go versions
```

### Skip workflows for specific files

Add patterns to `paths-ignore`:

```yaml
on:
  pull_request:
    paths-ignore:
      - '**.md'
      - 'docs/**'
      - 'examples/**'
```
