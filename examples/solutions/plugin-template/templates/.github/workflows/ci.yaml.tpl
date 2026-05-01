name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
    paths-ignore:
      - "**.md"
      - "docs/**"
      - ".gitignore"
      - "LICENSE"

concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

permissions:
  contents: read

jobs:
  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - uses: golangci/golangci-lint-action@v7
        with:
          version: v2.11.4
      - name: Check go mod tidy
        run: |
          go mod tidy
          git diff --exit-code go.mod go.sum

  test:
    name: Test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - run: go build ./...
      - run: go test -race -coverprofile=coverage.out -covermode=atomic ./...
      - name: Upload coverage
        if: github.event_name == 'push'
        uses: codecov/codecov-action@v4
        with:
          files: coverage.out
          fail_ci_if_error: false
