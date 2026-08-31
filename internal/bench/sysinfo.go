package bench

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// SysInfo is a small snapshot shown on the welcome screen and used for defaults.
type SysInfo struct {
	Hostname   string
	CPUModel   string
	NumCPU     int
	MemTotalGB float64
	MemAvailGB float64
	IsRoot     bool
}

func Info() SysInfo {
	s := SysInfo{NumCPU: runtime.NumCPU(), IsRoot: os.Geteuid() == 0}
	s.Hostname, _ = os.Hostname()

	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(ln, "model name") {
				if i := strings.Index(ln, ":"); i >= 0 {
					s.CPUModel = strings.TrimSpace(ln[i+1:])
				}
				break
			}
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			f := strings.Fields(ln)
			if len(f) < 2 {
				continue
			}
			kb, _ := strconv.ParseFloat(f[1], 64)
			switch f[0] {
			case "MemTotal:":
				s.MemTotalGB = kb / 1024 / 1024
			case "MemAvailable:":
				s.MemAvailGB = kb / 1024 / 1024
			}
		}
	}
	return s
}

// FreeSpaceGB returns the free space available to a normal user at path.
func FreeSpaceGB(path string) float64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return float64(st.Bavail) * float64(st.Bsize) / 1e9
}

// DiskTargets lists sensible directories to run a disk test in: the current
// directory, $HOME and any writable "real" mount points from /proc/mounts.
func DiskTargets() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			return
		}
		if unix_access_w(p) {
			seen[p] = true
			out = append(out, p)
		}
	}
	if wd, err := os.Getwd(); err == nil {
		add(wd)
	}
	add(os.Getenv("HOME"))

	if b, err := os.ReadFile("/proc/mounts"); err == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			f := strings.Fields(ln)
			if len(f) < 3 {
				continue
			}
			dev, mnt, fs := f[0], f[1], f[2]
			switch fs {
			case "ext4", "xfs", "btrfs", "zfs", "ext3", "f2fs", "nfs", "nfs4":
			default:
				continue
			}
			if !strings.HasPrefix(dev, "/dev/") && !strings.Contains(dev, ":") {
				continue
			}
			add(mnt)
		}
	}
	return out
}

func unix_access_w(p string) bool {
	return syscall.Access(p, 2) == nil // W_OK
}

// physicalCores counts distinct (physical id, core id) pairs in /proc/cpuinfo,
// i.e. real cores excluding SMT/hyper-threads. Returns 0 if it cannot tell.
func physicalCores() int {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	seen := map[string]bool{}
	var phys, core string
	flush := func() {
		if core != "" {
			seen[phys+"/"+core] = true
		}
		phys, core = "", ""
	}
	for _, ln := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(ln, ":")
		if !ok {
			if strings.TrimSpace(ln) == "" {
				flush()
			}
			continue
		}
		switch strings.TrimSpace(k) {
		case "physical id":
			phys = strings.TrimSpace(v)
		case "core id":
			core = strings.TrimSpace(v)
		}
	}
	flush()
	return len(seen)
}
