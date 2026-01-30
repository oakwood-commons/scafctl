package snapshot

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandSnapshot(t *testing.T) {
	cliParams := &settings.Run{}
	ioStreams := terminal.IOStreams{}

	cmd := CommandSnapshot(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "snapshot", cmd.Use)
	assert.Equal(t, "Manage resolver execution snapshots", cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)

	// Verify subcommands are added
	subcommands := cmd.Commands()
	assert.Len(t, subcommands, 3, "should have 3 subcommands")

	foundSave := false
	foundShow := false
	foundDiff := false

	for _, sub := range subcommands {
		switch sub.Name() {
		case "save":
			foundSave = true
		case "show":
			foundShow = true
		case "diff":
			foundDiff = true
		}
	}

	assert.True(t, foundSave, "save subcommand should be present")
	assert.True(t, foundShow, "show subcommand should be present")
	assert.True(t, foundDiff, "diff subcommand should be present")
}

func TestCommandSnapshot_ExampleContainsBinaryName(t *testing.T) {
	cliParams := &settings.Run{}
	ioStreams := terminal.IOStreams{}
	binaryName := "testcli"

	cmd := CommandSnapshot(cliParams, ioStreams, binaryName)

	assert.Contains(t, cmd.Example, binaryName, "example should contain binary name")
}
