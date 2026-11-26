// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugintest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	// sandboxPrefix is deliberately short. The plugin's unix socket is created
	// inside the sandbox, and socket paths are limited to roughly 104 bytes on
	// macOS, where the temp directory is already long.
	sandboxPrefix = "plt-"

	pollInterval = 20 * time.Millisecond
	reapGrace    = 2 * time.Second

	dirMode  = 0o750
	fileMode = 0o600
)

// Sandbox is a per-test directory that holds everything the fixture plugin
// reads and writes. It works because the sandbox directory becomes the
// process temp directory for the duration of the test: the fixture finds its
// control file there, records its state there, and go-plugin creates its unix
// socket (or the host creates a directory for it) there. A test can therefore
// tell exactly which processes, sockets and directories it caused, regardless
// of anything else running on the machine.
//
// NewSandbox changes TMPDIR with Setenv for an in-process host, so a test that
// uses it cannot run in parallel. NewDetachedSandbox leaves the test process
// alone, for a host that runs as a subprocess and is given Env instead. In both
// cases the variable is inherited by plugin processes whether or not the host
// sanitizes their environment, since TMPDIR is one of the variables that
// survives sanitizing.
type Sandbox struct {
	// Binary is the path of the fixture plugin executable.
	Binary string

	// Dir is the sandbox directory.
	Dir string
}

// HostSocketDirPrefix is the name prefix of the directories the plugin host
// creates to own its plugins' sockets. It must match the host's prefix; a
// mismatch makes the lifecycle tests fail rather than pass quietly.
const HostSocketDirPrefix = "plugin-dir"

// NewSandbox builds the fixture if needed, creates the sandbox, writes cfg as
// the fixture's control file, and points the process temp directory at the
// sandbox. Cleanup kills any fixture process the test left running, so a
// failing leak test does not leave orphans on the developer's machine, and then
// removes the sandbox.
func NewSandbox(t testing.TB, cfg Config) *Sandbox {
	t.Helper()
	s := NewDetachedSandbox(t, cfg)
	for key, value := range s.Env() {
		t.Setenv(key, value)
	}
	return s
}

// NewDetachedSandbox is NewSandbox without changing this process's
// environment. Use it when the plugin host is a subprocess, such as the CLI
// binary, and pass Env to that subprocess. Because it never touches TMPDIR it
// is safe in parallel tests.
func NewDetachedSandbox(t testing.TB, cfg Config) *Sandbox {
	t.Helper()
	binary := BuildFixture(t)

	dir, err := os.MkdirTemp("", sandboxPrefix)
	if err != nil {
		t.Fatalf("create plugin sandbox: %v", err)
	}
	s := &Sandbox{Binary: binary, Dir: dir}
	s.SetConfig(t, cfg)

	t.Cleanup(func() {
		s.reap(t)
		_ = os.RemoveAll(dir)
	})
	return s
}

// Env returns the environment variables that point a plugin host at this
// sandbox: the process temp directory.
func (s *Sandbox) Env() map[string]string {
	env := map[string]string{"TMPDIR": s.Dir}
	if runtime.GOOS == "windows" {
		env["TMP"] = s.Dir
		env["TEMP"] = s.Dir
	}
	return env
}

// SetConfig replaces the fixture's control file. It takes effect for plugin
// processes started afterwards; a running process has already read it.
func (s *Sandbox) SetConfig(t testing.TB, cfg Config) {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("encode fixture config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, ControlFile), data, fileMode); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}
}

// PIDs returns the process ID of every fixture process that has ever started
// in this sandbox, running or not, in ascending order.
func (s *Sandbox) PIDs() []int {
	entries, err := os.ReadDir(filepath.Join(s.Dir, StateDir))
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".pid")
		if !ok {
			continue
		}
		if pid, err := strconv.Atoi(name); err == nil {
			pids = append(pids, pid)
		}
	}
	slices.Sort(pids)
	return pids
}

// LivePIDs returns the subset of PIDs that are still running.
func (s *Sandbox) LivePIDs() []int {
	var live []int
	for _, pid := range s.PIDs() {
		if processAlive(pid) {
			live = append(live, pid)
		}
	}
	return live
}

// Sockets returns the unix sockets that exist in the sandbox, either directly
// in it or one directory down (go-plugin can be told to use a per-client
// directory). A socket outlives its plugin only when the plugin was killed
// before it could close its listener.
func (s *Sandbox) Sockets() []string {
	var found []string
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		path := filepath.Join(s.Dir, e.Name())
		switch {
		case e.Type()&os.ModeSocket != 0:
			found = append(found, path)
		case e.IsDir() && e.Name() != StateDir:
			sub, err := os.ReadDir(path)
			if err != nil {
				continue
			}
			for _, se := range sub {
				if se.Type()&os.ModeSocket != 0 {
					found = append(found, filepath.Join(path, se.Name()))
				}
			}
		}
	}
	slices.Sort(found)
	return found
}

// Dirs returns the directories the plugin host created in the sandbox to own
// its plugins' sockets. A host that cleans up after itself leaves none once its
// plugins are stopped, so a leftover one is a leak even when it is empty.
func (s *Sandbox) Dirs() []string {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil
	}
	var found []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), HostSocketDirPrefix) {
			found = append(found, filepath.Join(s.Dir, e.Name()))
		}
	}
	slices.Sort(found)
	return found
}

// Resources is a point-in-time view of what the fixture currently has running
// or on disk. A test that expects no leaks asserts it is empty; a test that
// runs many invocations compares it with a baseline.
type Resources struct {
	// Processes are the IDs of fixture processes that are still running.
	Processes []int

	// Sockets are the paths of unix sockets that still exist.
	Sockets []string

	// Dirs are the paths of host-created socket directories that still exist.
	Dirs []string
}

// Empty reports whether nothing is running and nothing remains on disk.
func (r Resources) Empty() bool {
	return len(r.Processes) == 0 && len(r.Sockets) == 0 && len(r.Dirs) == 0
}

func (r Resources) String() string {
	return fmt.Sprintf("processes=%v sockets=%v dirs=%v", r.Processes, r.Sockets, r.Dirs)
}

// Resources returns the current Resources of the sandbox.
func (s *Sandbox) Resources() Resources {
	return Resources{Processes: s.LivePIDs(), Sockets: s.Sockets(), Dirs: s.Dirs()}
}

// RequireClean fails the test unless every fixture process has exited and every
// socket and host-created socket directory has been removed within the given
// time. It polls rather than checking once because the operating system takes
// a moment to finish tearing down a process that has just been killed.
func (s *Sandbox) RequireClean(t testing.TB, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		r := s.Resources()
		if r.Empty() {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("plugin resources still present after %s: %s", within, r)
			return
		}
		time.Sleep(pollInterval)
	}
}

// WaitForExit waits for the process to exit and reports whether it did.
func (s *Sandbox) WaitForExit(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(pollInterval)
	}
	return true
}

// Calls returns the RPCs the process served, in the order it served them.
func (s *Sandbox) Calls(pid int) []Call {
	data, err := os.ReadFile(filepath.Join(s.Dir, StateDir, strconv.Itoa(pid)+".calls"))
	if err != nil {
		return nil
	}
	var calls []Call
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line != "" {
			calls = append(calls, ParseCall(line))
		}
	}
	return calls
}

// CallNames returns just the RPC names from Calls.
func (s *Sandbox) CallNames(pid int) []string {
	calls := s.Calls(pid)
	names := make([]string, 0, len(calls))
	for _, c := range calls {
		names = append(names, c.RPC)
	}
	return names
}

// ExitReason returns why the process exited, as recorded by the process
// itself: ExitGraceful or ExitWatchdog. It is empty for a process that is
// still running and for one that was killed or crashed, because such a process
// cannot record anything. Distinguishing "shut down when asked" from "had to be
// killed" is the purpose of the record.
func (s *Sandbox) ExitReason(pid int) string {
	data, err := os.ReadFile(filepath.Join(s.Dir, StateDir, strconv.Itoa(pid)+".exit"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// reap force-kills any fixture process still running at the end of a test.
func (s *Sandbox) reap(t testing.TB) {
	for _, pid := range s.LivePIDs() {
		if !processIs(pid, s.Binary) {
			continue
		}
		t.Logf("plugintest: killing fixture process %d left running by the test", pid)
		killProcess(pid)
		s.WaitForExit(pid, reapGrace)
	}
}
