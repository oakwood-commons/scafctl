// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package get

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/filepath"
	"github.com/oakwood-commons/scafctl/pkg/fs"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/metrics"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// CatalogResolver is an interface for fetching solutions from a catalog.
// This avoids a circular dependency with the catalog package.
type CatalogResolver interface {
	// FetchSolution retrieves a solution from the catalog by name[@version].
	// Returns the solution content bytes and any error.
	FetchSolution(ctx context.Context, nameWithVersion string) ([]byte, error)
}

type Getter struct {
	readFile        fs.ReadFileFunc
	statFunc        fs.StatFunc
	httpClient      *httpc.Client
	logger          logr.Logger
	catalogResolver CatalogResolver
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
		readFile:   os.ReadFile,
		statFunc:   os.Stat,
		httpClient: httpc.NewClient(nil), // Use default HTTP client
		logger:     logr.Discard(),       // Use discard logger by default
	}

	// Apply all options
	for _, opt := range opts {
		opt(g)
	}

	return g
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

	defer func() {
		metrics.GetSolutionTimeHistogram.WithLabelValues(path).Observe(time.Since(start).Seconds())
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
func (o *Getter) FromLocalFileSystem(_ context.Context, path string) (*solution.Solution, error) {
	if o.readFile == nil {
		o.readFile = os.ReadFile
	}

	o.logger.V(1).Info("Reading solution from local filesystem", "path", path)

	data, err := o.readFile(path)
	if err != nil {
		o.logger.V(1).Info("Failed to read file", "path", path, "error", err)
		return &solution.Solution{}, fmt.Errorf("unable to get the solution. Failed reading file '%s': %w", path, err)
	}

	o.logger.V(1).Info("Unmarshalling solution data", "path", path, "size", len(data))

	sol := solution.Solution{}
	err = sol.LoadFromBytes(data)
	if err != nil {
		o.logger.Error(err, "Failed to unmarshal solution", "path", path)
		return &solution.Solution{}, fmt.Errorf("unable to get the solution. Failed unmarshalling data from file '%s': %w", path, err)
	}

	o.logger.V(1).Info("Successfully loaded solution from local filesystem", "path", path)
	sol.SetPath(path)
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

	o.logger.V(1).Info("Fetching solution from URL", "url", url)
	resp, err := o.httpClient.Get(ctx, url)
	if err != nil {
		o.logger.Error(err, "Failed to fetch solution from URL", "url", url)
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
		return nil, fmt.Errorf("unable to get the solution. Failed unmarshalling data from URL '%s': %w", url, err)
	}

	o.logger.Info("Successfully loaded solution from URL", "url", url)
	sol.SetPath(url)
	return &sol, nil
}

// FindSolution searches for a solution file by iterating over the configured root solution folders
// and solution file names. It returns the full path to the first solution file found using the
// provided stat function. If no solution file is found, it returns an empty string.
func (o *Getter) FindSolution() string {
	for _, folder := range settings.RootSolutionFolders {
		for _, filename := range settings.SolutionFileNames {
			fullPath := filepath.Join(folder, filename)
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
			fullPath := filepath.Join(folder, filename)
			paths = append(paths, fullPath)
		}
	}
	return paths
}
