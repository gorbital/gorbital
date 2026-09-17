package tunnel

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ProcessFile records the cloudflared this orb dev is running, inside the
// app directory (git ignores .orb). It exists only while cloudflared runs:
// orb writes it when the process starts and removes it when the process
// ends, so a file left behind names a process an orb dev that was killed
// never got to stop. It never holds the token, which travels to cloudflared
// in its environment and never on its command line.
const ProcessFile = ".orb/portal/cloudflared.json"

// processRecord identifies a running cloudflared and the orb dev that
// started it. Started and Command come from the operating system, so a PID
// reused by another program fails the comparison and is left alone.
type processRecord struct {
	PID int `json:"pid"`
	// PGID is the process group cloudflared leads (equal to PID): stopping
	// it stops everything cloudflared started.
	PGID int `json:"pgid,omitempty"`
	// Started is the process's start time as the system reports it, and
	// Command its command line: together they tell this cloudflared from a
	// later process that reuses its PID.
	Started string `json:"started,omitempty"`
	Command string `json:"command,omitempty"`
	// Path is the cloudflared program orb ran.
	Path string `json:"path,omitempty"`
	// Mode is the tunnel it runs, for the developer reading the file.
	Mode Mode `json:"mode,omitempty"`
	// OrbPID and OrbStarted identify the orb dev that started it, the same
	// way: a leftover is adopted only when that orb is gone.
	OrbPID     int    `json:"orb_pid"`
	OrbStarted string `json:"orb_started,omitempty"`
	// WrittenAt is when orb started cloudflared.
	WrittenAt time.Time `json:"written_at"`
}

func processFilePath(dir string) string {
	return filepath.Join(dir, filepath.FromSlash(ProcessFile))
}

// readProcessRecord reads ProcessFile; ok is false when it is absent or
// unreadable. Without an app directory there is no record: orb never reads
// or writes one relative to whatever the working directory happens to be,
// because it decides which process to stop.
func readProcessRecord(dir string) (processRecord, bool) {
	if dir == "" {
		return processRecord{}, false
	}
	data, err := os.ReadFile(processFilePath(dir))
	if err != nil {
		return processRecord{}, false
	}
	var r processRecord
	if json.Unmarshal(data, &r) != nil || r.PID <= 0 {
		return processRecord{}, false
	}
	return r, true
}

// writeProcessRecord records a cloudflared that has just started.
func writeProcessRecord(dir string, r processRecord) error {
	if dir == "" {
		return errors.New("no app directory")
	}
	path := processFilePath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// removeProcessRecord deletes ProcessFile when it names pid, or whatever it
// names when pid is 0.
func removeProcessRecord(dir string, pid int) {
	if dir == "" {
		return
	}
	if pid != 0 {
		if r, ok := readProcessRecord(dir); !ok || r.PID != pid {
			return
		}
	}
	_ = os.Remove(processFilePath(dir))
}

// recordProcess describes a cloudflared orb has just started.
func recordProcess(dir string, pid int, path string, mode Mode) error {
	started, command, _ := processIdentity(pid)
	orbStarted, _, _ := processIdentity(os.Getpid())
	return writeProcessRecord(dir, processRecord{
		PID: pid, PGID: pid, Started: started, Command: command, Path: path, Mode: mode,
		OrbPID: os.Getpid(), OrbStarted: orbStarted, WrittenAt: time.Now().UTC(),
	})
}

// stopLeftover stops a cloudflared an earlier orb dev left running, so
// `kill -9` of orb dev doesn't leave a tunnel on the internet (ADR-0086).
// It acts only on a process it can prove is that cloudflared: the record's
// start time and command line must still match what the system reports, and
// the orb dev that started it must be gone. Anything less certain — a PID
// the system describes differently, a PID it can't describe, an orb dev
// still running — is left alone, with a message saying what to run, because
// killing an unrelated process would be worse than the leak.
func (m *Manager) stopLeftover() {
	dir := m.cfg.Dir
	r, ok := readProcessRecord(dir)
	if !ok {
		return
	}
	if !adoptSupported {
		// Nothing here can identify the process; don't guess.
		removeProcessRecord(dir, 0)
		return
	}
	if r.OrbPID == os.Getpid() {
		return // this orb dev's own record
	}
	started, command, alive := processIdentity(r.PID)
	if !alive {
		removeProcessRecord(dir, r.PID) // it ended; the record is stale
		return
	}
	if orbStarted, _, orbAlive := processIdentity(r.OrbPID); orbAlive && (r.OrbStarted == "" || orbStarted == r.OrbStarted) {
		m.cfg.Logf("orb: %s names a cloudflared (pid %d) started by orb dev %d, which is still running; leaving it alone", ProcessFile, r.PID, r.OrbPID)
		return
	}
	switch {
	case r.Started == "" || r.Command == "":
		// Written where the system couldn't describe the process, so
		// there is nothing to compare it with now.
		m.cfg.Logf("orb: %s names a cloudflared (pid %d) left by orb dev %d, but records nothing that identifies it, so orb left it alone. Check it with \"ps -p %d -o command=\" and stop it with \"kill %d\" if it is cloudflared",
			ProcessFile, r.PID, r.OrbPID, r.PID, r.PID)
		return
	case started != r.Started:
		// The likeliest case: the PID belongs to another program now, and
		// the recorded cloudflared is already gone.
		m.cfg.Logf("orb: %s names a cloudflared (pid %d) left by orb dev %d, but process %d started at another time and now runs %s: it isn't that cloudflared, so orb left it alone. If a cloudflared from an earlier run is still going, find it with \"ps -ax | grep cloudflared\" and stop it with \"kill <pid>\"",
			ProcessFile, r.PID, r.OrbPID, r.PID, quoteCommand(command))
		removeProcessRecord(dir, r.PID)
		return
	case command != r.Command:
		m.cfg.Logf("orb: %s names a cloudflared (pid %d) left by orb dev %d, but process %d now runs %s: orb couldn't confirm it and left it alone. Check it with \"ps -p %d -o command=\" and stop it with \"kill %d\" if it is that cloudflared",
			ProcessFile, r.PID, r.OrbPID, r.PID, quoteCommand(command), r.PID, r.PID)
		return
	}
	m.cfg.Logf("orb: orb dev %d was killed and left cloudflared running (pid %d, %s); stopping it before starting a new tunnel", r.OrbPID, r.PID, quoteCommand(r.Command))
	if !m.endLeftover(r) {
		m.cfg.Logf("orb: the leftover cloudflared (pid %d) didn't stop; stop it yourself with \"kill -9 %d\"", r.PID, r.PID)
		return
	}
	removeProcessRecord(dir, r.PID)
	m.cfg.Logf("orb: the leftover cloudflared (pid %d) is stopped", r.PID)
}

// endLeftover sends SIGTERM to the leftover's process group, then SIGKILL
// after StopTimeout, and reports whether it is gone.
func (m *Manager) endLeftover(r processRecord) bool {
	group := r.PGID == r.PID
	for _, hard := range []bool{false, true} {
		if err := signalLeftover(r.PID, group, hard); err != nil {
			return !leftoverAlive(r.PID)
		}
		if waitLeftoverGone(r.PID, m.cfg.StopTimeout) {
			return true
		}
	}
	return !leftoverAlive(r.PID)
}

// waitLeftoverGone polls until pid is gone or timeout passes.
func waitLeftoverGone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !leftoverAlive(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// quoteCommand renders a command line for a message, shortened.
func quoteCommand(command string) string {
	if command == "" {
		return "an unknown program"
	}
	return strconv.Quote(truncate(command, 120))
}
