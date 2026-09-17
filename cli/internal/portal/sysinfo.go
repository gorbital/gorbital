package portal

import (
	"context"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

// SystemInfo is what the machine and the app process are doing
// (ADR-0073), sampled by [SystemSampler].
type SystemInfo struct {
	SampledAt time.Time  `json:"sampled_at"`
	Host      HostInfo   `json:"host"`
	App       *ProcInfo  `json:"app,omitempty"`
	Orb       ProcInfo   `json:"orb"`
	Disk      *DiskInfo  `json:"disk,omitempty"`
	Memory    MemoryInfo `json:"memory"`
}

// HostInfo is the machine.
type HostInfo struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Cores    int    `json:"cores"`
	Hostname string `json:"hostname"`
	// CPUPercent is the whole machine's use over the last sample interval.
	CPUPercent float64 `json:"cpu_percent"`
	// Load averages; zero where the OS has none.
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
	Uptime uint64  `json:"uptime_seconds"`
}

// MemoryInfo is the machine's memory in bytes.
type MemoryInfo struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Available uint64  `json:"available"`
	Percent   float64 `json:"percent"`
}

// DiskInfo is the volume holding the app directory.
type DiskInfo struct {
	Path    string  `json:"path"`
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Free    uint64  `json:"free"`
	Percent float64 `json:"percent"`
}

// ProcInfo is one process.
type ProcInfo struct {
	PID        int32   `json:"pid"`
	CPUPercent float64 `json:"cpu_percent"`
	// RSS is the resident memory in bytes.
	RSS     uint64 `json:"rss"`
	Threads int32  `json:"threads"`
	// OpenFiles is -1 where the OS doesn't say.
	OpenFiles int32      `json:"open_files"`
	StartedAt *time.Time `json:"started_at,omitempty"`
}

// SystemSampler samples the machine and the app every interval, so a
// request never waits for a CPU measurement. Start it with Run.
type SystemSampler struct {
	dir      string
	appPID   func() int
	interval time.Duration
	mu       sync.Mutex
	last     SystemInfo
	procs    map[int32]*process.Process
}

// NewSystemSampler returns a sampler for the app in dir whose PID appPID
// reports (0 when it isn't running).
func NewSystemSampler(dir string, appPID func() int) *SystemSampler {
	return &SystemSampler{dir: dir, appPID: appPID, interval: 2 * time.Second, procs: map[int32]*process.Process{}}
}

// Run samples until ctx ends.
func (s *SystemSampler) Run(ctx context.Context) {
	s.sample(ctx, 0) // the first sample answers immediately, without CPU use
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sample(ctx, s.interval)
		}
	}
}

// Last returns the most recent sample.
func (s *SystemSampler) Last() SystemInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

func (s *SystemSampler) sample(ctx context.Context, since time.Duration) {
	info := SystemInfo{SampledAt: time.Now().UTC(), Host: HostInfo{OS: runtime.GOOS, Arch: runtime.GOARCH, Cores: runtime.NumCPU()}}
	info.Host.Hostname, _ = os.Hostname()
	if since > 0 {
		// gopsutil compares with the previous call's counters.
		if pct, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(pct) > 0 {
			info.Host.CPUPercent = pct[0]
		}
	} else {
		_, _ = cpu.PercentWithContext(ctx, 0, false) // prime the counters
	}
	if avg, err := load.AvgWithContext(ctx); err == nil {
		info.Host.Load1, info.Host.Load5, info.Host.Load15 = avg.Load1, avg.Load5, avg.Load15
	}
	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		info.Memory = MemoryInfo{Total: vm.Total, Used: vm.Used, Available: vm.Available, Percent: vm.UsedPercent}
	}
	if du, err := disk.UsageWithContext(ctx, s.dir); err == nil {
		info.Disk = &DiskInfo{Path: du.Path, Total: du.Total, Used: du.Used, Free: du.Free, Percent: du.UsedPercent}
	}
	info.Orb = s.proc(ctx, int32(os.Getpid())) //nolint:gosec // a PID fits
	if pid := s.appPID(); pid > 0 {
		p := s.proc(ctx, int32(pid)) //nolint:gosec // a PID fits
		info.App = &p
	} else {
		s.mu.Lock()
		for pid := range s.procs {
			if int(pid) != os.Getpid() {
				delete(s.procs, pid)
			}
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	s.last = info
	s.mu.Unlock()
}

// proc samples one process, keeping its handle so CPU percentages are
// measured between samples.
func (s *SystemSampler) proc(ctx context.Context, pid int32) ProcInfo {
	s.mu.Lock()
	p, ok := s.procs[pid]
	if !ok {
		var err error
		if p, err = process.NewProcessWithContext(ctx, pid); err != nil {
			s.mu.Unlock()
			return ProcInfo{PID: pid, OpenFiles: -1}
		}
		s.procs[pid] = p
	}
	s.mu.Unlock()
	info := ProcInfo{PID: pid, OpenFiles: -1}
	if pct, err := p.PercentWithContext(ctx, 0); err == nil {
		info.CPUPercent = pct
	}
	if m, err := p.MemoryInfoWithContext(ctx); err == nil && m != nil {
		info.RSS = m.RSS
	}
	if n, err := p.NumThreadsWithContext(ctx); err == nil {
		info.Threads = n
	}
	if n, err := p.NumFDsWithContext(ctx); err == nil {
		info.OpenFiles = n
	}
	if ms, err := p.CreateTimeWithContext(ctx); err == nil && ms > 0 {
		t := time.UnixMilli(ms).UTC()
		info.StartedAt = &t
	}
	return info
}
