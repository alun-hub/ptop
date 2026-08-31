package bench

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var mibPerSecRe = regexp.MustCompile(`\(([0-9.]+)\s*MiB/sec\)`)

func runMem(ctx context.Context, cfg Config, out chan<- Event) (Result, error) {
	info := Info()
	availFrac := 0.0
	if info.MemTotalGB > 0 {
		availFrac = info.MemAvailGB / info.MemTotalGB
	}
	base := []Metric{{
		Name:    "Available memory",
		Display: fmt.Sprintf("%.1f GB of %.1f GB", info.MemAvailGB, info.MemTotalGB),
		Verdict: memAvailVerdict(info),
		Note:    memAvailNote(info),
		Bar:     availFrac, HasBar: true,
		ScaleLo: "none free", ScaleHi: "all free",
	}}

	var bwGBs float64
	var tool string
	var err error
	if have("sysbench") {
		tool = "sysbench"
		bwGBs, err = memSysbench(ctx, cfg, out)
	} else {
		out <- LogLine{Text: "sysbench not found - using the built-in copy test"}
		tool = "built-in copy test"
		bwGBs, err = memNative(ctx, cfg, out)
	}
	if err != nil {
		return Result{Tool: tool, Metrics: base}, err
	}

	res := Result{Tool: tool}
	res.Metrics = append(base, Metric{
		Name:    "Memory bandwidth (write)",
		Display: fmt.Sprintf("%.1f GB/s", bwGBs),
		Verdict: memBWVerdict(bwGBs),
		Note:    memBWNote(bwGBs),
		Bar:     normLog(clampLo(bwGBs, 1), 1, 50), HasBar: true,
		ScaleLo: "slow", ScaleHi: "fast server DDR",
	}.cmp(bwGBs, "GB/s", false))

	// Latency: a dependent-load pointer chase through a region much larger than
	// the last-level cache. This is what stalls the CPU on random-access
	// workloads (hash maps, pointer-heavy code, databases).
	out <- Progress{Frac: 0.7, Label: "memory latency (pointer chase)"}
	if ns := memLatencyNs(ctx); ns > 0 {
		res.Metrics = append(res.Metrics, Metric{
			Name:    "Memory latency (random access)",
			Display: fmt.Sprintf("%.0f ns", ns),
			Verdict: memLatVerdict(ns),
			Note:    memLatNote(ns),
			Bar:     1 - norm(ns, 40, 300), HasBar: true,
			ScaleLo: "slow", ScaleHi: "fast",
		}.cmp(ns, "ns", true))
	}
	if m, ok := numaMetric(); ok {
		res.Metrics = append(res.Metrics, m)
	}
	out <- Progress{Frac: 1, Label: "done"}

	res.Summary = fmt.Sprintf("Memory delivers about %.1f GB/s. %s", bwGBs, memAvailNote(info))
	return res, nil
}

// numaMetric reports the NUMA topology from /sys/devices/system/node.
func numaMetric() (Metric, bool) {
	entries, err := os.ReadDir("/sys/devices/system/node")
	if err != nil {
		return Metric{}, false
	}
	var nodes []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, "node") && len(n) > 4 && n[4] >= '0' && n[4] <= '9' {
			nodes = append(nodes, n)
		}
	}
	if len(nodes) == 0 {
		return Metric{}, false
	}
	if len(nodes) == 1 {
		return Metric{
			Name: "NUMA topology", Display: "single node",
			Verdict: VGood,
			Note:    "all RAM is equally close to every core - no placement tuning needed",
		}, true
	}

	// distance line: local is 10, remote is 10*ratio
	maxRatio := 1.0
	if b, err := os.ReadFile("/sys/devices/system/node/" + nodes[0] + "/distance"); err == nil {
		for _, f := range strings.Fields(string(b)) {
			if v, e := strconv.ParseFloat(f, 64); e == nil && v/10 > maxRatio {
				maxRatio = v / 10
			}
		}
	}
	var total float64
	for _, n := range nodes {
		total += nodeMemGB(n)
	}
	m := Metric{
		Name:    "NUMA topology",
		Display: fmt.Sprintf("%d nodes, remote RAM up to %.1fx farther", len(nodes), maxRatio),
		Note: fmt.Sprintf("%.0f GB split across nodes - pin latency-sensitive services to one node (numactl / systemd NUMAPolicy)",
			total),
		Bar: 1 - norm(maxRatio, 1, 3.5), HasBar: true, ScaleLo: "far", ScaleHi: "uniform",
	}
	switch {
	case maxRatio < 1.6:
		m.Verdict = VOkay
	case maxRatio < 2.3:
		m.Verdict = VOkay
	default:
		m.Verdict = VPoor
	}
	return m, true
}

func nodeMemGB(node string) float64 {
	b, err := os.ReadFile("/sys/devices/system/node/" + node + "/meminfo")
	if err != nil {
		return 0
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.Contains(ln, "MemTotal:") {
			f := strings.Fields(ln)
			if len(f) >= 4 {
				kb, _ := strconv.ParseFloat(f[3], 64)
				return kb / 1024 / 1024
			}
		}
	}
	return 0
}

func memSysbench(ctx context.Context, cfg Config, out chan<- Event) (float64, error) {
	secs := cfg.Depth.Seconds()
	stop := timeProgress(out, time.Duration(secs)*time.Second, "writing memory blocks")
	raw, err := streamCmd(ctx, out, "sysbench", "memory",
		"--memory-block-size=1M", "--memory-total-size=100G",
		"--memory-oper=write", "--threads=1", "--time="+strconv.Itoa(secs), "run")
	stop()
	if err != nil {
		return 0, err
	}
	m := mibPerSecRe.FindStringSubmatch(raw)
	if m == nil {
		return 0, fmt.Errorf("could not parse sysbench output")
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v * 1.048576 / 1000, nil // MiB/s -> GB/s
}

func memNative(ctx context.Context, cfg Config, out chan<- Event) (float64, error) {
	const bufMB = 256
	src := make([]byte, bufMB<<20)
	dst := make([]byte, bufMB<<20)
	for i := range src {
		src[i] = byte(i)
	}
	secs := 4
	stop := timeProgress(out, time.Duration(secs)*time.Second, "copying memory")
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	var bytes float64
	for time.Now().Before(deadline) {
		copy(dst, src)
		bytes += float64(len(src))
		select {
		case <-ctx.Done():
			stop()
			return 0, ctx.Err()
		default:
		}
	}
	stop()
	return bytes / float64(secs) / 1e9, nil
}

// memLatencyNs measures average random-access latency (nanoseconds per load)
// via a pointer chase over a 128 MiB permutation cycle.
func memLatencyNs(ctx context.Context) float64 {
	const n = 128 << 20 / 8 // number of uint64 slots (128 MiB)
	idx := make([]uint64, n)
	perm := rand.New(rand.NewSource(1)).Perm(n)
	// Build a single cycle: idx[perm[i]] -> perm[i+1].
	for i := 0; i < n; i++ {
		idx[perm[i]] = uint64(perm[(i+1)%n])
	}
	// Warm up a little and bail out if cancelled.
	select {
	case <-ctx.Done():
		return 0
	default:
	}

	const steps = 20_000_000
	var p uint64
	start := time.Now()
	for i := 0; i < steps; i++ {
		p = idx[p]
	}
	elapsed := time.Since(start)
	if p == ^uint64(0) { // keep the compiler from optimising the loop away
		return 0
	}
	return float64(elapsed.Nanoseconds()) / float64(steps)
}

func memAvailVerdict(i SysInfo) Verdict {
	if i.MemTotalGB == 0 {
		return VNeutral
	}
	switch frac := i.MemAvailGB / i.MemTotalGB; {
	case frac >= 0.4:
		return VGood
	case frac >= 0.15:
		return VOkay
	default:
		return VPoor
	}
}
func memAvailNote(i SysInfo) string {
	if i.MemTotalGB == 0 {
		return ""
	}
	switch frac := i.MemAvailGB / i.MemTotalGB; {
	case frac >= 0.4:
		return "plenty of free memory"
	case frac >= 0.15:
		return "some memory free - watch it under load"
	default:
		return "little free memory - risk of swapping or OOM during spikes"
	}
}
func memBWVerdict(g float64) Verdict {
	switch {
	case g >= 15:
		return VGood
	case g >= 5:
		return VOkay
	case g > 0:
		return VPoor
	}
	return VNeutral
}
func memBWNote(g float64) string {
	switch {
	case g >= 30:
		return "very high - DDR5 or many memory channels"
	case g >= 15:
		return "good - modern server DDR4/DDR5"
	case g >= 5:
		return "ok - typical for a single-channel or virtualised machine"
	case g > 0:
		return "low - can bottleneck memory-intensive work"
	}
	return ""
}
func memLatVerdict(ns float64) Verdict {
	switch {
	case ns <= 90:
		return VGood
	case ns <= 160:
		return VOkay
	default:
		return VPoor
	}
}
func memLatNote(ns float64) string {
	switch {
	case ns <= 90:
		return "low - bare metal or a well-placed VM"
	case ns <= 160:
		return "moderate - virtualisation overhead or a busy memory bus"
	default:
		return "high - NUMA-remote memory, heavy noisy-neighbour load, or an old platform"
	}
}
