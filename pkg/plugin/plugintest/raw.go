// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugintest

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
)

const dialStartTimeout = 10 * time.Second

// RawClient is a wire-level client for the fixture. It speaks the provider gRPC
// service directly instead of going through the host's plugin client, so a test
// sees exactly the messages the plugin sent: unknown fields, protocol versions,
// and stream endings that the host wrapper would normalize away. It is the tool
// for contract tests; use the host client to test host behavior.
type RawClient struct {
	proto.PluginServiceClient

	client *goplugin.Client
}

// Dial starts the fixture with the standard provider handshake and returns a
// RawClient. The plugin is killed when the test ends; Kill stops it earlier.
func (s *Sandbox) Dial(t testing.TB) *RawClient {
	t.Helper()
	hc := sdkplugin.HandshakeConfig()
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: goplugin.HandshakeConfig{
			ProtocolVersion:  hc.ProtocolVersion,
			MagicCookieKey:   hc.MagicCookieKey,
			MagicCookieValue: hc.MagicCookieValue,
		},
		Plugins:          goplugin.PluginSet{sdkplugin.PluginName: &sdkplugin.GRPCPlugin{}},
		Cmd:              exec.CommandContext(context.Background(), s.Binary), //nolint:gosec // the fixture binary this package built
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Logger:           hclog.NewNullLogger(),
		StartTimeout:     dialStartTimeout,
	})
	t.Cleanup(client.Kill)

	rpc, err := client.Client()
	if err != nil {
		t.Fatalf("start fixture plugin: %v", err)
	}
	raw, err := rpc.Dispense(sdkplugin.PluginName)
	if err != nil {
		t.Fatalf("dispense fixture provider: %v", err)
	}
	stub, ok := raw.(*sdkplugin.GRPCClient)
	if !ok {
		t.Fatalf("dispensed %T, want *plugin.GRPCClient", raw)
	}
	return &RawClient{PluginServiceClient: stub.Client(), client: client}
}

// PID returns the process ID of the plugin, or zero if it is not running.
func (c *RawClient) PID() int {
	if cfg := c.client.ReattachConfig(); cfg != nil {
		return cfg.Pid
	}
	return 0
}

// Kill stops the plugin the way the host does: a shutdown request, then a
// forced kill if the plugin does not exit promptly. It is safe to call twice.
func (c *RawClient) Kill() {
	c.client.Kill()
}
