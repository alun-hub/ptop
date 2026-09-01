package bench

import (
	"fmt"
	"sort"
)

// Severity ranks an Advice from lowest to highest urgency.
type Severity int

const (
	SevInfo Severity = iota
	SevWarn
	SevCrit
)

func (s Severity) Label() string {
	switch s {
	case SevWarn:
		return "warn"
	case SevCrit:
		return "important"
	}
	return "info"
}

// Advice is one plain-language recommendation derived from the machine
// inventory: an observation, why it matters, and (when possible) an exact
// command that fixes it.
type Advice struct {
	Severity Severity
	Title    string // the observation, one line
	Detail   string // why it matters
	Fix      string // shell command or config change; "" when there is nothing to run
}

// Advise inspects the inventory and returns recommendations, most urgent first.
// It needs no benchmark results - every rule fires on system configuration
// alone. The list is empty when nothing stands out.
func Advise(inv Inventory) []Advice {
	var out []Advice
	add := func(sev Severity, title, detail, fix string) {
		out = append(out, Advice{sev, title, detail, fix})
	}

	switch inv.Governor {
	case "powersave", "ondemand", "conservative":
		add(SevWarn,
			"CPU scaling governor is "+inv.Governor,
			"The CPU may stay at a low clock under bursty load, giving inconsistent and lower benchmark numbers. Servers usually want the 'performance' governor.",
			"sudo cpupower frequency-set -g performance   # or: sudo tuned-adm profile throughput-performance")
	}

	if inv.THP == "always" {
		add(SevWarn,
			"Transparent huge pages set to 'always'",
			"Databases and latency-sensitive services (PostgreSQL, Redis, MongoDB) recommend 'madvise' - 'always' causes allocation stalls and jitter.",
			"echo madvise | sudo tee /sys/kernel/mm/transparent_hugepage/enabled")
	}

	if inv.Swappiness > 10 && inv.MemTotalGB >= 8 {
		sev := SevInfo
		if inv.Swappiness >= 60 {
			sev = SevWarn
		}
		add(sev,
			fmt.Sprintf("vm.swappiness is %d", inv.Swappiness),
			"With plenty of RAM this makes the kernel swap out working-set pages too eagerly, hurting tail latency. A value of 1-10 is typical for servers.",
			"sudo sysctl -w vm.swappiness=10   # persist in /etc/sysctl.d/99-ptop.conf")
	}

	if inv.SwapTotalGB == 0 && inv.MemTotalGB < 4 {
		add(SevInfo,
			"No swap configured",
			"On a small-memory host a brief spike can trigger the OOM killer instead of a temporary slowdown. A little swap or zram adds a safety margin.",
			"")
	}

	if inv.MemTotalGB > 0 && inv.MemAvailGB > 0 && inv.MemAvailGB < 0.1*inv.MemTotalGB {
		add(SevWarn,
			"Very little free memory",
			"Less than 10% of RAM is available. Benchmarks that allocate a working set may be served from swap or evict caches, skewing results.",
			"")
	}

	if len(inv.CPUFlags) > 0 && !hasFlag(inv.CPUFlags, "aes") {
		add(SevInfo,
			"No AES-NI on this CPU",
			"TLS termination, disk encryption (LUKS) and the CPU crypto benchmark will run several times slower in software.",
			"")
	}

	if p := primaryDisk(inv); p != nil {
		if p.Rotational {
			add(SevInfo,
				"Primary disk "+p.Device+" is a spinning HDD",
				"Random-I/O and database verdicts are graded on an absolute scale, so an HDD will read as 'low' even when healthy for its class. Judge it against other HDDs.",
				"")
			if p.Scheduler == "none" {
				add(SevInfo,
					"I/O scheduler 'none' on HDD "+p.Device,
					"Rotational disks benefit from request reordering. 'mq-deadline' or 'bfq' usually improves throughput and fairness.",
					fmt.Sprintf("echo mq-deadline | sudo tee /sys/block/%s/queue/scheduler", p.Device))
			}
		} else if p.Scheduler == "bfq" {
			add(SevInfo,
				"I/O scheduler 'bfq' on SSD "+p.Device,
				"On fast SSD/NVMe the scheduler overhead often costs IOPS; 'none' or 'mq-deadline' is usually better.",
				fmt.Sprintf("echo none | sudo tee /sys/block/%s/queue/scheduler", p.Device))
		}
	}

	if inv.NUMANodes > 1 {
		add(SevInfo,
			fmt.Sprintf("%d NUMA nodes", inv.NUMANodes),
			"Cross-node memory access is slower. Pin latency-sensitive processes to one node (numactl --cpunodebind=0 --membind=0) and check the memory test's NUMA rows.",
			"")
	}

	for _, tool := range []string{"fio", "sysbench", "iperf3"} {
		if !have(tool) {
			add(SevInfo,
				"'"+tool+"' is not installed",
				"ptop falls back to a built-in implementation, but "+tool+" gives more accurate and comparable numbers for the "+toolArea(tool)+" test.",
				"sudo "+pkgHint(tool))
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Severity > out[j].Severity })
	return out
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

// primaryDisk returns the largest non-removable block device, the best guess
// for where the OS and data live.
func primaryDisk(inv Inventory) *DiskInfo {
	var best *DiskInfo
	for i := range inv.Disks {
		d := &inv.Disks[i]
		if best == nil || d.SizeGB > best.SizeGB {
			best = d
		}
	}
	return best
}

func toolArea(tool string) string {
	switch tool {
	case "fio":
		return "disk"
	case "sysbench":
		return "CPU and memory"
	case "iperf3":
		return "network throughput"
	}
	return ""
}

func pkgHint(tool string) string {
	// Covers the common package managers without detecting the distro.
	return fmt.Sprintf("apt install %s   (or: dnf install %s / pacman -S %s)", tool, tool, tool)
}
