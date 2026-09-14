// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

// RedactedValue is the placeholder inserted for sensitive fields.
const RedactedValue = "***REDACTED***"

// SanitizedConfig mirrors Config but with sensitive fields redacted. It is a
// fail-closed allowlist: every top-level Config section has a corresponding
// entry here, either as a raw copy (when the type has no secret-bearing
// fields) or as a sanitized mirror. New Config fields do NOT flow through
// automatically -- they must be added here explicitly.
type SanitizedConfig struct {
	Version    int                 `json:"version,omitempty" yaml:"version,omitempty" doc:"Config file version" maximum:"10" example:"1"`
	Catalogs   []SanitizedCatalog  `json:"catalogs" yaml:"catalogs" doc:"Configured solution catalogs" maxItems:"50"`
	Settings   Settings            `json:"settings" yaml:"settings" doc:"General application settings"`
	Logging    LoggingConfig       `json:"logging" yaml:"logging" doc:"Logging configuration"`
	Telemetry  TelemetryConfig     `json:"telemetry,omitempty" yaml:"telemetry,omitempty" doc:"OpenTelemetry configuration"`
	HTTPClient HTTPClientConfig    `json:"httpClient" yaml:"httpClient" doc:"HTTP client configuration"`
	CEL        CELConfig           `json:"cel" yaml:"cel" doc:"CEL expression engine configuration"`
	GoTemplate GoTemplateConfig    `json:"goTemplate,omitempty" yaml:"goTemplate,omitempty" doc:"Go template engine configuration"`
	Resolver   ResolverConfig      `json:"resolver" yaml:"resolver" doc:"Resolver execution configuration"`
	Action     ActionConfig        `json:"action" yaml:"action" doc:"Action execution configuration"`
	Auth       SanitizedAuth       `json:"auth" yaml:"auth" doc:"Authentication configuration (redacted)"`
	Build      BuildConfig         `json:"build" yaml:"build" doc:"Build configuration"`
	APIServer  SanitizedAPIServer  `json:"apiServer,omitempty" yaml:"apiServer,omitempty" doc:"REST API server configuration (TLS key path and opaque auth handler map redacted)"`
	Discovery  DiscoveryConfig     `json:"discovery,omitempty" yaml:"discovery,omitempty" doc:"Auto-discovery configuration"`
	Plugins    PluginsConfig       `json:"plugins,omitempty" yaml:"plugins,omitempty" doc:"Plugin management configuration"`
	MCP        SanitizedMCPConfig  `json:"mcp,omitempty" yaml:"mcp,omitempty" doc:"MCP server configuration (URLs redacted)"`
	Kube       SanitizedKubeConfig `json:"kube,omitempty" yaml:"kube,omitempty" doc:"Kubernetes cluster resolution (resolver headers redacted)"`
}

// SanitizedCatalog redacts auth tokens from catalog config.
type SanitizedCatalog struct {
	Name     string            `json:"name" yaml:"name" doc:"Catalog name" maxLength:"256" example:"my-catalog"`
	Type     string            `json:"type" yaml:"type" doc:"Catalog type" maxLength:"64" example:"git"`
	Path     string            `json:"path,omitempty" yaml:"path,omitempty" doc:"Local filesystem path" maxLength:"1024" example:"/path/to/catalog"`
	URL      string            `json:"url,omitempty" yaml:"url,omitempty" doc:"Remote URL" maxLength:"2048" example:"https://github.com/org/catalog"`
	Auth     *SanitizedCatAuth `json:"auth,omitempty" yaml:"auth,omitempty" doc:"Authentication settings (redacted)"`
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty" doc:"Additional metadata"`
}

// SanitizedCatAuth contains only non-sensitive catalog auth fields.
type SanitizedCatAuth struct {
	Type        string `json:"type" yaml:"type" doc:"Authentication type" maxLength:"64" example:"token"`
	TokenEnvVar string `json:"tokenEnvVar,omitempty" yaml:"tokenEnvVar,omitempty" doc:"Environment variable name for token" maxLength:"256" example:"CATALOG_TOKEN"`
}

// SanitizedAuth redacts client secrets and tokens from auth config.
type SanitizedAuth struct {
	HTTPClient                 *HTTPClientConfig               `json:"httpClient,omitempty" yaml:"httpClient,omitempty" doc:"HTTP client overrides for auth handlers"`
	Entra                      *SanitizedEntraAuth             `json:"entra,omitempty" yaml:"entra,omitempty" doc:"Entra ID auth configuration (redacted)"`
	GitHub                     *SanitizedGitHubAuth            `json:"github,omitempty" yaml:"github,omitempty" doc:"GitHub auth configuration (redacted)"`
	GCP                        *SanitizedGCPAuth               `json:"gcp,omitempty" yaml:"gcp,omitempty" doc:"GCP auth configuration (redacted)"`
	CustomOAuth2               []SanitizedCustomOAuth2         `json:"customOAuth2,omitempty" yaml:"customOAuth2,omitempty" doc:"User-defined OAuth2 auth handlers (secrets redacted)" maxItems:"20"`
	Handlers                   map[string]SanitizedAuthHandler `json:"handlers,omitempty" yaml:"handlers,omitempty" doc:"Per-handler configuration; opaque plugin settings dropped"`
	TrustedVerificationDomains []string                        `json:"trustedVerificationDomains,omitempty" yaml:"trustedVerificationDomains,omitempty" doc:"Additional trusted domains for device code verification URIs" maxItems:"50"`
}

// SanitizedEntraAuth contains only non-sensitive Entra ID fields.
type SanitizedEntraAuth struct {
	ClientID      string   `json:"clientId,omitempty" yaml:"clientId,omitempty" doc:"Entra ID application client ID" maxLength:"256" example:"00000000-0000-0000-0000-000000000000"`
	TenantID      string   `json:"tenantId,omitempty" yaml:"tenantId,omitempty" doc:"Entra ID tenant ID" maxLength:"256" example:"00000000-0000-0000-0000-000000000000"`
	DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" doc:"Default OAuth scopes" maxItems:"20"`
	DefaultFlow   string   `json:"defaultFlow,omitempty" yaml:"defaultFlow,omitempty" doc:"Default interactive auth flow" maxLength:"32" example:"device_code"`
}

// SanitizedGitHubAuth contains only non-sensitive GitHub auth fields.
type SanitizedGitHubAuth struct {
	ClientID      string   `json:"clientId,omitempty" yaml:"clientId,omitempty" doc:"GitHub OAuth app client ID" maxLength:"256" example:"Iv1.abc123"`
	Hostname      string   `json:"hostname,omitempty" yaml:"hostname,omitempty" doc:"GitHub hostname" maxLength:"256" example:"github.com"`
	DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" doc:"Default OAuth scopes" maxItems:"20"`
}

// SanitizedGCPAuth contains only non-sensitive GCP auth fields.
type SanitizedGCPAuth struct {
	ClientID                  string   `json:"clientId,omitempty" yaml:"clientId,omitempty" doc:"GCP OAuth client ID" maxLength:"256" example:"123456789.apps.googleusercontent.com"`
	GCPClientCredential       string   `json:"gcpClientCredential,omitempty" yaml:"gcpClientCredential,omitempty" doc:"GCP client credential file path" maxLength:"1024" example:"/path/to/credentials.json"`
	DefaultScopes             []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" doc:"Default OAuth scopes" maxItems:"20"`
	ImpersonateServiceAccount string   `json:"impersonateServiceAccount,omitempty" yaml:"impersonateServiceAccount,omitempty" doc:"Service account to impersonate" maxLength:"512" example:"sa@project.iam.gserviceaccount.com"`
	Project                   string   `json:"project,omitempty" yaml:"project,omitempty" doc:"GCP project ID" maxLength:"256" example:"my-gcp-project"`
}

// SanitizedCustomOAuth2 mirrors CustomOAuth2Config with the client secret and
// token-exchange request body dropped.
type SanitizedCustomOAuth2 struct {
	Name                      string                              `json:"name" yaml:"name" doc:"Handler name" maxLength:"64" example:"quay"`
	DisplayName               string                              `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Human-readable display name" maxLength:"128"`
	AuthorizeURL              string                              `json:"authorizeURL,omitempty" yaml:"authorizeURL,omitempty" doc:"OAuth2 authorization endpoint" maxLength:"2048"`
	TokenURL                  string                              `json:"tokenURL" yaml:"tokenURL" doc:"OAuth2 token endpoint" maxLength:"2048"`
	DeviceAuthURL             string                              `json:"deviceAuthURL,omitempty" yaml:"deviceAuthURL,omitempty" doc:"OAuth2 device authorization endpoint" maxLength:"2048"`
	ClientID                  string                              `json:"clientID" yaml:"clientID" doc:"OAuth2 client ID" maxLength:"256"`
	Scopes                    []string                            `json:"scopes,omitempty" yaml:"scopes,omitempty" doc:"Default OAuth scopes" maxItems:"20"`
	DefaultFlow               string                              `json:"defaultFlow,omitempty" yaml:"defaultFlow,omitempty" doc:"Default OAuth2 flow" maxLength:"32"`
	CallbackPort              int                                 `json:"callbackPort,omitempty" yaml:"callbackPort,omitempty" doc:"Local callback port"`
	CallbackPath              string                              `json:"callbackPath,omitempty" yaml:"callbackPath,omitempty" doc:"Callback path" maxLength:"256"`
	CallbackHost              string                              `json:"callbackHost,omitempty" yaml:"callbackHost,omitempty" doc:"Callback host" maxLength:"253"`
	DeviceCodePollInterval    int                                 `json:"deviceCodePollInterval,omitempty" yaml:"deviceCodePollInterval,omitempty" doc:"Device code poll interval"`
	DisablePKCE               bool                                `json:"disablePKCE,omitempty" yaml:"disablePKCE,omitempty" doc:"Disable PKCE"`
	ResponseType              string                              `json:"responseType,omitempty" yaml:"responseType,omitempty" doc:"OAuth2 response type" maxLength:"16"`
	VerifyURL                 string                              `json:"verifyURL,omitempty" yaml:"verifyURL,omitempty" doc:"Token verification endpoint" maxLength:"2048"`
	IdentityFields            *IdentityFieldMapping               `json:"identityFields,omitempty" yaml:"identityFields,omitempty" doc:"Field mapping from verify response to identity claims"`
	Registry                  string                              `json:"registry,omitempty" yaml:"registry,omitempty" doc:"OCI registry host" maxLength:"253"`
	RegistryUsername          string                              `json:"registryUsername,omitempty" yaml:"registryUsername,omitempty" doc:"Registry username" maxLength:"256"`
	TokenExchange             *SanitizedTokenExchange             `json:"tokenExchange,omitempty" yaml:"tokenExchange,omitempty" doc:"Token exchange configuration (request body redacted)"`
	DynamicClientRegistration *SanitizedDynamicClientRegistration `json:"dynamicClientRegistration,omitempty" yaml:"dynamicClientRegistration,omitempty" doc:"RFC 7591 Dynamic Client Registration configuration (initial access token redacted)"`
}

// SanitizedTokenExchange mirrors TokenExchangeConfig with the request body
// dropped -- it is a Go template that can render credentials into the wire
// request.
type SanitizedTokenExchange struct {
	URL              string `json:"url" yaml:"url" doc:"API endpoint to derive a secondary credential" maxLength:"2048"`
	Method           string `json:"method,omitempty" yaml:"method,omitempty" doc:"HTTP method" maxLength:"10"`
	TokenJSONPath    string `json:"tokenJSONPath" yaml:"tokenJSONPath" doc:"JSON path to the derived token" maxLength:"256"`
	UsernameJSONPath string `json:"usernameJSONPath,omitempty" yaml:"usernameJSONPath,omitempty" doc:"JSON path to username" maxLength:"256"`
}

// SanitizedDynamicClientRegistration mirrors DynamicClientRegistrationConfig
// with the initial access token dropped -- it is a bearer credential used to
// authorize the registration request.
type SanitizedDynamicClientRegistration struct {
	RegistrationEndpoint string         `json:"registrationEndpoint,omitempty" yaml:"registrationEndpoint,omitempty" doc:"RFC 7591 Dynamic Client Registration endpoint" maxLength:"2048"`
	ClientMetadata       map[string]any `json:"clientMetadata,omitempty" yaml:"clientMetadata,omitempty" doc:"Client metadata for dynamic registration"`
}

// SanitizedAuthHandler mirrors HandlerConfig but drops the opaque plugin
// Settings map (arbitrary shape, could hold secrets) and redacts any
// resolver-source Headers values.
type SanitizedAuthHandler struct {
	Hostname                   *SanitizedHostnameConfig `json:"hostname,omitempty" yaml:"hostname,omitempty" doc:"Host-side hostname alias/endpoint resolution"`
	Plugin                     *HandlerPluginConfig     `json:"plugin,omitempty" yaml:"plugin,omitempty" doc:"Catalog pin for a third-party auth handler plugin"`
	TrustedVerificationDomains []string                 `json:"trustedVerificationDomains,omitempty" yaml:"trustedVerificationDomains,omitempty" doc:"Per-handler trusted device-code verification domains" maxItems:"50"`
}

// SanitizedHostnameConfig mirrors HostnameConfig with resolver source headers
// redacted.
type SanitizedHostnameConfig struct {
	Aliases  map[string]string                `json:"aliases,omitempty" yaml:"aliases,omitempty" doc:"Static map of hostname selector to concrete endpoint URL"`
	Resolver *SanitizedHostnameResolverConfig `json:"resolver,omitempty" yaml:"resolver,omitempty" doc:"Dynamic hostname inventory resolver"`
}

// SanitizedHostnameResolverConfig mirrors HostnameResolverConfig.
type SanitizedHostnameResolverConfig struct {
	Source    SanitizedHostnameResolverSource `json:"source,omitempty" yaml:"source,omitempty" doc:"Where to fetch the hostname inventory"`
	Transform string                          `json:"transform,omitempty" yaml:"transform,omitempty" doc:"CEL normalization expression" maxLength:"8192"`
	TTL       string                          `json:"ttl,omitempty" yaml:"ttl,omitempty" doc:"Cache duration" maxLength:"32"`
}

// SanitizedHostnameResolverSource mirrors HostnameResolverSource with the
// arbitrary Headers map values redacted -- they can carry bearer tokens.
type SanitizedHostnameResolverSource struct {
	URL          string            `json:"url,omitempty" yaml:"url,omitempty" doc:"Inventory endpoint URL" maxLength:"2048"`
	AuthProvider string            `json:"authProvider,omitempty" yaml:"authProvider,omitempty" doc:"Auth handler name" maxLength:"64"`
	AuthScope    string            `json:"authScope,omitempty" yaml:"authScope,omitempty" doc:"OAuth scope" maxLength:"1024"`
	Headers      map[string]string `json:"headers,omitempty" yaml:"headers,omitempty" doc:"Additional static request headers (values redacted)"`
}

// SanitizedKubeConfig mirrors KubeConfig.
type SanitizedKubeConfig struct {
	Clusters SanitizedClusterResolutionConfig `json:"clusters,omitempty" yaml:"clusters,omitempty" doc:"Cluster name resolution"`
}

// SanitizedClusterResolutionConfig mirrors ClusterResolutionConfig with the
// dynamic resolver's Headers redacted. Static Aliases are preserved verbatim:
// their fields (Server URL, DefaultHandler, AuthType, OIDCAudience, CAData,
// ConsoleURL, InsecureSkipTLS) are cluster identifiers, not credentials.
type SanitizedClusterResolutionConfig struct {
	Aliases  map[string]ClusterAlias          `json:"aliases,omitempty" yaml:"aliases,omitempty" doc:"Static map of cluster selector to connection details"`
	Resolver *SanitizedHostnameResolverConfig `json:"resolver,omitempty" yaml:"resolver,omitempty" doc:"Dynamic cluster inventory resolver"`
}

// SanitizedAPIServer mirrors APIServerConfig with the TLS private key path
// redacted and the opaque APIAuthConfig.Handlers plugin map redacted.
// Everything else is a raw copy -- no other APIServerConfig field is
// secret-bearing.
type SanitizedAPIServer struct {
	Host             string                  `json:"host,omitempty" yaml:"host,omitempty" doc:"Bind host" maxLength:"253"`
	Port             int                     `json:"port,omitempty" yaml:"port,omitempty" doc:"Listen port"`
	APIVersion       string                  `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty" doc:"API version prefix" maxLength:"10"`
	ShutdownTimeout  string                  `json:"shutdownTimeout,omitempty" yaml:"shutdownTimeout,omitempty" doc:"Graceful shutdown timeout" maxLength:"20"`
	RequestTimeout   string                  `json:"requestTimeout,omitempty" yaml:"requestTimeout,omitempty" doc:"Default request timeout" maxLength:"20"`
	IdleTimeout      string                  `json:"idleTimeout,omitempty" yaml:"idleTimeout,omitempty" doc:"Keep-alive idle connection timeout" maxLength:"20"`
	MaxHeaderBytes   int                     `json:"maxHeaderBytes,omitempty" yaml:"maxHeaderBytes,omitempty" doc:"Maximum size of request headers in bytes"`
	AllowedHosts     []string                `json:"allowedHosts,omitempty" yaml:"allowedHosts,omitempty" doc:"Host header values this server will answer to" maxItems:"50"`
	BodyReadTimeout  string                  `json:"bodyReadTimeout,omitempty" yaml:"bodyReadTimeout,omitempty" doc:"Body read timeout" maxLength:"20"`
	MaxRequestSize   int64                   `json:"maxRequestSize,omitempty" yaml:"maxRequestSize,omitempty" doc:"Max request body size"`
	TLS              SanitizedAPITLSConfig   `json:"tls,omitempty" yaml:"tls,omitempty" doc:"TLS configuration (key path redacted)"`
	CORS             APICORSConfig           `json:"cors,omitempty" yaml:"cors,omitempty" doc:"CORS configuration"`
	RateLimit        APIRateLimitConfig      `json:"rateLimit,omitempty" yaml:"rateLimit,omitempty" doc:"Rate limiting configuration"`
	Auth             SanitizedAPIAuthConfig  `json:"auth,omitempty" yaml:"auth,omitempty" doc:"Authentication configuration (opaque handler settings redacted)"`
	Compression      APICompressionConfig    `json:"compression,omitempty" yaml:"compression,omitempty" doc:"Response compression configuration"`
	OpenAPI          APIOpenAPIConfig        `json:"openAPI,omitempty" yaml:"openAPI,omitempty" doc:"OpenAPI specification configuration"`
	Profiler         APIProfilerConfig       `json:"profiler,omitempty" yaml:"profiler,omitempty" doc:"Profiler configuration"`
	Audit            APIAuditConfig          `json:"audit,omitempty" yaml:"audit,omitempty" doc:"Audit logging configuration"`
	Tracing          APITracingConfig        `json:"tracing,omitempty" yaml:"tracing,omitempty" doc:"Tracing configuration"`
	MaxConcurrent    int                     `json:"maxConcurrent,omitempty" yaml:"maxConcurrent,omitempty" doc:"Max concurrent requests"`
	Plugins          APIPluginConfig         `json:"plugins,omitempty" yaml:"plugins,omitempty" doc:"Plugin security configuration"`
	TokenPassThrough *TokenPassThroughConfig `json:"tokenPassThrough,omitempty" yaml:"tokenPassThrough,omitempty" doc:"Token pass-through configuration"`
}

// SanitizedAPITLSConfig mirrors APITLSConfig with the private key path
// redacted. The cert path is preserved -- it points at a public artifact.
type SanitizedAPITLSConfig struct {
	Enabled bool   `json:"enabled,omitempty" yaml:"enabled,omitempty" doc:"Enable TLS"`
	Cert    string `json:"cert,omitempty" yaml:"cert,omitempty" doc:"Path to TLS certificate file" maxLength:"4096"`
	Key     string `json:"key,omitempty" yaml:"key,omitempty" doc:"Path to TLS private key file (redacted)" maxLength:"4096"`
}

// SanitizedAPIAuthConfig mirrors APIAuthConfig with the opaque Handlers plugin
// map values redacted -- shape is defined by third-party plugins and may
// carry credentials.
type SanitizedAPIAuthConfig struct {
	AzureOIDC APIAzureOIDCConfig `json:"azureOIDC,omitempty" yaml:"azureOIDC,omitempty" doc:"Azure AD OIDC configuration"`
	Handlers  map[string]string  `json:"handlers,omitempty" yaml:"handlers,omitempty" doc:"Auth handler plugin settings keyed by handler name (values redacted)"`
}

// SanitizedMCPConfig mirrors MCPConfig but with URLs redacted.
type SanitizedMCPConfig struct {
	Servers map[string]SanitizedMCPServerConfig `json:"servers,omitempty" yaml:"servers,omitempty" doc:"Upstream MCP server configurations (URLs redacted)"`
}

// SanitizedMCPServerConfig contains only non-sensitive upstream MCP server fields.
type SanitizedMCPServerConfig struct {
	Enabled    *bool               `json:"enabled,omitempty" yaml:"enabled,omitempty" doc:"Whether this upstream server is active"`
	URL        string              `json:"url,omitempty" yaml:"url,omitempty" doc:"Upstream URL (redacted)"`
	Auth       MCPServerAuthConfig `json:"auth,omitempty" yaml:"auth,omitempty" doc:"Authentication configuration"`
	Timeout    string              `json:"timeout,omitempty" yaml:"timeout,omitempty" doc:"Request timeout"`
	ToolPrefix string              `json:"toolPrefix,omitempty" yaml:"toolPrefix,omitempty" doc:"Prefix added to upstream tool names"`
	Tools      []string            `json:"tools,omitempty" yaml:"tools,omitempty" doc:"Tool name allowlist patterns"`
}

// SanitizeConfig creates a sanitized copy of the config with sensitive values
// redacted. It is a fail-closed allowlist: only explicitly-copied fields make
// it into the result. Add a new field to the sanitized types AND to this
// function when extending Config with a new section.
func SanitizeConfig(cfg *Config) SanitizedConfig {
	s := SanitizedConfig{
		Version:    cfg.Version,
		Settings:   cfg.Settings,
		Logging:    cfg.Logging,
		Telemetry:  cfg.Telemetry,
		HTTPClient: cfg.HTTPClient,
		CEL:        cfg.CEL,
		GoTemplate: cfg.GoTemplate,
		Resolver:   cfg.Resolver,
		Action:     cfg.Action,
		Build:      cfg.Build,
		APIServer:  sanitizeAPIServer(cfg.APIServer),
		Discovery:  cfg.Discovery,
		Plugins:    cfg.Plugins,
		Kube:       sanitizeKube(cfg.Kube),
	}

	// Sanitize MCP upstream servers -- redact URLs.
	if len(cfg.MCP.Servers) > 0 {
		s.MCP.Servers = make(map[string]SanitizedMCPServerConfig, len(cfg.MCP.Servers))
		for name, srv := range cfg.MCP.Servers {
			sanitized := SanitizedMCPServerConfig{
				Enabled:    srv.Enabled,
				Auth:       srv.Auth,
				Timeout:    srv.Timeout,
				ToolPrefix: srv.ToolPrefix,
				Tools:      srv.Tools,
			}
			if srv.URL != "" {
				sanitized.URL = RedactedValue
			}
			s.MCP.Servers[name] = sanitized
		}
	}

	// Sanitize catalogs
	s.Catalogs = make([]SanitizedCatalog, 0, len(cfg.Catalogs))
	for _, cat := range cfg.Catalogs {
		sc := SanitizedCatalog{
			Name:     cat.Name,
			Type:     cat.Type,
			Path:     cat.Path,
			URL:      cat.URL,
			Metadata: cat.Metadata,
		}
		if cat.Auth != nil {
			sc.Auth = &SanitizedCatAuth{
				Type:        cat.Auth.Type,
				TokenEnvVar: cat.Auth.TokenEnvVar,
			}
		}
		s.Catalogs = append(s.Catalogs, sc)
	}

	// Sanitize auth -- redact secrets
	s.Auth.HTTPClient = cfg.Auth.HTTPClient
	s.Auth.TrustedVerificationDomains = cfg.Auth.TrustedVerificationDomains
	if cfg.Auth.Entra != nil {
		s.Auth.Entra = &SanitizedEntraAuth{
			ClientID:      cfg.Auth.Entra.ClientID,
			TenantID:      cfg.Auth.Entra.TenantID,
			DefaultScopes: cfg.Auth.Entra.DefaultScopes,
			DefaultFlow:   cfg.Auth.Entra.DefaultFlow,
		}
	}
	if cfg.Auth.GitHub != nil {
		s.Auth.GitHub = &SanitizedGitHubAuth{
			ClientID:      cfg.Auth.GitHub.ClientID,
			Hostname:      cfg.Auth.GitHub.Hostname,
			DefaultScopes: cfg.Auth.GitHub.DefaultScopes,
		}
	}
	if cfg.Auth.GCP != nil {
		gcp := &SanitizedGCPAuth{
			ClientID:                  cfg.Auth.GCP.ClientID,
			DefaultScopes:             cfg.Auth.GCP.DefaultScopes,
			ImpersonateServiceAccount: cfg.Auth.GCP.ImpersonateServiceAccount,
			Project:                   cfg.Auth.GCP.Project,
		}
		if cfg.Auth.GCP.ClientSecret != "" {
			gcp.GCPClientCredential = RedactedValue
		}
		s.Auth.GCP = gcp
	}
	if len(cfg.Auth.CustomOAuth2) > 0 {
		s.Auth.CustomOAuth2 = make([]SanitizedCustomOAuth2, 0, len(cfg.Auth.CustomOAuth2))
		for _, oc := range cfg.Auth.CustomOAuth2 {
			s.Auth.CustomOAuth2 = append(s.Auth.CustomOAuth2, sanitizeCustomOAuth2(oc))
		}
	}
	if len(cfg.Auth.Handlers) > 0 {
		s.Auth.Handlers = make(map[string]SanitizedAuthHandler, len(cfg.Auth.Handlers))
		for name, h := range cfg.Auth.Handlers {
			s.Auth.Handlers[name] = sanitizeAuthHandler(h)
		}
	}

	return s
}

// sanitizeCustomOAuth2 copies non-sensitive fields and drops the client
// secret and any templated request body.
func sanitizeCustomOAuth2(oc CustomOAuth2Config) SanitizedCustomOAuth2 {
	out := SanitizedCustomOAuth2{
		Name:                   oc.Name,
		DisplayName:            oc.DisplayName,
		AuthorizeURL:           oc.AuthorizeURL,
		TokenURL:               oc.TokenURL,
		DeviceAuthURL:          oc.DeviceAuthURL,
		ClientID:               oc.ClientID,
		Scopes:                 oc.Scopes,
		DefaultFlow:            oc.DefaultFlow,
		CallbackPort:           oc.CallbackPort,
		CallbackPath:           oc.CallbackPath,
		CallbackHost:           oc.CallbackHost,
		DeviceCodePollInterval: oc.DeviceCodePollInterval,
		DisablePKCE:            oc.DisablePKCE,
		ResponseType:           oc.ResponseType,
		VerifyURL:              oc.VerifyURL,
		IdentityFields:         oc.IdentityFields,
		Registry:               oc.Registry,
		RegistryUsername:       oc.RegistryUsername,
	}
	if oc.TokenExchange != nil {
		out.TokenExchange = &SanitizedTokenExchange{
			URL:              oc.TokenExchange.URL,
			Method:           oc.TokenExchange.Method,
			TokenJSONPath:    oc.TokenExchange.TokenJSONPath,
			UsernameJSONPath: oc.TokenExchange.UsernameJSONPath,
		}
	}
	if oc.DynamicClientRegistration != nil {
		out.DynamicClientRegistration = &SanitizedDynamicClientRegistration{
			RegistrationEndpoint: oc.DynamicClientRegistration.RegistrationEndpoint,
			ClientMetadata:       oc.DynamicClientRegistration.ClientMetadata,
		}
	}
	return out
}

// sanitizeAuthHandler drops the opaque plugin Settings map and redacts any
// resolver-source header values on the host-consumed Hostname block.
func sanitizeAuthHandler(h HandlerConfig) SanitizedAuthHandler {
	out := SanitizedAuthHandler{
		Plugin:                     h.Plugin,
		TrustedVerificationDomains: h.TrustedVerificationDomains,
	}
	if h.Hostname != nil {
		out.Hostname = sanitizeHostnameConfig(h.Hostname)
	}
	return out
}

// sanitizeHostnameConfig preserves the static aliases (endpoints, useful for
// verification) and delegates resolver sanitization.
func sanitizeHostnameConfig(h *HostnameConfig) *SanitizedHostnameConfig {
	out := &SanitizedHostnameConfig{Aliases: h.Aliases}
	if h.Resolver != nil {
		sr := sanitizeHostnameResolverConfig(*h.Resolver)
		out.Resolver = &sr
	}
	return out
}

// sanitizeHostnameResolverConfig redacts the source's Headers map values --
// they can carry Authorization bearer tokens.
func sanitizeHostnameResolverConfig(r HostnameResolverConfig) SanitizedHostnameResolverConfig {
	src := SanitizedHostnameResolverSource{
		URL:          r.Source.URL,
		AuthProvider: r.Source.AuthProvider,
		AuthScope:    r.Source.AuthScope,
	}
	if len(r.Source.Headers) > 0 {
		src.Headers = make(map[string]string, len(r.Source.Headers))
		for k := range r.Source.Headers {
			src.Headers[k] = RedactedValue
		}
	}
	return SanitizedHostnameResolverConfig{
		Source:    src,
		Transform: r.Transform,
		TTL:       r.TTL,
	}
}

// sanitizeKube reuses the hostname-resolver sanitizer for the shared inventory
// contract. Static ClusterAlias entries are preserved verbatim: their fields
// are cluster identifiers (API URL, handler name, audience, CA bundle path,
// console URL, TLS skip flag), not credentials.
func sanitizeKube(k KubeConfig) SanitizedKubeConfig {
	out := SanitizedKubeConfig{
		Clusters: SanitizedClusterResolutionConfig{
			Aliases: k.Clusters.Aliases,
		},
	}
	if k.Clusters.Resolver != nil {
		sr := sanitizeHostnameResolverConfig(*k.Clusters.Resolver)
		out.Clusters.Resolver = &sr
	}
	return out
}

// sanitizeAPIServer fixes a pre-existing raw-copy bug: APIServerConfig was
// previously assigned unchanged into SanitizedConfig, leaking TLS.Key paths
// and the opaque APIAuthConfig.Handlers plugin map. Everything except those
// two fields is a straight copy.
func sanitizeAPIServer(api APIServerConfig) SanitizedAPIServer {
	out := SanitizedAPIServer{
		Host:            api.Host,
		Port:            api.Port,
		APIVersion:      api.APIVersion,
		ShutdownTimeout: api.ShutdownTimeout,
		RequestTimeout:  api.RequestTimeout,
		IdleTimeout:     api.IdleTimeout,
		MaxHeaderBytes:  api.MaxHeaderBytes,
		AllowedHosts:    api.AllowedHosts,
		BodyReadTimeout: api.BodyReadTimeout,
		MaxRequestSize:  api.MaxRequestSize,
		TLS: SanitizedAPITLSConfig{
			Enabled: api.TLS.Enabled,
			Cert:    api.TLS.Cert,
		},
		CORS:      api.CORS,
		RateLimit: api.RateLimit,
		Auth: SanitizedAPIAuthConfig{
			AzureOIDC: api.Auth.AzureOIDC,
		},
		Compression:      api.Compression,
		OpenAPI:          api.OpenAPI,
		Profiler:         api.Profiler,
		Audit:            api.Audit,
		Tracing:          api.Tracing,
		MaxConcurrent:    api.MaxConcurrent,
		Plugins:          api.Plugins,
		TokenPassThrough: api.TokenPassThrough,
	}
	if api.TLS.Key != "" {
		out.TLS.Key = RedactedValue
	}
	if len(api.Auth.Handlers) > 0 {
		out.Auth.Handlers = make(map[string]string, len(api.Auth.Handlers))
		for name := range api.Auth.Handlers {
			out.Auth.Handlers[name] = RedactedValue
		}
	}
	return out
}
