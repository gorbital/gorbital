//go:build unix

package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startFake runs the fake cloudflared as a process group of its own, the
// way orb dev starts the real one, and reaps it when the test ends.
func startFake(t *testing.T, behaviour string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "FAKE_CLOUDFLARED="+behaviour, "FAKE_DIR=")
	ownProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Reap it as soon as it dies: a zombie still answers kill(pid, 0), and
	// the leftover code waits for the process to be gone.
	waited := make(chan struct{})
	go func() { defer close(waited); _ = cmd.Wait() }()
	t.Cleanup(func() {
		_ = signalLeftover(cmd.Process.Pid, true, true)
		select {
		case <-waited:
		case <-time.After(5 * time.Second):
			t.Errorf("the fake cloudflared %d didn't exit", cmd.Process.Pid)
		}
	})
	return cmd
}

// deadPID returns the PID of a process that has exited, for a record left
// by an orb dev that is gone.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := startFake(t, "exit")
	pid := cmd.Process.Pid
	deadline := time.Now().Add(5 * time.Second)
	for leftoverAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("the fake %d didn't exit", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return pid
}

// longAgo is a start time no live process has, so a record carrying it
// never matches.
const longAgo = "Thu Jan 1 00:00:00 1970"

func (h *harness) messages() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.Join(h.logs, "\n")
}

func (h *harness) record(t *testing.T) processRecord {
	t.Helper()
	r, ok := readProcessRecord(h.dir)
	if !ok {
		t.Fatalf("%s is missing or unreadable", ProcessFile)
	}
	return r
}

func TestProcessFileRecordsAndForgetsCloudflared(t *testing.T) {
	h := newHarness(t, "quick")
	if err := h.m.Start(context.Background(), StartOptions{Mode: ModeQuick}); err != nil {
		t.Fatal(err)
	}
	s := h.waitFor("connected", func(s Status) bool { return s.State == StateConnected })
	r := h.record(t)
	exe, _ := os.Executable()
	switch {
	case r.PID != s.PID || r.PGID != r.PID:
		t.Errorf("record %+v, want pid %d as its own group leader", r, s.PID)
	case r.OrbPID != os.Getpid() || r.OrbStarted == "":
		t.Errorf("record %+v, want orb %d with its start time", r, os.Getpid())
	case r.Started == "" || !strings.Contains(r.Command, exe) || r.Path != exe || r.Mode != ModeQuick:
		t.Errorf("record %+v, want the start time and command line of %s", r, exe)
	case r.WrittenAt.IsZero():
		t.Errorf("record %+v has no time", r)
	}
	// The record says what the system says about the process.
	started, command, alive := processIdentity(s.PID)
	if !alive || started != r.Started || command != r.Command {
		t.Errorf("processIdentity(%d) = %q, %q, %v; record has %q, %q", s.PID, started, command, alive, r.Started, r.Command)
	}

	if err := h.m.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(processFilePath(h.dir)); !os.IsNotExist(err) {
		t.Errorf("%s survived a clean stop: %v", ProcessFile, err)
	}
}

func TestProcessFileIsRemovedWhenCloudflaredExitsByItself(t *testing.T) {
	h := newHarness(t, "crash")
	if err := h.m.Start(context.Background(), StartOptions{Mode: ModeQuick}); err != nil {
		t.Fatal(err)
	}
	h.waitFor("failed", func(s Status) bool { return s.State == StateFailed })
	if _, err := os.Stat(processFilePath(h.dir)); !os.IsNotExist(err) {
		t.Errorf("%s survived cloudflared's own exit: %v", ProcessFile, err)
	}
}

func TestStopsALeftoverCloudflaredFromAKilledOrb(t *testing.T) {
	h := newHarness(t, "quick")
	leftover := startFake(t, "silent")
	pid := leftover.Process.Pid
	started, command, alive := processIdentity(pid)
	if !alive {
		t.Fatalf("the leftover %d isn't running", pid)
	}
	writeRecord(t, h.dir, processRecord{
		PID: pid, PGID: pid, Started: started, Command: command, Path: command,
		OrbPID: deadPID(t), OrbStarted: longAgo, WrittenAt: time.Now().UTC(),
	})

	if err := h.m.Start(context.Background(), StartOptions{Mode: ModeQuick}); err != nil {
		t.Fatal(err)
	}
	s := h.waitFor("connected", func(s Status) bool { return s.State == StateConnected })
	assertGone(t, []int{pid})
	messages := h.messages()
	for _, want := range []string{"left cloudflared running", fmt.Sprintf("pid %d", pid), "is stopped"} {
		if !strings.Contains(messages, want) {
			t.Errorf("messages lack %q:\n%s", want, messages)
		}
	}
	// The record now names the new cloudflared, not the leftover.
	if r := h.record(t); r.PID != s.PID || r.PID == pid {
		t.Errorf("record %+v, want the new cloudflared %d", r, s.PID)
	}
}

func TestStaleProcessFileIsRemoved(t *testing.T) {
	h := newHarness(t, "quick")
	pid := deadPID(t)
	writeRecord(t, h.dir, processRecord{
		PID: pid, PGID: pid, Started: longAgo, Command: "cloudflared tunnel --url http://127.0.0.1:18090",
		OrbPID: deadPID(t), OrbStarted: longAgo, WrittenAt: time.Now().UTC(),
	})
	h.m.stopLeftover()
	if _, err := os.Stat(processFilePath(h.dir)); !os.IsNotExist(err) {
		t.Errorf("a record naming the dead process %d survived: %v", pid, err)
	}
	if got := h.messages(); got != "" {
		t.Errorf("a stale record said something:\n%s", got)
	}
}

func TestALeftoverThatFailsTheIdentityCheckIsLeftAlone(t *testing.T) {
	tests := []struct {
		name   string
		record func(pid int, started, command string) processRecord
		want   func(pid int) []string
	}{
		{
			// The likely case: the PID belongs to another program now.
			name: "another process reused the PID",
			record: func(pid int, _, _ string) processRecord {
				return processRecord{PID: pid, PGID: pid, Started: longAgo, Command: "cloudflared tunnel --url http://127.0.0.1:18090"}
			},
			want: func(int) []string {
				return []string{"left it alone", "ps -ax | grep cloudflared", "kill <pid>"}
			},
		},
		{
			// No identity was recorded at all: nothing to compare with.
			name: "the record identifies nothing",
			record: func(pid int, _, _ string) processRecord {
				return processRecord{PID: pid, PGID: pid}
			},
			want: func(pid int) []string {
				return []string{"records nothing that identifies it", fmt.Sprintf("kill %d", pid), fmt.Sprintf("ps -p %d -o command=", pid)}
			},
		},
		{
			// The same process, describing itself differently: orb can't
			// be sure, so it says what to check and what to run.
			name: "the command line doesn't match",
			record: func(pid int, started, _ string) processRecord {
				return processRecord{PID: pid, PGID: pid, Started: started, Command: "cloudflared tunnel --url http://127.0.0.1:18090"}
			},
			want: func(pid int) []string {
				return []string{"couldn't confirm", fmt.Sprintf("kill %d", pid), fmt.Sprintf("ps -p %d -o command=", pid)}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, "quick")
			leftover := startFake(t, "silent")
			pid := leftover.Process.Pid
			started, command, alive := processIdentity(pid)
			if !alive {
				t.Fatalf("the leftover %d isn't running", pid)
			}
			r := tt.record(pid, started, command)
			r.OrbPID, r.OrbStarted, r.WrittenAt = deadPID(t), longAgo, time.Now().UTC()
			writeRecord(t, h.dir, r)

			h.m.stopLeftover()
			if !leftoverAlive(pid) {
				t.Fatalf("orb stopped process %d although it couldn't identify it", pid)
			}
			messages := h.messages()
			for _, want := range tt.want(pid) {
				if !strings.Contains(messages, want) {
					t.Errorf("messages lack %q:\n%s", want, messages)
				}
			}
		})
	}
}

// TestOutputPipeEndsCloudflaredWhenOrbIsKilled records the backstop orb
// dev's arrangement gives on macOS, where there is no parent-death signal:
// cloudflared's stdout and stderr are a pipe only orb holds the read end
// of, so once orb is gone the next line cloudflared writes gets EPIPE, and
// a Go program's runtime turns that into SIGPIPE on file descriptor 1 or 2
// and exits. It is a backstop, not a guarantee: a cloudflared that writes
// nothing keeps running until the next orb dev stops it.
func TestOutputPipeEndsCloudflaredWhenOrbIsKilled(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "FAKE_CLOUDFLARED=chatty", "FAKE_DIR=")
	cmd.Stdout, cmd.Stderr = writer, writer
	ownProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	buf := make([]byte, 64)
	if _, err := reader.Read(buf); err != nil {
		t.Fatalf("the fake wrote nothing: %v", err)
	}
	// Everything orb held closes when orb is killed, the read end with it.
	_ = reader.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var status syscall.WaitStatus
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			status, _ = exit.Sys().(syscall.WaitStatus)
		}
		if !status.Signaled() || status.Signal() != syscall.SIGPIPE {
			t.Errorf("the fake exited with %v, want SIGPIPE", err)
		}
	case <-time.After(10 * time.Second):
		_ = signalLeftover(cmd.Process.Pid, true, true)
		t.Error("the fake outlived the pipe it writes to")
	}
}

func writeRecord(t *testing.T, dir string, r processRecord) {
	t.Helper()
	if err := writeProcessRecord(dir, r); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(ProcessFile)))
	if err != nil {
		t.Fatal(err)
	}
	var back processRecord
	if json.Unmarshal(data, &back) != nil || back.PID != r.PID {
		t.Fatalf("%s reads back as %q", ProcessFile, data)
	}
}
