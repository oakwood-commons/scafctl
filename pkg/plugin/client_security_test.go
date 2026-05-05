// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafePluginEnv_OnlyAllowedKeys(t *testing.T) {
	// Set some dangerous env vars for the test
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("HOME", "/home/test")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	t.Setenv("KUBECONFIG", "/home/test/.kube/config")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-agent.sock")
	t.Setenv("GITHUB_TOKEN", "ghp_secrettoken")
	t.Setenv("TMPDIR", "/tmp")

	env := safePluginEnv()

	// Should include safe keys
	envMap := envToMap(env)
	assert.Equal(t, "/usr/bin:/bin", envMap["PATH"])
	assert.Equal(t, "/home/test", envMap["HOME"])
	assert.Equal(t, "/tmp", envMap["TMPDIR"])

	// Should NOT include sensitive keys
	assert.NotContains(t, envMap, "AWS_ACCESS_KEY_ID")
	assert.NotContains(t, envMap, "AWS_SECRET_ACCESS_KEY")
	assert.NotContains(t, envMap, "KUBECONFIG")
	assert.NotContains(t, envMap, "SSH_AUTH_SOCK")
	assert.NotContains(t, envMap, "GITHUB_TOKEN")
}

func TestSafePluginEnv_AllSafeKeysIncluded(t *testing.T) {
	// Set all safe keys
	for key := range safeEnvKeys {
		t.Setenv(key, "test-value-"+key)
	}

	env := safePluginEnv()
	envMap := envToMap(env)

	for key := range safeEnvKeys {
		assert.Contains(t, envMap, key, "safe key %q should be included", key)
	}
}

func TestPluginCmd_HasSanitizedEnv(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "should-not-leak")
	t.Setenv("PATH", "/usr/bin")

	cmd := pluginCmdSanitized("/fake/plugin/binary")

	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Args[0], "/fake/plugin/binary")

	// Env should be explicitly set (not nil, which means inherit all)
	require.NotNil(t, cmd.Env)

	envMap := envToMap(cmd.Env)
	assert.Contains(t, envMap, "PATH")
	assert.NotContains(t, envMap, "AWS_SECRET_ACCESS_KEY")
}

func TestPluginCmd_UnsanitizedInheritsAll(t *testing.T) {
	cmd := pluginCmd("/fake/plugin/binary")
	require.NotNil(t, cmd)
	// When Env is nil, the child inherits the full parent environment
	assert.Nil(t, cmd.Env)
}

func TestSafeEnvKeys_NoSensitiveDefaults(t *testing.T) {
	// Verify that commonly dangerous keys are not in the safe list
	dangerousKeys := []string{
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
		"AZURE_CLIENT_SECRET",
		"GOOGLE_APPLICATION_CREDENTIALS",
		"KUBECONFIG",
		"SSH_AUTH_SOCK",
		"GITHUB_TOKEN",
		"GH_TOKEN",
		"DOCKER_CONFIG",
		"NPM_TOKEN",
		"DATABASE_URL",
	}

	for _, key := range dangerousKeys {
		assert.False(t, safeEnvKeys[key], "dangerous key %q should not be in safeEnvKeys", key)
	}
}

// envToMap converts a []string env slice to a map for easier assertions.
func envToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		for i := range kv {
			if kv[i] == '=' {
				m[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return m
}
