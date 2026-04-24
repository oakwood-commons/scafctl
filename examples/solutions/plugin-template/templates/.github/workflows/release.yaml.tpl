name: Release

on:
  push:
    tags: ["v*"]

permissions:
  contents: write
  packages: write
  id-token: write

jobs:
  test:
    name: Verify
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - run: go test -race ./...

  release:
    name: Release
    needs: test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: "~> v2"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}

  publish-oci:
    name: Publish OCI Artifact
    needs: release
    runs-on: ubuntu-latest
    permissions:
      packages: write
      contents: read
    steps:
      - name: Install scafctl
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          TMP_DIR="$(mktemp -d)"
          gh release download --repo oakwood-commons/scafctl \
            --pattern 'scafctl_*_Linux_x86_64.tar.gz' \
            --dir "${TMP_DIR}"
          tar xzf "${TMP_DIR}"/scafctl_*_Linux_x86_64.tar.gz -C "${TMP_DIR}"
          sudo mv "${TMP_DIR}/scafctl" /usr/local/bin/scafctl
          rm -rf "${TMP_DIR}"

      - name: Login to GitHub Container Registry
        run: echo "${{ secrets.GITHUB_TOKEN }}" | docker login ghcr.io -u ${{ github.actor }} --password-stdin

      - name: Download release archives
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          TAG="${GITHUB_REF#refs/tags/}"
          mkdir -p dist
          gh release download "${TAG}" --repo "${{ github.repository }}" --dir dist

      - name: Extract binaries from archives
        run: |
          BINARY="<% .name %>"
          cd dist

          for platform in linux_amd64 linux_arm64 darwin_amd64 darwin_arm64; do
            archive="${BINARY}_${platform}.tar.gz"
            if [ -f "${archive}" ]; then
              mkdir -p "tmp_${platform}"
              tar xzf "${archive}" -C "tmp_${platform}"
              mv "tmp_${platform}/${BINARY}" "${BINARY}-${platform//_/-}"
              rm -rf "tmp_${platform}"
            fi
          done

          archive="${BINARY}_windows_amd64.zip"
          if [ -f "${archive}" ]; then
            mkdir -p tmp_windows_amd64
            unzip -o "${archive}" -d tmp_windows_amd64
            mv "tmp_windows_amd64/${BINARY}.exe" "${BINARY}-windows-amd64.exe"
            rm -rf tmp_windows_amd64
          fi

      - name: Build and push OCI artifact
        run: |
          VERSION="${GITHUB_REF#refs/tags/v}"
          BINARY="<% .name %>"

          scafctl build plugin \
            --name "<% .provider_name %>" \
            --kind "<% .plugin_type %>" \
            --version "${VERSION}" \
            --platform "linux/amd64=dist/${BINARY}-linux-amd64" \
            --platform "linux/arm64=dist/${BINARY}-linux-arm64" \
            --platform "darwin/amd64=dist/${BINARY}-darwin-amd64" \
            --platform "darwin/arm64=dist/${BINARY}-darwin-arm64" \
            --platform "windows/amd64=dist/${BINARY}-windows-amd64.exe"

          scafctl catalog push \
            "<% .provider_name %>@${VERSION}" \
            --catalog "ghcr.io/<% .registry_owner %>" \
            --kind "<% .plugin_type %>" \
            --force
