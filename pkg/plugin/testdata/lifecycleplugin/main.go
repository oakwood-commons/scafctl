// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Command lifecycleplugin is a configurable provider plugin used only by tests.
// It implements the provider gRPC service directly, rather than through the
// SDK's typed server, so that it can misbehave at the wire level: emit
// undecodable output, unknown protobuf fields, truncated streams, or a
// different protocol version.
//
// It is controlled in two ways, both described in package plugintest:
//
//   - process-level behavior comes from the JSON control file found in the
//     process temp directory (plugintest.Config), because it must be known
//     before the handshake;
//   - per-call behavior comes from the "action" key of the provider input.
//
// It records a PID file, a call log and an exit record under the process temp
// directory so tests can account for every process and socket it leaves
// behind. A watchdog ends the process after a bounded lifetime so a test that
// dies early cannot leave a plugin running.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	goplugin "github.com/hashicorp/go-plugin"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/plugin/plugintest"
	"google.golang.org/grpc"
)

// maxRecvMsgSize matches the receive limit the SDK sets on its plugin server,
// so large-message behavior is the same as for a real plugin.
const maxRecvMsgSize = 64 << 20

const (
	exitUsage       = 2
	defaultExitCode = 1
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lifecycleplugin: %v\n", err)
		os.Exit(exitUsage)
	}

	dir := stateDir()
	if err := os.MkdirAll(dir, stateDirMode); err != nil {
		fmt.Fprintf(os.Stderr, "lifecycleplugin: create state dir: %v\n", err)
		os.Exit(exitUsage)
	}
	pid := os.Getpid()
	writePIDFile(dir, pid)
	startWatchdog(dir, pid, cfg.MaxLifetime())

	switch cfg.Startup {
	case plugintest.StartupExit:
		code := cfg.ExitCode
		if code == 0 {
			code = defaultExitCode
		}
		os.Exit(code)
	case plugintest.StartupHang:
		select {}
	case plugintest.StartupNormal, plugintest.StartupBadCookie:
	}

	fx := &fixture{
		cfg: cfg,
		log: &callLog{path: filepath.Join(dir, strconv.Itoa(pid)+".calls")},
	}
	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: handshake(cfg),
		Plugins:         goplugin.PluginSet{sdkplugin.PluginName: &grpcPlugin{fx: fx}},
		GRPCServer:      newGRPCServer,
	})

	// Serve returns only after the host asked the server to shut down, so
	// reaching this line is what a graceful exit means.
	recordExit(dir, pid, plugintest.ExitGraceful)
}

// loadConfig reads the control file. A missing file is the zero Config.
func loadConfig() (plugintest.Config, error) {
	var cfg plugintest.Config
	data, err := os.ReadFile(filepath.Join(os.TempDir(), plugintest.ControlFile))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read control file: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("decode control file: %w", err)
	}
	return cfg, nil
}

// handshake is the SDK handshake with the overrides from cfg applied.
func handshake(cfg plugintest.Config) goplugin.HandshakeConfig {
	hc := sdkplugin.HandshakeConfig()
	out := goplugin.HandshakeConfig{
		ProtocolVersion:  hc.ProtocolVersion,
		MagicCookieKey:   hc.MagicCookieKey,
		MagicCookieValue: hc.MagicCookieValue,
	}
	if cfg.HandshakeVersion != 0 {
		out.ProtocolVersion = cfg.HandshakeVersion
	}
	if cfg.Startup == plugintest.StartupBadCookie {
		out.MagicCookieValue += "-mismatch"
	}
	return out
}

func newGRPCServer(opts []grpc.ServerOption) *grpc.Server {
	return grpc.NewServer(append([]grpc.ServerOption{grpc.MaxRecvMsgSize(maxRecvMsgSize)}, opts...)...)
}

// grpcPlugin registers the fixture as the provider service.
type grpcPlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	fx *fixture
}

// GRPCServer registers the fixture as the provider service. It must return an
// error to satisfy goplugin.GRPCPlugin, though registration cannot fail.
//
//nolint:unparam // required by the go-plugin GRPCPlugin interface
func (p *grpcPlugin) GRPCServer(broker *goplugin.GRPCBroker, s *grpc.Server) error {
	p.fx.broker = broker
	proto.RegisterPluginServiceServer(s, p.fx)
	return nil
}

func (p *grpcPlugin) GRPCClient(context.Context, *goplugin.GRPCBroker, *grpc.ClientConn) (any, error) {
	return nil, errors.New("the lifecycle fixture is a server only")
}
