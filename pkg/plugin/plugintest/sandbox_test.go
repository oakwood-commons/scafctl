// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugintest

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deadPID is far above any real process ID, so no process can hold it.
const deadPID = 2147483646

// shortDir returns a temp directory whose path is short enough for a unix
// socket to be created inside it.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", sandboxPrefix)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// writeState writes a file the fixture would have written for pid.
func writeState(t *testing.T, sb *Sandbox, pid int, ext, body string) {
	t.Helper()
	dir := filepath.Join(sb.Dir, StateDir)
	require.NoError(t, os.MkdirAll(dir, dirMode))
	require.NoError(t, os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+ext), []byte(body), fileMode))
}

// listen creates a unix socket at path that exists until the test ends.
func listen(t *testing.T, path string) string {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	return path
}

// recordingTB captures failures so a test can assert that a helper reports one.
type recordingTB struct {
	testing.TB
	errs []string
}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func TestCall_RoundTrip(t *testing.T) {
	t.Parallel()
	tests := map[string]Call{
		"rpc only":            {RPC: "GetProviders"},
		"rpc with detail":     {RPC: "ExecuteProvider", Detail: "action=echo"},
		"detail with a tab":   {RPC: "ConfigureProvider", Detail: "a\tb"},
		"empty call is empty": {},
	}
	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, call, ParseCall(FormatCall(call)))
		})
	}
}

func TestConfig_MaxLifetime(t *testing.T) {
	t.Parallel()
	assert.Equal(t, DefaultMaxLifetime, Config{}.MaxLifetime())
	assert.Equal(t, 250*time.Millisecond, Config{MaxLifetimeMillis: 250}.MaxLifetime())
}

func TestSandbox_ProcessAccounting(t *testing.T) {
	sb := &Sandbox{Dir: shortDir(t)}
	assert.Empty(t, sb.PIDs(), "no state directory means no processes")
	assert.True(t, sb.Resources().Empty())

	self := os.Getpid()
	writeState(t, sb, self, ".pid", "")
	writeState(t, sb, deadPID, ".pid", "")
	writeState(t, sb, deadPID, ".calls", "")
	require.NoError(t, os.WriteFile(filepath.Join(sb.Dir, StateDir, "not-a-pid.pid"), nil, fileMode))

	assert.Equal(t, []int{self, deadPID}, sb.PIDs(), "only <pid>.pid files count, in ascending order")
	assert.Equal(t, []int{self}, sb.LivePIDs(), "the process that does not exist is not live")

	r := sb.Resources()
	assert.False(t, r.Empty())
	assert.Equal(t, []int{self}, r.Processes)
	assert.Contains(t, r.String(), strconv.Itoa(self))
}

func TestSandbox_CallsAndExitReason(t *testing.T) {
	sb := &Sandbox{Dir: shortDir(t)}
	assert.Nil(t, sb.Calls(42), "a process with no call log served no calls")
	assert.Empty(t, sb.CallNames(42))
	assert.Empty(t, sb.ExitReason(42), "a process with no exit record has not exited gracefully")

	writeState(t, sb, 42, ".calls", FormatCall(Call{RPC: "GetProviders"})+"\n"+FormatCall(Call{RPC: "ExecuteProvider", Detail: "action=echo"})+"\n")
	writeState(t, sb, 42, ".exit", ExitGraceful+"\n")

	assert.Equal(t, []Call{{RPC: "GetProviders"}, {RPC: "ExecuteProvider", Detail: "action=echo"}}, sb.Calls(42))
	assert.Equal(t, []string{"GetProviders", "ExecuteProvider"}, sb.CallNames(42))
	assert.Equal(t, ExitGraceful, sb.ExitReason(42))
}

func TestSandbox_Sockets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("go-plugin uses TCP on Windows; there are no unix sockets to account for")
	}
	sb := &Sandbox{Dir: shortDir(t)}
	assert.Empty(t, sb.Sockets())

	direct := listen(t, filepath.Join(sb.Dir, "plugin1"))
	nestedDir := filepath.Join(sb.Dir, "plugin-dir2")
	require.NoError(t, os.Mkdir(nestedDir, dirMode))
	nested := listen(t, filepath.Join(nestedDir, "plugin3"))
	require.NoError(t, os.WriteFile(filepath.Join(sb.Dir, "plugin-regular-file"), nil, fileMode))
	writeState(t, sb, 1, ".pid", "")

	assert.ElementsMatch(t, []string{direct, nested}, sb.Sockets(), "sockets directly in the sandbox and one directory down; regular files and the state directory are ignored")
}

func TestSandbox_Dirs(t *testing.T) {
	sb := &Sandbox{Dir: shortDir(t)}
	assert.Empty(t, sb.Dirs())

	owned := filepath.Join(sb.Dir, HostSocketDirPrefix+"123")
	require.NoError(t, os.Mkdir(owned, dirMode))
	require.NoError(t, os.Mkdir(filepath.Join(sb.Dir, "unrelated"), dirMode))
	require.NoError(t, os.WriteFile(filepath.Join(sb.Dir, HostSocketDirPrefix+"-file"), nil, fileMode))

	assert.Equal(t, []string{owned}, sb.Dirs(), "only directories with the host's prefix count; a test's own temp directories and plain files do not")
	r := sb.Resources()
	assert.False(t, r.Empty(), "a leftover directory is a leak even when it is empty")
	assert.Contains(t, r.String(), HostSocketDirPrefix+"123")
}

func TestNewDetachedSandbox(t *testing.T) {
	t.Parallel()
	before := os.Getenv("TMPDIR")
	sb := NewDetachedSandbox(t, Config{})

	assert.Equal(t, before, os.Getenv("TMPDIR"), "the test process keeps its own temp directory")
	assert.Equal(t, sb.Dir, sb.Env()["TMPDIR"], "the host subprocess is pointed at the sandbox instead")
	_, err := os.Stat(filepath.Join(sb.Dir, ControlFile))
	assert.NoError(t, err, "the control file is written")

	// t.Setenv panics in a parallel test, so this passing shows a detached
	// sandbox can be used by tests that run in parallel.
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			other := NewDetachedSandbox(t, Config{})
			assert.NotEqual(t, sb.Dir, other.Dir)
		})
	}
}

func TestSandbox_RequireClean(t *testing.T) {
	t.Run("an empty sandbox is clean", func(t *testing.T) {
		sb := &Sandbox{Dir: shortDir(t)}
		rec := &recordingTB{TB: t}
		sb.RequireClean(rec, time.Second)
		assert.Empty(t, rec.errs)
	})

	t.Run("a running process is reported with its ID", func(t *testing.T) {
		sb := &Sandbox{Dir: shortDir(t)}
		writeState(t, sb, os.Getpid(), ".pid", "")
		rec := &recordingTB{TB: t}
		sb.RequireClean(rec, 80*time.Millisecond)
		require.Len(t, rec.errs, 1)
		assert.Contains(t, rec.errs[0], strconv.Itoa(os.Getpid()))
	})

	t.Run("a leftover socket is reported", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("no unix sockets on Windows")
		}
		sb := &Sandbox{Dir: shortDir(t)}
		path := listen(t, filepath.Join(sb.Dir, "plugin1"))
		rec := &recordingTB{TB: t}
		sb.RequireClean(rec, 80*time.Millisecond)
		require.Len(t, rec.errs, 1)
		assert.Contains(t, rec.errs[0], path)
	})

	t.Run("waits for a process that is on its way out", func(t *testing.T) {
		sleep, err := exec.LookPath("sleep")
		if err != nil {
			t.Skip("sleep is not available")
		}
		cmd := exec.CommandContext(t.Context(), sleep, "0.3")
		require.NoError(t, cmd.Start())
		go func() { _ = cmd.Wait() }()

		sb := &Sandbox{Dir: shortDir(t)}
		writeState(t, sb, cmd.Process.Pid, ".pid", "")
		rec := &recordingTB{TB: t}
		sb.RequireClean(rec, 10*time.Second)
		assert.Empty(t, rec.errs, "the process exits well inside the allowed time")
	})
}

func TestSandbox_WaitForExit(t *testing.T) {
	sb := &Sandbox{Dir: shortDir(t)}
	assert.True(t, sb.WaitForExit(deadPID, time.Second), "a process that does not exist has exited")
	assert.False(t, sb.WaitForExit(os.Getpid(), 60*time.Millisecond), "a running process has not")
}

// The cleanup kill is a safety net for leaked fixture processes. It must never
// hit another process that happens to be recorded, for instance because the
// operating system reused a process ID, even one that shares the fixture's file
// name: only the exact binary this sandbox started counts.
func TestSandbox_ReapKillsOnlyFixtureProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process identity checks are not implemented on Windows")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep is not available")
	}
	binary := filepath.Join(shortDir(t), fixtureBinaryName)

	// startAs runs sleep with argv[0] set to name, which is what ps reports.
	startAs := func(t *testing.T, name string) (cmd *exec.Cmd, exited <-chan struct{}) {
		t.Helper()
		cmd = &exec.Cmd{Path: sleep, Args: []string{name, "30"}}
		require.NoError(t, cmd.Start())
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		return cmd, done
	}

	unrelated := exec.CommandContext(t.Context(), sleep, "30")
	require.NoError(t, unrelated.Start())
	t.Cleanup(func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() })

	// Runs sleep, but its command line has only the fixture's file name, as
	// another program of the same name in another directory would.
	lookalike, _ := startAs(t, fixtureBinaryName)

	// Runs sleep, but its command line names the exact fixture binary.
	fixture, fixtureExited := startAs(t, binary)

	sb := &Sandbox{Binary: binary, Dir: shortDir(t)}
	for _, cmd := range []*exec.Cmd{unrelated, lookalike, fixture} {
		writeState(t, sb, cmd.Process.Pid, ".pid", "")
	}

	sb.reap(t)

	select {
	case <-fixtureExited:
	case <-time.After(10 * time.Second):
		t.Fatal("a process running the fixture binary was not killed")
	}
	assert.True(t, processAlive(unrelated.Process.Pid), "an unrelated process must be left alone")
	assert.True(t, processAlive(lookalike.Process.Pid), "a process that only shares the file name must be left alone")
}
