// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"encoding/base64"

	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// ManagedEntry describes a scafctl-managed kubeconfig context: a context whose
// user is a scafctl exec-credential entry written by "kube login".
type ManagedEntry struct {
	// ClusterName is the kubeconfig cluster entry the context references.
	ClusterName string

	// ContextName is the kubeconfig context entry name.
	ContextName string

	// UserName is the kubeconfig user entry the context references.
	UserName string

	// Server is the referenced cluster's API server URL, when present.
	Server string
}

// ContextStatus describes the current kubeconfig context for "kube status".
type ContextStatus struct {
	// Context is the current-context name; empty when none is set.
	Context string

	// Cluster and User are the entries the context references.
	Cluster string
	User    string

	// Namespace is the context's default namespace, when set.
	Namespace string

	// Server is the referenced cluster's API server URL, when present.
	Server string

	// Managed reports whether the context's user is a scafctl exec-credential
	// entry (written by "kube login") for the querying binary.
	Managed bool

	// Handler is the auth handler baked into a managed entry's exec block.
	// Empty for non-managed contexts.
	Handler string

	// Profile is the auth profile baked into a managed entry's exec block
	// (--profile). Empty when none was set.
	Profile string

	// Scope is the exec credential scope baked into a managed entry's exec block
	// (--scope), i.e. the cluster's OIDC audience. Empty when none was set.
	Scope string

	// CAData is the PEM-encoded cluster CA bundle decoded from the kubeconfig
	// cluster entry, when present. Used for a best-effort whoami over TLS.
	CAData string

	// InsecureSkipTLS mirrors the cluster entry's insecure-skip-tls-verify flag.
	InsecureSkipTLS bool

	// Username and Groups come from a best-effort whoami; empty when the whoami
	// did not run or did not succeed.
	Username string
	Groups   []string
}

// managedUser captures the handler and profile baked into a managed user's exec
// block, extracted from its "auth token <handler> ... --profile <profile>" args.
type managedUser struct {
	handler string
	profile string
	scope   string
}

// ListManaged reads the kubeconfig at path (empty resolves KUBECONFIG or
// ~/.kube/config) and returns the contexts whose user is a scafctl exec-
// credential entry for binaryName. An entry is scafctl-managed when its user's
// exec block invokes binaryName with a leading "auth token" argument pair, which
// is exactly what "kube login" writes. A missing kubeconfig yields an empty list
// and no error.
func ListManaged(binaryName, kubeconfigPath string) ([]ManagedEntry, error) {
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}
	path, err := resolveKubeconfigPath(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	cfg, err := loadKubeconfig(path)
	if err != nil {
		return nil, err
	}

	managed := managedUsers(toAnySlice(cfg[keyUsers]), binaryName)
	if len(managed) == 0 {
		return nil, nil
	}
	servers := clusterServers(toAnySlice(cfg[keyClusters]))

	var out []ManagedEntry
	for _, item := range toAnySlice(cfg[keyContexts]) {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		inner, _ := m["context"].(map[string]any)
		if inner == nil {
			continue
		}
		userName, _ := inner["user"].(string)
		if _, ok := managed[userName]; !ok {
			continue
		}
		clusterName, _ := inner["cluster"].(string)
		out = append(out, ManagedEntry{
			ClusterName: clusterName,
			ContextName: name,
			UserName:    userName,
			Server:      servers[clusterName],
		})
	}
	return out, nil
}

// CurrentContext reads the current-context from the kubeconfig at path (empty
// resolves KUBECONFIG or ~/.kube/config) and reports its cluster, user,
// namespace, and server. Managed reports whether the context's user is a
// scafctl exec-credential entry for binaryName. A missing kubeconfig or absent
// current-context yields a zero ContextStatus and no error.
func CurrentContext(binaryName, kubeconfigPath string) (ContextStatus, error) {
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}
	path, err := resolveKubeconfigPath(kubeconfigPath)
	if err != nil {
		return ContextStatus{}, err
	}
	cfg, err := loadKubeconfig(path)
	if err != nil {
		return ContextStatus{}, err
	}

	current, _ := cfg[keyCurrentContext].(string)
	st := ContextStatus{Context: current}
	if current == "" {
		return st, nil
	}

	inner := namedInner(toAnySlice(cfg[keyContexts]), current, "context")
	if inner == nil {
		return st, nil
	}
	st.Cluster, _ = inner["cluster"].(string)
	st.User, _ = inner["user"].(string)
	st.Namespace, _ = inner["namespace"].(string)

	clusterEntry := namedInner(toAnySlice(cfg[keyClusters]), st.Cluster, "cluster")
	st.Server, _ = clusterEntry["server"].(string)
	st.CAData = decodeCAData(clusterEntry)
	st.InsecureSkipTLS, _ = clusterEntry["insecure-skip-tls-verify"].(bool)

	if mu, ok := managedUsers(toAnySlice(cfg[keyUsers]), binaryName)[st.User]; ok {
		st.Managed = true
		st.Handler = mu.handler
		st.Profile = mu.profile
		st.Scope = mu.scope
	}
	return st, nil
}

// managedUsers returns the managed user entries keyed by name. A user is managed
// when its exec block invokes binaryName with a leading "auth token" argument
// pair. The value captures the handler (args[2]) and the --profile value baked
// into the exec args, when present.
func managedUsers(users []any, binaryName string) map[string]managedUser {
	out := make(map[string]managedUser)
	for _, item := range users {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		inner, _ := m["user"].(map[string]any)
		if name == "" || inner == nil {
			continue
		}
		exec, _ := inner["exec"].(map[string]any)
		if exec == nil {
			continue
		}
		if cmd, _ := exec["command"].(string); cmd != binaryName {
			continue
		}
		args := toAnySlice(exec["args"])
		if !isAuthTokenArgs(args) {
			continue
		}
		out[name] = managedUser{
			handler: argString(args, 2),
			profile: argValue(args, execProfileFlag),
			scope:   argValue(args, execScopeFlag),
		}
	}
	return out
}

// argString returns the string at index i of the exec args, or "" when absent.
func argString(args []any, i int) string {
	if i < 0 || i >= len(args) {
		return ""
	}
	s, _ := args[i].(string)
	return s
}

// argValue returns the string argument immediately following flag in the exec
// args (e.g. the value of --profile), or "" when the flag is absent or last.
func argValue(args []any, flag string) string {
	for i := 0; i < len(args)-1; i++ {
		if s, _ := args[i].(string); s == flag {
			next, _ := args[i+1].(string)
			return next
		}
	}
	return ""
}

// decodeCAData returns the PEM-encoded CA bundle from a kubeconfig cluster
// entry's base64 certificate-authority-data, or "" when absent or malformed.
func decodeCAData(cluster map[string]any) string {
	b64, _ := cluster["certificate-authority-data"].(string)
	if b64 == "" {
		return ""
	}
	pem, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return ""
	}
	return string(pem)
}

// isAuthTokenArgs reports whether the exec args begin with the "auth token"
// subcommand pair that "kube login" bakes into managed entries.
func isAuthTokenArgs(args []any) bool {
	if len(args) < 2 {
		return false
	}
	a0, _ := args[0].(string)
	a1, _ := args[1].(string)
	return a0 == execAuthCmd && a1 == execTokenCmd
}

// clusterServers maps cluster entry names to their API server URLs.
func clusterServers(clusters []any) map[string]string {
	out := make(map[string]string)
	for _, item := range clusters {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		inner, _ := m["cluster"].(map[string]any)
		if name == "" || inner == nil {
			continue
		}
		server, _ := inner["server"].(string)
		out[name] = server
	}
	return out
}

// namedInner returns the inner value map (keyed by key) of the list entry named
// name, or nil when absent.
func namedInner(list []any, name, key string) map[string]any {
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := m["name"].(string); n == name {
			inner, _ := m[key].(map[string]any)
			return inner
		}
	}
	return nil
}
