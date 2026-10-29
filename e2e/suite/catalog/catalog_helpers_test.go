// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package catalogtest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

const (
	// fixtureName is the solution fixture every catalog spec packages.
	fixtureName = "catalog-basic"

	// fixtureDescriptionLine is the line writeSolutionVariant rewrites so two
	// copies of the same name@version get different digests. The indent
	// anchors it to metadata.description rather than the fixture's header
	// comment, which mentions the same text.
	fixtureDescriptionIndent = "  "
	fixtureDescriptionLine   = fixtureDescriptionIndent + "description: baseline"

	// demoName is the metadata.name of the catalog-basic fixture.
	demoName = "demo"

	// catalogRepository is the repository path every spec publishes under.
	catalogRepository = "scafctl"

	// builtOrigin is how a locally built copy is labelled in an ambiguity listing.
	builtOrigin = "built"
)

// copyLine matches one entry of the ambiguity listing printed when a short
// reference matches several local copies, for example:
//
//   - 1.0.0 (remote:127.0.0.1:5000/scafctl) sha256:abc...
var copyLine = regexp.MustCompile(`-\s+\S+\s+\(([^)]+)\)\s+(sha256:[0-9a-f]+)`)

// packageResult is the subset of `scafctl package solution -o json` the specs
// use.
type packageResult struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

// listItem is the subset of `scafctl catalog list -o json` the specs use.
type listItem struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Tag     string `json:"tag"`
	Kind    string `json:"kind"`
	Digest  string `json:"digest"`
	Catalog string `json:"catalog"`
}

// tagItem is the subset of `scafctl catalog tags -o json` the specs use.
type tagItem struct {
	Tag     string `json:"tag"`
	Version string `json:"version"`
}

// catalogURL is the catalog (registry plus repository) a spec publishes to.
func catalogURL(registryAddr string) string {
	return registryAddr + "/" + catalogRepository
}

// solutionRef is the full remote reference of a solution.
func solutionRef(registryAddr, name, version string) string {
	return fmt.Sprintf("%s/solutions/%s@%s", catalogURL(registryAddr), name, version)
}

// writeSolutionVariant writes a copy of the catalog-basic fixture to a temp
// dir with the given description and returns the solution file path. Two
// variants with different descriptions package to different digests.
func writeSolutionVariant(description string) string {
	GinkgoHelper()

	src, err := os.ReadFile(filepath.Join(utils.SolutionFixture(fixtureName), "solution.yaml"))
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(string(src)).To(gomega.ContainSubstring(fixtureDescriptionLine))

	out := strings.Replace(string(src), fixtureDescriptionLine, fixtureDescriptionIndent+"description: "+description, 1)
	path := filepath.Join(GinkgoT().TempDir(), "solution.yaml")
	gomega.Expect(os.WriteFile(path, []byte(out), 0o600)).To(gomega.Succeed())
	return path
}

// packageSolution packages file into the isolated local catalog as a built
// artifact and returns the result.
func packageSolution(iso utils.Isolation, file, version string) packageResult {
	GinkgoHelper()

	session := utils.Scafctl(
		"package", "solution",
		"-f", file,
		"--version", version,
		"-o", "json",
	).WithEnv(iso.Env).Exec()

	var result packageResult
	gomega.Expect(json.Unmarshal(session.Out.Contents(), &result)).To(gomega.Succeed())
	gomega.Expect(result.Digest).To(gomega.HavePrefix("sha256:"))
	return result
}

// publishVariant packages a description variant of the fixture in a throwaway
// isolated environment and pushes it to registryAddr, so the registry holds
// that content while the calling spec's own local catalog stays untouched. It
// returns the published digest.
func publishVariant(registryAddr, version, description string) string {
	GinkgoHelper()

	iso := utils.NewIsolatedEnv()
	built := packageSolution(iso, writeSolutionVariant(description), version)

	utils.Scafctl(
		"catalog", "push", solutionRef(registryAddr, demoName, version),
		"--insecure",
	).WithEnv(iso.Env).Exec()

	return built.Digest
}

// remoteTags returns the tags of a remote artifact. kindPlural is the path
// segment of the artifact kind ("solutions", "providers").
func remoteTags(iso utils.Isolation, registryAddr, kindPlural, name string) []string {
	GinkgoHelper()

	session := utils.Scafctl(
		"catalog", "tags",
		fmt.Sprintf("%s/%s/%s", catalogURL(registryAddr), kindPlural, name),
		"--insecure",
		"-o", "json",
	).WithEnv(iso.Env).Exec()

	var items []tagItem
	gomega.Expect(json.Unmarshal(session.Out.Contents(), &items)).To(gomega.Succeed())

	tags := make([]string, 0, len(items))
	for _, item := range items {
		// Skip the "sha256-<digest>" referrer-fallback tags an attached SBOM
		// leaves behind; they are not versions.
		if strings.HasPrefix(item.Tag, "sha256-") {
			continue
		}
		tags = append(tags, item.Tag)
	}
	return tags
}

// remoteList lists every artifact version in the remote catalog.
func remoteList(iso utils.Isolation, registryAddr string) []listItem {
	GinkgoHelper()

	session := utils.Scafctl(
		"catalog", "list",
		"--catalog", catalogURL(registryAddr),
		"--insecure",
		"--all-versions",
		"-o", "json",
	).WithEnv(iso.Env).Exec()

	var items []listItem
	gomega.Expect(json.Unmarshal(session.Out.Contents(), &items)).To(gomega.Succeed())
	return items
}

// pluginKind describes a plugin artifact kind and the fixture that builds it.
type pluginKind struct {
	// Kind is the --kind / kind-subcommand value ("provider", "auth-handler").
	Kind string
	// Plural is the repository path segment of the kind.
	Plural string
	// Name is the artifact name the fixture is packaged as.
	Name string
	// FixtureDir is the plugin source, relative to the repo root.
	FixtureDir string
}

var (
	providerPlugin = pluginKind{
		Kind:       "provider",
		Plural:     "providers",
		Name:       "test-provider",
		FixtureDir: pluginFixtureDir,
	}
	authHandlerPlugin = pluginKind{
		Kind:       "auth-handler",
		Plural:     "auth-handlers",
		Name:       authHandlerName,
		FixtureDir: authHandlerFixtureDir,
	}
)

// packageDigest matches the digest line `package plugin` prints on success.
var packageDigest = regexp.MustCompile(`Digest:\s+(sha256:[0-9a-f]+)`)

// publishPlugin builds the fixture for the host platform, packages it in a
// throwaway isolated environment and pushes it to registryAddr, so the
// registry holds the artifact while the calling spec's own local catalog stays
// untouched. It returns the published digest.
func publishPlugin(k pluginKind, registryAddr, version string) string {
	GinkgoHelper()

	iso := utils.NewIsolatedEnv()
	bin := filepath.Join(GinkgoT().TempDir(), k.Name)
	platform := runtime.GOOS + "/" + runtime.GOARCH
	utils.BuildPluginBinaryForPlatform(bin, k.FixtureDir, platform)

	packaged := utils.Scafctl(
		"package", "plugin",
		"--name", k.Name,
		"--kind", k.Kind,
		"--version", version,
		"--platform", platform+"="+bin,
	).WithEnv(iso.Env).Exec()

	match := packageDigest.FindStringSubmatch(output(packaged))
	gomega.Expect(match).To(gomega.HaveLen(2), "package plugin output carries a digest")

	utils.Scafctl(
		"catalog", "push", k.Kind, k.Name+"@"+version,
		"--catalog", catalogURL(registryAddr),
		"--insecure",
	).WithEnv(iso.Env).Exec()

	return match[1]
}

// localPluginDigest returns the digest of the local copy of a plugin
// name@version. `catalog inspect` only handles solutions, so this reads the
// local catalog list; it assumes a single local copy because the list does not
// report an origin.
func localPluginDigest(iso utils.Isolation, k pluginKind, version string) string {
	GinkgoHelper()

	session := utils.Scafctl(
		"catalog", "list",
		"--kind", k.Kind,
		"--name", k.Name,
		"--all-versions",
		"-o", "json",
	).WithEnv(iso.Env).Exec()

	var items []listItem
	gomega.Expect(json.Unmarshal(session.Out.Contents(), &items)).To(gomega.Succeed())

	var digests []string
	for _, item := range items {
		if item.Version == version {
			digests = append(digests, item.Digest)
		}
	}
	gomega.Expect(digests).To(gomega.HaveLen(1), "exactly one local copy of %s@%s", k.Name, version)
	return digests[0]
}

// remoteOrigin is how a copy pulled from registryAddr is labelled in an
// ambiguity listing.
func remoteOrigin(registryAddr string) string {
	return "remote:" + catalogURL(registryAddr)
}

// localCopies returns the origin -> digest of every local copy of a solution
// name@version. `catalog list` does not report an origin, so this reads
// the ambiguity listing `catalog inspect` prints when a short reference
// matches several copies; it therefore requires at least two copies.
func localCopies(iso utils.Isolation, name, version string) map[string]string { //nolint:unparam
	GinkgoHelper()

	session := utils.Scafctl(
		"catalog", "inspect", name+"@"+version,
	).WithEnv(iso.Env).ExpectFailure().Exec()

	copies := map[string]string{}
	for _, m := range copyLine.FindAllStringSubmatch(output(session), -1) {
		copies[m[1]] = m[2]
	}
	return copies
}

// localDigest returns the digest of the single local copy of a solution
// name@version via `catalog inspect`. It fails when the reference is missing
// or ambiguous; use localCopies when several copies exist. `catalog list` is
// not used for local state because it does not report an origin.
func localDigest(iso utils.Isolation, name, version string) string {
	GinkgoHelper()

	session := utils.Scafctl(
		"catalog", "inspect", name+"@"+version,
		"-o", "json",
	).WithEnv(iso.Env).Exec()

	var info struct {
		Digest string `json:"digest"`
	}
	gomega.Expect(json.Unmarshal(session.Out.Contents(), &info)).To(gomega.Succeed())
	gomega.Expect(info.Digest).To(gomega.HavePrefix("sha256:"))
	return info.Digest
}

// remoteDigest returns the manifest digest of a solution name@version in the
// remote catalog, via `catalog inspect` on the full remote reference (remote
// listings do not carry digests).
func remoteDigest(iso utils.Isolation, registryAddr, name, version string) string { //nolint:unparam
	GinkgoHelper()

	session := utils.Scafctl(
		"catalog", "inspect", solutionRef(registryAddr, name, version),
		"--insecure",
		"-o", "json",
	).WithEnv(iso.Env).Exec()

	var info struct {
		Digest string `json:"digest"`
	}
	gomega.Expect(json.Unmarshal(session.Out.Contents(), &info)).To(gomega.Succeed())
	gomega.Expect(info.Digest).To(gomega.HavePrefix("sha256:"))
	return info.Digest
}

// remoteNames returns the distinct artifact names in the remote catalog.
func remoteNames(iso utils.Isolation, registryAddr string) []string {
	GinkgoHelper()

	seen := map[string]struct{}{}
	var names []string
	for _, item := range remoteList(iso, registryAddr) {
		if _, ok := seen[item.Name]; !ok {
			seen[item.Name] = struct{}{}
			names = append(names, item.Name)
		}
	}
	return names
}

// output returns stdout and stderr of a finished session as one string, for
// asserting on messages without caring which stream the CLI wrote them to.
func output(session *gexec.Session) string {
	return string(session.Out.Contents()) + string(session.Err.Contents())
}
