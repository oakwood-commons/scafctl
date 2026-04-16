version: "3"

vars:
  BINARY: <% .name %>
  DIST: "{{.ROOT_DIR | toSlash}}/dist"
  COVER: "{{.ROOT_DIR | toSlash}}/cover"

env:
  CGO_ENABLED: "0"

tasks:
  default:
    cmds:
      - task: build

  build:
    desc: Build the plugin binary
    sources:
      - "**/*.go"
      - go.mod
      - go.sum
    generates:
      - "{{.DIST}}/{{.BINARY}}"
    cmds:
      - mkdir -p {{.DIST}}
      - >-
        go build -ldflags "-s -w"
        -o {{.DIST}}/{{.BINARY}}
        ./cmd/{{.BINARY}}/

  test:
    desc: Run tests
    cmds:
      - go test -race -count=1 -shuffle=on -timeout 5m ./...

  test:cover:
    desc: Run tests with coverage
    cmds:
      - mkdir -p {{.COVER}}
      - go test -race -coverprofile={{.COVER}}/cover.out -covermode=atomic ./...

  lint:
    desc: Run linter
    cmds:
      - golangci-lint run ./...

  lint:fix:
    desc: Run linter with auto-fix
    cmds:
      - golangci-lint run --fix ./...

  bench:
    desc: Run benchmarks
    cmds:
      - go test -run='^$' -bench='.' -benchmem -count=1 ./...

  clean:
    desc: Remove build artifacts
    cmds:
      - rm -rf dist/ cover/ .task/

  ci:
    desc: Full CI pipeline (lint + test + build)
    cmds:
      - task: lint
      - task: test
      - task: build
