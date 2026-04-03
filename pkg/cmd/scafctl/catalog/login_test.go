// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandLogin(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLogin(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "login <registry>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandLogin_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLogin(cliParams, ioStreams, "scafctl/catalog")

	flagTests := []struct {
		name         string
		defaultValue string
	}{
		{"auth-provider", ""},
		{"scope", ""},
		{"username", ""},
		{"password-stdin", "false"},
		{"password-env", ""},
		{"write-registry-auth", "false"},
	}

	for _, tt := range flagTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := cmd.Flags().Lookup(tt.name)
			require.NotNil(t, f, "flag %q should exist", tt.name)
			assert.Equal(t, tt.defaultValue, f.DefValue)
		})
	}
}

func TestCommandLogin_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLogin(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg(s)")
}

func TestReadPassword_BothStdinAndEnv(t *testing.T) {
	t.Parallel()

	opts := &LoginOptions{
		PasswordStdin: true,
		PasswordEnv:   "SOME_VAR",
	}
	_, err := readPassword(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot use both")
}

func TestReadPassword_NeitherStdinNorEnv(t *testing.T) {
	t.Parallel()

	opts := &LoginOptions{
		PasswordStdin: false,
		PasswordEnv:   "",
	}
	_, err := readPassword(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--password-stdin or --password-env is required")
}

func TestReadPassword_EmptyEnvVar(t *testing.T) {
	t.Parallel()

	opts := &LoginOptions{
		PasswordEnv: "SCAFCTL_TEST_EMPTY_VAR_" + t.Name(),
	}
	_, err := readPassword(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is empty or not set")
}

func TestReadPassword_FromEnvVar(t *testing.T) {
	envKey := "SCAFCTL_TEST_PASSWORD_" + t.Name()
	t.Setenv(envKey, "my-secret-token")

	opts := &LoginOptions{
		PasswordEnv: envKey,
	}
	password, err := readPassword(opts)
	require.NoError(t, err)
	assert.Equal(t, "my-secret-token", password)
}

func BenchmarkCommandLogin(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for b.Loop() {
		_ = CommandLogin(cliParams, ioStreams, "scafctl/catalog")
	}
}
