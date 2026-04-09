// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package get

import (
	"context"
	"fmt"
	"io"
	"os"
	pathlib "path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/filepath"
	"github.com/oakwood-commons/scafctl/pkg/fs"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/metrics"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// CatalogResolver is an interface for fetching solutions from a catalog.
// This avoids a circular dependency with the catalog package.
type CatalogResolver interface {
	// FetchSolution retrieves a solution from the catalog by name[@version].
	// Returns the solution content bytes and any error.
	FetchSolution(ctx context.Context, nameWithVersion string) ([]byte, error)
}

// BundleAwareCatalogResolver extends CatalogResolver with bundle fetching.
type BundleAwareCatalogResolver interface {
	CatalogResolver
	// FetchSolutionWithBundle retrieves a solution and its bundle from the catalog.
	// Returns the solution content bytes, bundle tar bytes (nil if no bundle), and any error.
	FetchSolutionWithBundle(ctx context.Context, nameWithVersion string) (content, bundleData []byte, err error)
}

// RemoteResolver resolves Docker-style OCI remote references (e.g.,
// "ghcr.io/myorg/starter-kit@1.0.0"). Implementations are responsible for
// registry authentication and fetching.
type RemoteResolver interface {
	// FetchRemoteSolution fetches a solution from a remote OCI reference.
	// The ref is the full remote reference string (e.g., "ghcr.io/myorg/starter-kit@1.0.0").
	FetchRemoteSolution(ctx context.Context, ref string) (content, bundleData []byte, err error)
}

type Getter struct {
	readFile          fs.ReadFileFunc
	statFunc          fs.StatFunc
	httpClient        *httpc.Client
	logger            logr.Logger
	catalogResolver   CatalogResolver
	remoteResolver    RemoteResolver
	solutionFolders   []string
	solutionFileNames []string
}

// Option defines a function type that modifies a Getter instance.
// It can be used to configure or customize the behavior of Getter by applying various options.
type Option func(*Getter)

// WithReadFile returns an Option that sets the readFile function used by the Getter.
// This allows customization of how files are read, enabling dependency injection for testing or alternative file systems.
//
// readFile: a function conforming to fs.ReadFileFunc, used to read files.
// Returns: an Option that sets the Getter's readFile field.
func WithReadFile(readFile fs.ReadFileFunc) Option {
	return func(g *Getter) {
		g.readFile = readFile
	}
}

// WithStatFunc returns an Option that sets the statFunc field of a Getter.
// The provided statFunc is used to retrieve file information during operations.
// This allows customization of how file statistics are obtained.
func WithStatFunc(statFunc fs.StatFunc) Option {
	return func(g *Getter) {
		g.statFunc = statFunc
	}
}

// WithHTTPClient returns an Option that sets the HTTP client for the Getter.
// This allows customization of the HTTP client used for network requests.
func WithHTTPClient(client *httpc.Client) Option {
	return func(g *Getter) {
		g.httpClient = client
	}
}

// WithLogger returns an Option that sets the logger for the Getter.
// It allows customizing the logging behavior by providing a logr.Logger instance.
func WithLogger(logger logr.Logger) Option {
	return func(g *Getter) {
		g.logger = logger
	}
}

// WithCatalogResolver returns an Option that sets the catalog resolver for the Getter.
// When a catalog resolver is set, the Getter will attempt to resolve bare names
// (names without path separators or URL schemes) from the catalog first.
func WithCatalogResolver(resolver CatalogResolver) Option {
	return func(g *Getter) {
		g.catalogResolver = resolver
	}
}

// WithRemoteResolver returns an Option that sets the remote resolver for the Getter.
// When a remote resolver is set, the Getter will attempt to resolve Docker-style
// OCI references (e.g., "ghcr.io/myorg/starter-kit@1.0.0") from remote registries.
func WithRemoteResolver(resolver RemoteResolver) Option {
	return func(g *Getter) {
		g.remoteResolver = resolver
	}
}

// WithSolutionDiscovery overrides the default solution folder and file name
// lists used by FindSolution. Pass the result of settings.SolutionFoldersFor
// and settings.SolutionFileNamesFor to search for <binaryName>.yaml etc.
func WithSolutionDiscovery(folders, fileNames []string) Option {
	return func(g *Getter) {
		g.solutionFolders = folders
		g.solutionFileNames = fileNames
	}
}

// WithAppConfig returns an Option that configures the HTTP client using the application configuration.
// It creates an HTTP client with settings from the provided config.HTTPClientConfig.
// The logger is used for HTTP client logging.
func WithAppConfig(cfg *config.HTTPClientConfig, logger logr.Logger) Option {
	return func(g *Getter) {
		g.httpClient = httpc.NewClientFromAppConfig(cfg, logger)
		g.logger = logger
	}
}

// NewGetter creates a new Getter instance with the provided options.
// By default, it sets up the Getter with the standard file reading and stat functions,
// a default HTTP client, and a discard logger. Options can be supplied to customize
// the behavior of the Getter.
func NewGetter(opts ...Option) *Getter {
	g := &Getter{
		readFile:          os.ReadFile,
		statFunc:          os.Stat,
		httpClient:        httpc.NewClient(nil), // Use default HTTP client
		logger:            logr.Discard(),       // Use discard logger by default
		solutionFolders:   settings.RootSolutionFolders,
		solutionFileNames: settings.SolutionFileNames,
	}

	// Apply all options
	for _, opt := range opts {
		opt(g)
	}

	return g
}

// NewGetterFromContext creates a Getter using the binary name from settings.Run
// in the context to configure solution discovery paths. If the context does not
// contain settings or the binary name matches the default, no override is applied.
// Additional options are applied after the context-derived configuration.
func NewGetterFromContext(ctx context.Context, opts ...Option) *Getter {
	var ctxOpts []Option
	if ctx != nil {
		if s, ok := settings.FromContext(ctx); ok && s.BinaryName != "" && s.BinaryName != settings.CliBinaryName {
			ctxOpts = append(ctxOpts, WithSolutionDiscovery(
				settings.SolutionFoldersFor(s.BinaryName),
				settings.SolutionFileNamesFor(s.BinaryName),
			))
		}
	}
	return NewGetter(append(ctxOpts, opts...)...)
}

// Interface defines methods for retrieving a Solution from different sources.
// Implementations should provide logic to load a Solution either from the local file system,
// from a remote URL, or automatically discover from default locations.
//
// Methods:
//   - FromLocalFileSystem: Loads a Solution from a specified local file path.
//   - FromUrl: Loads a Solution from a specified remote URL.
//   - Get: Loads a Solution from a path (local or URL) with auto-discovery support.
//   - FindSolution: Searches for a solution file in default locations.
type Interface interface {
	FromLocalFileSystem(ctx context.Context, path string) (*solution.Solution, error)
	FromURL(ctx context.Context, url string) (*solution.Solution, error)
	Get(ctx context.Context, path string) (*solution.Solution, error)
	// GetWithBundle retrieves a Solution and its bundle tar data (if any).
	// bundleData is nil when the solution has no bundle or comes from a local file.
	GetWithBundle(ctx context.Context, path string) (sol *solution.Solution, bundleData []byte, err error)
	FindSolution() string
}

// Get retrieves a Solution from the specified path, which can be a local file or a URL.
// If the path is empty, it attempts to find a solution file in default locations.
// The method records the time taken to retrieve the solution for metrics purposes.
// Returns an error if no solution path is provided or found.
//
// Parameters:
//
//	ctx  - The context for cancellation and deadlines.
//	path - The path to the solution file or URL.
//
// Returns:
//
//	*solution.Solution - The retrieved solution object.
//	error              - An error if retrieval fails.
func (o *Getter) Get(ctx context.Context, path string) (*solution.Solution, error) {
	start := time.Now()
	if path == "" {
		path = o.FindSolution()
	}

	ctx, span := telemetry.Tracer(telemetry.TracerSolution).Start(ctx, "solution.Get",
		trace.WithAttributes(attribute.String("solution.path", path)),
	)
	defer span.End()

	defer func() {
		if metrics.GetSolutionTimeHistogram != nil {
			metrics.GetSolutionTimeHistogram.Record(ctx, time.Since(start).Seconds(),
				metric.WithAttributes(attribute.String(metrics.AttrPath, path)))
		}
	}()

	if path == "" {
		return nil, fmt.Errorf("no solution path provided and no solution file found in default locations")
	}

	// Check if this is a bare name that should be resolved from catalog.
	// A bare name has no path separators and is not a URL.
	var catalogErr error
	if o.catalogResolver != nil && o.isBareName(path) {
		o.logger.V(1).Info("attempting to resolve from catalog", "name", path)
		sol, err := o.fromCatalog(ctx, path)
		if err == nil {
			o.logger.V(1).Info("resolved solution from catalog", "name", path)
			return sol, nil
		}

		// If the path contains @, user explicitly requested a version from catalog.
		// Don't fall back to file resolution - return the catalog error directly.
		if strings.Contains(path, "@") {
			return nil, err
		}

		// Save catalog error for combined error message if file resolution also fails
		catalogErr = err
		o.logger.V(1).Info("solution not found in catalog, falling back to file resolution", "name", path, "error", err)
	}

	if filepath.IsURL(path) {
		return o.FromURL(ctx, path)
	}

	// Check if this is a Docker-style OCI remote reference (e.g., ghcr.io/myorg/starter-kit@1.0.0).
	// Remote references have a registry hostname (contains "." or ":") in the first path segment.
	if o.remoteResolver != nil && IsCatalogReference(path) && strings.Contains(path, "/") {
		// Docker-like behavior: check local catalog first, fall back to remote.
		if sol, ok := o.tryLocalCatalogForRef(ctx, path); ok {
			return sol, nil
		}

		o.logger.V(1).Info("attempting to resolve from remote registry", "ref", path)
		sol, err := o.fromRemoteRef(ctx, path)
		if err == nil {
			return sol, nil
		}
		return nil, fmt.Errorf("failed to resolve remote reference %q: %w", path, err)
	}

	sol, fileErr := o.FromLocalFileSystem(ctx, path)
	if fileErr == nil {
		return sol, nil
	}

	// If we tried catalog and it failed, provide a combined error message
	if catalogErr != nil {
		return nil, fmt.Errorf("%w; also not found on file system", catalogErr)
	}

	return nil, fileErr
}

// GetWithBundle retrieves a Solution and its bundle tar data from the specified path.
// bundleData is nil when the solution has no bundle or comes from a local file/URL.
func (o *Getter) GetWithBundle(ctx context.Context, path string) (*solution.Solution, []byte, error) {
	start := time.Now()
	if path == "" {
		path = o.FindSolution()
	}

	ctx, span := telemetry.Tracer(telemetry.TracerSolution).Start(ctx, "solution.GetWithBundle",
		trace.WithAttributes(attribute.String("solution.path", path)),
	)
	defer span.End()

	defer func() {
		if metrics.GetSolutionTimeHistogram != nil {
			metrics.GetSolutionTimeHistogram.Record(ctx, time.Since(start).Seconds(),
				metric.WithAttributes(attribute.String(metrics.AttrPath, path)))
		}
	}()

	if path == "" {
		return nil, nil, fmt.Errorf("no solution path provided and no solution file found in default locations")
	}

	// Check if this is a bare name that should be resolved from catalog
	if o.catalogResolver != nil && o.isBareName(path) {
		o.logger.V(1).Info("attempting to resolve with bundle from catalog", "name", path)
		sol, bundleData, err := o.fromCatalogWithBundle(ctx, path)
		if err == nil {
			o.logger.V(1).Info("resolved solution with bundle from catalog", "name", path, "hasBundle", len(bundleData) > 0)
			return sol, bundleData, nil
		}

		// If the path contains @, user explicitly requested a version from catalog
		if strings.Contains(path, "@") {
			return nil, nil, err
		}

		o.logger.V(1).Info("solution not found in catalog, falling back to file resolution", "name", path, "error", err)
	}

	// For local files and URLs, bundle data is nil (files are already on disk)
	if filepath.IsURL(path) {
		sol, err := o.FromURL(ctx, path)
		return sol, nil, err
	}

	// Check if this is a Docker-style OCI remote reference
	if o.remoteResolver != nil && IsCatalogReference(path) && strings.Contains(path, "/") {
		// Docker-like behavior: check local catalog first, fall back to remote.
		if sol, bundleData, ok := o.tryLocalCatalogForRefWithBundle(ctx, path); ok {
			return sol, bundleData, nil
		}

		o.logger.V(1).Info("attempting to resolve with bundle from remote registry", "ref", path)
		sol, bundleData, err := o.fromRemoteRefWithBundle(ctx, path)
		if err == nil {
			return sol, bundleData, nil
		}
		return nil, nil, fmt.Errorf("failed to resolve remote reference %q: %w", path, err)
	}

	sol, err := o.FromLocalFileSystem(ctx, path)
	return sol, nil, err
}

// fromCatalogWithBundle retrieves a solution and its bundle from the catalog.
func (o *Getter) fromCatalogWithBundle(ctx context.Context, nameWithVersion string) (*solution.Solution, []byte, error) {
	// Try bundle-aware resolver first
	if bundleResolver, ok := o.catalogResolver.(BundleAwareCatalogResolver); ok {
		content, bundleData, err := bundleResolver.FetchSolutionWithBundle(ctx, nameWithVersion)
		if err != nil {
			return nil, nil, err
		}

		sol := solution.Solution{}
		if err := sol.LoadFromBytes(content); err != nil {
			return nil, nil, fmt.Errorf("failed to parse solution from catalog: %w", err)
		}

		sol.SetPath(fmt.Sprintf("catalog:%s", nameWithVersion))
		return &sol, bundleData, nil
	}

	// Fall back to basic resolver (no bundle)
	content, err := o.catalogResolver.FetchSolution(ctx, nameWithVersion)
	if err != nil {
		return nil, nil, err
	}

	sol := solution.Solution{}
	if err := sol.LoadFromBytes(content); err != nil {
		return nil, nil, fmt.Errorf("failed to parse solution from catalog: %w", err)
	}

	sol.SetPath(fmt.Sprintf("catalog:%s", nameWithVersion))
	return &sol, nil, nil
}

// ValidatePositionalRef validates a positional CLI argument intended to be a
// catalog or registry reference. Returns an error if:
//   - fileFlag is non-empty (both -f/--file and a positional arg were provided)
//   - arg looks like a local file path rather than a catalog/registry name
//
// cmdUsage is included in the error message to suggest the correct invocation
// (e.g., "scafctl explain solution").
func ValidatePositionalRef(arg, fileFlag, cmdUsage string) error {
	if fileFlag != "" {
		return fmt.Errorf("cannot use both -f/--file and a positional argument")
	}
	if !IsCatalogReference(arg) {
		return fmt.Errorf("local file paths must use -f/--file flag: %s -f %s", cmdUsage, arg)
	}
	return nil
}

// IsCatalogReference returns true when s looks like a catalog name or remote
// registry reference rather than a local file path. The check is intentionally
// conservative: when in doubt it returns false so callers are guided to use
// -f/--file instead of silently treating a filesystem path as a catalog lookup.
//
// Returns false (local file path) when:
//   - s starts with "/" (absolute path)
//   - s starts with "." (relative path like ./foo or ../bar)
//   - s ends with ".yaml", ".yml", or ".json" (file extension)
//   - s starts with a Windows drive letter (e.g., "C:\dir\sol" or "C:/dir/sol")
//   - s contains a backslash (Windows path separator)
//   - s contains "/" but the first path segment does not look like a hostname
//     (i.e., does not contain "." or ":") — catches relative paths like
//     "configs/solution" that lack a leading "./" but are still local
//
// Returns true (catalog / remote reference) for:
//   - bare names ("my-app"), versioned names ("my-app@1.0.0")
//   - registry refs where the first segment is hostname-like ("ghcr.io/org/sol:v1",
//     "localhost:5000/sol")
//   - URLs ("https://...", "oci://...")
func IsCatalogReference(s string) bool {
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") {
		return false
	}
	// URLs are not local file paths — they are handled by get.Getter.
	if strings.Contains(s, "://") {
		return true
	}
	lower := strings.ToLower(s)
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".json") {
		return false
	}
	// Windows absolute paths (e.g., "C:\dir\sol" or "C:/dir/sol").
	if len(s) >= 2 && s[1] == ':' {
		return false
	}
	// Any remaining backslash is a Windows path separator → local path.
	if strings.Contains(s, "\\") {
		return false
	}
	// Strings containing "/" without "://" are either registry refs or relative
	// local paths. Distinguish them by the first path segment: registry hostnames
	// always contain "." (ghcr.io) or ":" (localhost:5000), while plain directory
	// names (configs, relative, mydir) do not.
	if strings.Contains(s, "/") {
		firstSegment := strings.SplitN(s, "/", 2)[0]
		if strings.Contains(firstSegment, ".") || strings.Contains(firstSegment, ":") {
			return true
		}
		return false
	}
	return true
}

// tryLocalCatalogForRef checks the local catalog for a remote reference before
// fetching from the remote registry. Returns the solution and true if found.
func (o *Getter) tryLocalCatalogForRef(ctx context.Context, path string) (*solution.Solution, bool) {
	if o.catalogResolver == nil {
		return nil, false
	}
	nameVer := extractNameVersionFromRef(path)
	if nameVer == "" {
		return nil, false
	}
	o.logger.V(1).Info("checking local catalog before remote fetch", "ref", path, "nameVersion", nameVer)
	sol, err := o.fromCatalog(ctx, nameVer)
	if err != nil {
		o.logger.V(1).Info("not found in local catalog, fetching from remote", "ref", path)
		return nil, false
	}
	o.logger.V(1).Info("resolved from local catalog (skipping remote fetch)", "ref", path)
	return sol, true
}

// tryLocalCatalogForRefWithBundle is like tryLocalCatalogForRef but also returns bundle data.
func (o *Getter) tryLocalCatalogForRefWithBundle(ctx context.Context, path string) (*solution.Solution, []byte, bool) {
	if o.catalogResolver == nil {
		return nil, nil, false
	}
	nameVer := extractNameVersionFromRef(path)
	if nameVer == "" {
		return nil, nil, false
	}
	o.logger.V(1).Info("checking local catalog before remote fetch", "ref", path, "nameVersion", nameVer)
	sol, bundleData, err := o.fromCatalogWithBundle(ctx, nameVer)
	if err != nil {
		o.logger.V(1).Info("not found in local catalog, fetching from remote", "ref", path)
		return nil, nil, false
	}
	o.logger.V(1).Info("resolved from local catalog (skipping remote fetch)", "ref", path, "hasBundle", len(bundleData) > 0)
	return sol, bundleData, true
}

// extractNameVersionFromRef extracts a "name@version" string from a Docker-style
// remote reference like "ghcr.io/myorg/solutions/hello-world@0.1.0". Returns
// "hello-world@0.1.0" for local catalog lookup. Returns empty string if the
// reference cannot be parsed (no "/" or no name segment).
func extractNameVersionFromRef(ref string) string {
	ref = strings.TrimPrefix(ref, "oci://")

	// Split off version (@tag or :tag after last /)
	var tag string
	if atIdx := strings.LastIndex(ref, "@"); atIdx != -1 {
		tag = ref[atIdx+1:]
		ref = ref[:atIdx]
	} else if colonIdx := strings.LastIndex(ref, ":"); colonIdx != -1 {
		slashIdx := strings.LastIndex(ref, "/")
		if slashIdx != -1 && colonIdx > slashIdx {
			tag = ref[colonIdx+1:]
			ref = ref[:colonIdx]
		}
	}

	// Name is the last path segment
	lastSlash := strings.LastIndex(ref, "/")
	if lastSlash == -1 {
		return ""
	}
	name := ref[lastSlash+1:]
	if name == "" {
		return ""
	}

	if tag != "" {
		return name + "@" + tag
	}
	return name
}

// isBareName returns true if the path is a bare name suitable for catalog lookup.
// A bare name has no path separators (/, \) and is not a URL.
func (o *Getter) isBareName(path string) bool {
	// Not a bare name if it contains path separators
	if strings.Contains(path, "/") || strings.Contains(path, "\\") {
		return false
	}
	// Not a bare name if it's a URL
	if filepath.IsURL(path) {
		return false
	}
	// Not a bare name if it has a file extension (likely a file)
	if strings.Contains(path, ".yaml") || strings.Contains(path, ".yml") || strings.Contains(path, ".json") {
		return false
	}
	return true
}

// fromCatalog retrieves a solution from the catalog by name[@version].
func (o *Getter) fromCatalog(ctx context.Context, nameWithVersion string) (*solution.Solution, error) {
	content, err := o.catalogResolver.FetchSolution(ctx, nameWithVersion)
	if err != nil {
		return nil, err
	}

	sol := solution.Solution{}
	if err := sol.LoadFromBytes(content); err != nil {
		return nil, fmt.Errorf("failed to parse solution from catalog: %w", err)
	}

	// Mark the solution as coming from catalog
	sol.SetPath(fmt.Sprintf("catalog:%s", nameWithVersion))
	return &sol, nil
}

// fromRemoteRef resolves a Docker-style OCI remote reference (e.g., ghcr.io/myorg/starter-kit@1.0.0)
// using the configured RemoteResolver.
func (o *Getter) fromRemoteRef(ctx context.Context, ref string) (*solution.Solution, error) {
	content, _, err := o.remoteResolver.FetchRemoteSolution(ctx, ref)
	if err != nil {
		return nil, err
	}

	sol := solution.Solution{}
	if err := sol.LoadFromBytes(content); err != nil {
		return nil, fmt.Errorf("failed to parse solution from remote: %w", err)
	}

	sol.SetPath(fmt.Sprintf("remote:%s", ref))
	return &sol, nil
}

// fromRemoteRefWithBundle resolves a Docker-style OCI remote reference and returns
// the solution together with optional bundle data.
func (o *Getter) fromRemoteRefWithBundle(ctx context.Context, ref string) (*solution.Solution, []byte, error) {
	content, bundleData, err := o.remoteResolver.FetchRemoteSolution(ctx, ref)
	if err != nil {
		return nil, nil, err
	}

	sol := solution.Solution{}
	if err := sol.LoadFromBytes(content); err != nil {
		return nil, nil, fmt.Errorf("failed to parse solution from remote: %w", err)
	}

	sol.SetPath(fmt.Sprintf("remote:%s", ref))
	return &sol, bundleData, nil
}

// FromLocalFileSystem reads a solution from the local filesystem at the specified path.
// It uses the configured readFile function (defaults to os.ReadFile) to read the file contents,
// then unmarshals the data into a solution.Solution object. Logging is performed at various stages,
// including reading the file, unmarshalling, and error handling. If successful, the solution's path
// is set and the populated solution is returned. On failure, an empty solution and a wrapped error
// are returned.
//
// Parameters:
//
//	ctx  - The context for cancellation and deadlines (currently unused).
//	path - The filesystem path to the solution file.
//
// Returns:
//
//	*solution.Solution - The loaded solution object (empty on error).
//	error              - An error if reading or unmarshalling fails.
func (o *Getter) FromLocalFileSystem(ctx context.Context, path string) (*solution.Solution, error) {
	if o.readFile == nil {
		o.readFile = os.ReadFile
	}

	// Resolve relative paths against the context working directory (--cwd flag)
	// so that callers don't need to resolve paths before calling this method.
	if !pathlib.IsAbs(path) {
		resolved, err := provider.AbsFromContext(ctx, path)
		if err != nil {
			return &solution.Solution{}, fmt.Errorf("resolving solution path %q: %w", path, err)
		}
		path = resolved
	}

	_, span := telemetry.Tracer(telemetry.TracerSolution).Start(ctx, "solution.FromLocalFileSystem",
		trace.WithAttributes(attribute.String("solution.path", path)),
	)
	defer span.End()

	o.logger.V(1).Info("Reading solution from local filesystem", "path", path)

	data, err := o.readFile(path)
	if err != nil {
		o.logger.V(1).Info("Failed to read file", "path", path, "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &solution.Solution{}, fmt.Errorf("unable to get the solution. Failed reading file '%s': %w", path, err)
	}

	o.logger.V(1).Info("Unmarshalling solution data", "path", path, "size", len(data))

	sol := solution.Solution{}
	err = sol.LoadFromBytes(data)
	if err != nil {
		o.logger.Error(err, "Failed to unmarshal solution", "path", path)
		return &solution.Solution{}, fmt.Errorf("failed to load solution from '%s': %w", path, err)
	}

	o.logger.V(1).Info("Successfully loaded solution from local filesystem", "path", path)
	sol.SetPath(path)

	// Apply compose if the solution references composed files
	if len(sol.Compose) > 0 {
		bundleRoot := pathlib.Dir(path)
		composed, err := bundler.Compose(&sol, bundleRoot, bundler.WithReadFileFunc(o.readFile))
		if err != nil {
			o.logger.Error(err, "Failed to compose solution", "path", path)
			return &solution.Solution{}, fmt.Errorf("unable to compose solution from '%s': %w", path, err)
		}
		composed.SetPath(path)
		o.logger.V(1).Info("Successfully composed solution", "path", path, "composeFiles", len(sol.Compose))
		return composed, nil
	}

	return &sol, nil
}

// FromURL fetches a solution from the specified URL, unmarshals its contents, and returns a Solution object.
// It validates the URL, performs an HTTP GET request, checks for a successful response, reads the response body,
// and unmarshals the solution data. If any step fails, an error is returned with appropriate logging.
// The solution's path is set to the provided URL upon successful retrieval.
//
// Parameters:
//
//	ctx - The context for controlling cancellation and timeouts.
//	url - The URL from which to fetch the solution.
//
// Returns:
//
//	*solution.Solution - The unmarshalled solution object.
//	error - An error if the operation fails at any step.
func (o *Getter) FromURL(ctx context.Context, url string) (*solution.Solution, error) {
	if !filepath.IsURL(url) {
		o.logger.Error(nil, "Invalid URL provided", "url", url)
		return nil, fmt.Errorf("the provided path to the solution is not a valid URL: %s", url)
	}

	ctx, span := telemetry.Tracer(telemetry.TracerSolution).Start(ctx, "solution.FromURL",
		trace.WithAttributes(attribute.String("solution.url", url)),
	)
	defer span.End()

	o.logger.V(1).Info("Fetching solution from URL", "url", url)
	resp, err := o.httpClient.Get(ctx, url)
	if err != nil {
		o.logger.Error(err, "Failed to fetch solution from URL", "url", url)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("unable to get the solution. Failed fetching from URL '%s': %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		o.logger.Error(nil, "Non-200 response from URL", "url", url, "status_code", resp.StatusCode)
		return nil, fmt.Errorf("unable to get the solution. Received non-200 response from URL '%s': %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		o.logger.Error(err, "Failed to read response body", "url", url)
		return nil, fmt.Errorf("unable to get the solution. Failed reading response body from URL '%s': %w", url, err)
	}

	o.logger.V(1).Info("Unmarshalling solution data", "url", url, "size", len(data))

	sol := solution.Solution{}
	err = sol.LoadFromBytes(data)
	if err != nil {
		o.logger.Error(err, "Failed to unmarshal solution", "url", url)
		return nil, fmt.Errorf("failed to load solution from '%s': %w", url, err)
	}

	o.logger.Info("Successfully loaded solution from URL", "url", url)
	sol.SetPath(url)
	return &sol, nil
}

// FindSolution searches for a solution file by iterating over the configured root solution folders
// and solution file names. It returns the full path to the first solution file found using the
// provided stat function. If no solution file is found, it returns an empty string.
func (o *Getter) FindSolution() string {
	for _, folder := range o.solutionFolders {
		for _, filename := range o.solutionFileNames {
			fullPath := filepath.NormalizeFilePath(pathlib.Join(folder, filename))
			if filepath.PathExists(fullPath, o.statFunc) {
				return fullPath
			}
		}
	}
	return ""
}

// PossibleSolutionPaths returns a slice of possible solution file paths by combining
// each root solution folder with each solution file name defined in the settings.
// It constructs the full path for each combination and aggregates them into a list.
func PossibleSolutionPaths() []string {
	paths := make([]string, 0, len(settings.RootSolutionFolders)*len(settings.SolutionFileNames))

	for _, folder := range settings.RootSolutionFolders {
		for _, filename := range settings.SolutionFileNames {
			fullPath := filepath.NormalizeFilePath(pathlib.Join(folder, filename))
			paths = append(paths, fullPath)
		}
	}
	return paths
}
