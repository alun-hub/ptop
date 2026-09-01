package bench

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Inventory is a best-effort snapshot of the machine ptop is running on. Every
// field is filled from /proc, /sys and /etc (no root required) with a graceful
// zero value when the source is missing. It is shown by `ptop info` and is the
// basis for baseline profiles and recommendations.
type Inventory struct {
	Hostname string        `json:"hostname,omitempty"`
	OSName   string        `json:"os_name,omitempty"` // /etc/os-release PRETTY_NAME
	Kernel   string        `json:"kernel,omitempty"`  // /proc/sys/kernel/osrelease
	Arch     string        `json:"arch,omitempty"`    // GOARCH
	Uptime   time.Duration `json:"uptime_ns,omitempty"`

	CPUModel      string   `json:"cpu_model,omitempty"`
	CPUVendor     string   `json:"cpu_vendor,omitempty"`
	Sockets       int      `json:"sockets,omitempty"`
	PhysicalCores int      `json:"physical_cores,omitempty"`
	LogicalCPUs   int      `json:"logical_cpus,omitempty"`
	CPUMHzMax     float64  `json:"cpu_mhz_max,omitempty"`
	Governor      string   `json:"governor,omitempty"`    // scaling governor (all-CPU consensus, "" if mixed/unknown)
	CPUFlags      []string `json:"cpu_flags,omitempty"`   // interesting flags actually present (aes, avx2, ...)
	Virtualized   bool     `json:"virtualized,omitempty"` // running under a hypervisor

	MemTotalGB  float64 `json:"mem_total_gb,omitempty"`
	MemAvailGB  float64 `json:"mem_avail_gb,omitempty"`
	SwapTotalGB float64 `json:"swap_total_gb,omitempty"`
	Swappiness  int     `json:"swappiness,omitempty"` // -1 if unreadable
	THP         string  `json:"thp,omitempty"`
	NUMANodes   int     `json:"numa_nodes,omitempty"`

	Virt        string `json:"virt,omitempty"`         // "kvm", "docker", "none", ... best-effort
	CloudVendor string `json:"cloud_vendor,omitempty"` // "AWS", "GCP", "Azure", "Hetzner", ... "" if unknown
	ProductName string `json:"product_name,omitempty"` // DMI product name (often the instance type on cloud)

	Disks []DiskInfo `json:"disks,omitempty"`
	NICs  []NICInfo  `json:"nics,omitempty"`
}

// DiskInfo describes one whole block device (not a partition).
type DiskInfo struct {
	Device     string  `json:"device,omitempty"`
	Model      string  `json:"model,omitempty"`
	SizeGB     float64 `json:"size_gb,omitempty"`
	Rotational bool    `json:"rotational,omitempty"`
	Scheduler  string  `json:"scheduler,omitempty"`
}

// NICInfo describes one network interface.
type NICInfo struct {
	Name      string `json:"name,omitempty"`
	SpeedMbps int    `json:"speed_mbps,omitempty"` // 0 if unknown (virtual NICs often report -1)
	State     string `json:"state,omitempty"`
}

var interestingCPUFlags = []string{
	"aes", "sha_ni", "avx", "avx2", "avx512f", "sse4_2",
	"vmx", "svm", "hypervisor", "constant_tsc",
}

// Inventorize collects the machine inventory. It never fails; missing data is
// left at the zero value.
func Inventorize() Inventory {
	inv := Inventory{
		Arch:        runtime.GOARCH,
		LogicalCPUs: runtime.NumCPU(),
		Swappiness:  -1,
	}
	inv.Hostname, _ = os.Hostname()
	inv.Kernel = readTrim("/proc/sys/kernel/osrelease")
	inv.OSName = osReleasePretty(readFile("/etc/os-release"))
	inv.Uptime = parseUptime(readFile("/proc/uptime"))

	fillCPU(&inv)
	fillMemory(&inv)
	inv.NUMANodes = countNUMANodes()
	inv.Governor = consensusGovernor()
	inv.CPUMHzMax = maxCPUFreqMHz()

	inv.Virt, inv.Virtualized = detectVirt(inv)
	inv.ProductName = readTrim("/sys/class/dmi/id/product_name")
	inv.CloudVendor = detectCloud()

	inv.Disks = collectDisks()
	inv.NICs = collectNICs()
	return inv
}

func fillCPU(inv *Inventory) {
	b := readFile("/proc/cpuinfo")
	if b == "" {
		return
	}
	physIDs := map[string]bool{}
	for _, block := range strings.Split(b, "\n\n") {
		for _, ln := range strings.Split(block, "\n") {
			k, v, ok := strings.Cut(ln, ":")
			if !ok {
				continue
			}
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			switch k {
			case "model name":
				if inv.CPUModel == "" {
					inv.CPUModel = v
				}
			case "vendor_id":
				if inv.CPUVendor == "" {
					inv.CPUVendor = v
				}
			case "physical id":
				physIDs[v] = true
			case "flags", "Features":
				if inv.CPUFlags == nil {
					inv.CPUFlags = pickFlags(strings.Fields(v))
				}
			}
		}
	}
	inv.Sockets = len(physIDs)
	if inv.Sockets == 0 {
		inv.Sockets = 1
	}
	inv.PhysicalCores = physicalCores()
	if inv.PhysicalCores == 0 {
		inv.PhysicalCores = inv.LogicalCPUs
	}
	for _, f := range inv.CPUFlags {
		if f == "hypervisor" {
			inv.Virtualized = true
		}
	}
}

func pickFlags(have []string) []string {
	set := map[string]bool{}
	for _, f := range have {
		set[f] = true
	}
	var out []string
	for _, f := range interestingCPUFlags {
		if set[f] {
			out = append(out, f)
		}
	}
	return out
}

func fillMemory(inv *Inventory) {
	for _, ln := range strings.Split(readFile("/proc/meminfo"), "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		kb, _ := strconv.ParseFloat(f[1], 64)
		switch f[0] {
		case "MemTotal:":
			inv.MemTotalGB = kb / 1024 / 1024
		case "MemAvailable:":
			inv.MemAvailGB = kb / 1024 / 1024
		case "SwapTotal:":
			inv.SwapTotalGB = kb / 1024 / 1024
		}
	}
	if s := readTrim("/proc/sys/vm/swappiness"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			inv.Swappiness = n
		}
	}
	inv.THP = bracketChoice(readTrim("/sys/kernel/mm/transparent_hugepage/enabled"))
}

func countNUMANodes() int {
	entries, err := os.ReadDir("/sys/devices/system/node")
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "node") && isDigits(e.Name()[4:]) {
			n++
		}
	}
	return n
}

// consensusGovernor returns the scaling governor if every online CPU agrees,
// otherwise "".
func consensusGovernor() string {
	paths, _ := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_governor")
	gov := ""
	for _, p := range paths {
		g := readTrim(p)
		if g == "" {
			continue
		}
		if gov == "" {
			gov = g
		} else if gov != g {
			return ""
		}
	}
	return gov
}

func maxCPUFreqMHz() float64 {
	paths, _ := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*/cpufreq/cpuinfo_max_freq")
	var max float64
	for _, p := range paths {
		if khz, err := strconv.ParseFloat(readTrim(p), 64); err == nil && khz > max {
			max = khz
		}
	}
	return max / 1000
}

func detectVirt(inv Inventory) (name string, virtualized bool) {
	if out, err := runQuiet("systemd-detect-virt"); err == nil {
		v := strings.TrimSpace(out)
		if v != "" && v != "none" {
			return v, true
		}
		if v == "none" {
			return "none", false
		}
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "docker", true
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return "podman", true
	}
	if inv.Virtualized {
		return "vm", true
	}
	return "none", false
}

// dmiCloudVendors maps a lowercased substring of a DMI field to a display name.
var dmiCloudVendors = []struct{ needle, name string }{
	{"amazon ec2", "AWS"},
	{"amazon", "AWS"},
	{"google compute engine", "GCP"},
	{"google", "GCP"},
	{"microsoft corporation", "Azure"},
	{"hetzner", "Hetzner"},
	{"digitalocean", "DigitalOcean"},
	{"ovh", "OVH"},
	{"oracle", "Oracle Cloud"},
	{"scaleway", "Scaleway"},
	{"vultr", "Vultr"},
	{"linode", "Linode"},
	{"alibaba cloud", "Alibaba Cloud"},
}

func detectCloud() string {
	fields := []string{
		readTrim("/sys/class/dmi/id/sys_vendor"),
		readTrim("/sys/class/dmi/id/product_name"),
		readTrim("/sys/class/dmi/id/bios_vendor"),
		readTrim("/sys/class/dmi/id/chassis_asset_tag"),
	}
	return cloudFromDMI(fields)
}

func cloudFromDMI(fields []string) string {
	// Azure's well-known chassis asset tag.
	for _, f := range fields {
		if strings.Contains(f, "7783-7084-3265-9085-8269-3286-77") {
			return "Azure"
		}
	}
	for _, f := range fields {
		lf := strings.ToLower(f)
		for _, c := range dmiCloudVendors {
			if strings.Contains(lf, c.needle) {
				return c.name
			}
		}
	}
	return ""
}

func collectDisks() []DiskInfo {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil
	}
	var out []DiskInfo
	for _, e := range entries {
		name := e.Name()
		if isVirtualBlockDev(name) {
			continue
		}
		base := filepath.Join("/sys/block", name)
		d := DiskInfo{
			Device:     name,
			Model:      readTrim(filepath.Join(base, "device/model")),
			Rotational: readTrim(filepath.Join(base, "queue/rotational")) == "1",
			Scheduler:  bracketChoice(readTrim(filepath.Join(base, "queue/scheduler"))),
		}
		if sectors, err := strconv.ParseFloat(readTrim(filepath.Join(base, "size")), 64); err == nil {
			d.SizeGB = sectors * 512 / 1e9
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Device < out[j].Device })
	return out
}

func isVirtualBlockDev(name string) bool {
	for _, p := range []string{"loop", "ram", "dm-", "sr", "zram", "md", "fd"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func collectNICs() []NICInfo {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil
	}
	var out []NICInfo
	for _, e := range entries {
		name := e.Name()
		if name == "lo" {
			continue
		}
		base := filepath.Join("/sys/class/net", name)
		n := NICInfo{Name: name, State: readTrim(filepath.Join(base, "operstate"))}
		if mbps, err := strconv.Atoi(readTrim(filepath.Join(base, "speed"))); err == nil && mbps > 0 {
			n.SpeedMbps = mbps
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// --- small parsing helpers (pure, unit-tested) ---

func osReleasePretty(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if v, ok := strings.CutPrefix(ln, "PRETTY_NAME="); ok {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}

func parseUptime(s string) time.Duration {
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0
	}
	secs, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// bracketChoice extracts the selected option from a sysfs "a [b] c" list. If
// there is no bracketed token it returns the whole trimmed string.
func bracketChoice(s string) string {
	i := strings.IndexByte(s, '[')
	j := strings.IndexByte(s, ']')
	if i >= 0 && j > i {
		return s[i+1 : j]
	}
	return strings.TrimSpace(s)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func readTrim(path string) string {
	return strings.TrimSpace(readFile(path))
}

func runQuiet(name string, args ...string) (string, error) {
	if !have(name) {
		return "", os.ErrNotExist
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}
