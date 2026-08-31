// Package bench runs the individual performance tests and streams progress
// events back to the UI. Every test prefers a well-known external tool
// (fio, sysbench, iperf3) and falls back to a built-in implementation so
// ptop works on a bare server with nothing installed.
package bench

import (
	"context"
	"math"
)

// Kind identifies which family of test to run.
type Kind int

const (
	Disk Kind = iota
	CPU
	Mem
	Net
	GPU
)

func (k Kind) String() string {
	switch k {
	case Disk:
		return "Disk"
	case CPU:
		return "CPU"
	case Mem:
		return "Memory"
	case Net:
		return "Network"
	case GPU:
		return "GPU"
	}
	return "?"
}

// Depth is the guided "how thorough" choice presented to the user.
type Depth int

const (
	Quick  Depth = iota // ~10s per sub-test, good enough for a first look
	Normal              // ~30s, the recommended default
	Deep                // ~60s, for careful comparisons
)

func (d Depth) Seconds() int {
	switch d {
	case Quick:
		return 10
	case Deep:
		return 60
	}
	return 30
}

func (d Depth) String() string {
	switch d {
	case Quick:
		return "Quick (~10s)"
	case Deep:
		return "Thorough (~60s)"
	}
	return "Normal (~30s)"
}

// Token is the short lowercase form used on the CLI and in history.
func (d Depth) Token() string {
	switch d {
	case Quick:
		return "quick"
	case Deep:
		return "deep"
	}
	return "normal"
}

// Config is a fully resolved test request.
type Config struct {
	Kind   Kind
	Depth  Depth
	Path   string // Disk: directory to test in
	Host   string // Net: target host for ping/iperf3/ptop-serve (optional)
	Port   int    // Net: port for `ptop serve` (0 = default ServePort)
	URL    string // Net: download URL when no throughput peer is given
	SizeMB int    // Disk/Mem: working-set size
	IsRoot bool   // whether we run as uid 0 (enables cache dropping)
}

// Event is streamed from a running test to the UI.
type Event interface{ isEvent() }

// LogLine is a single line of raw output from the underlying tool.
type LogLine struct{ Text string }

// Progress reports fractional completion (0..1) with a short label.
type Progress struct {
	Frac  float64
	Label string
}

// Finished is always the last event. Err is nil on success.
type Finished struct {
	Result Result
	Err    error
}

func (LogLine) isEvent()  {}
func (Progress) isEvent() {}
func (Finished) isEvent() {}

// Verdict is a coarse human rating of a single metric.
type Verdict int

const (
	VNeutral Verdict = iota
	VGood
	VOkay
	VPoor
)

func (v Verdict) Label() string {
	switch v {
	case VGood:
		return "good"
	case VOkay:
		return "ok"
	case VPoor:
		return "low"
	}
	return ""
}

// Metric is one headline number with interpretation.
type Metric struct {
	Name    string
	Display string // preformatted value + unit, e.g. "512 MB/s"
	Verdict Verdict
	Note    string  // one-line plain-language interpretation
	Bar     float64 // 0..1 fill for the gauge; <=0 hides the bar
	// ScaleLo/ScaleHi label the two ends of the gauge: what an empty bar and a
	// full bar mean (e.g. "hard disk" ... "fast NVMe"). The Note says which
	// regime the actual value lands in.
	ScaleLo string
	ScaleHi string
	HasBar  bool // draw the gauge even when Bar rounds to 0

	// Value / LowerBetter drive run-to-run history comparison. Value is in a
	// stable unit for this metric name (see Unit); 0 means "not comparable"
	// (informational rows like GPU model or NUMA topology).
	Value       float64
	Unit        string
	LowerBetter bool
}

// cmp fills Value/Unit/LowerBetter and returns the metric (chaining helper).
func (m Metric) cmp(v float64, unit string, lowerBetter bool) Metric {
	m.Value, m.Unit, m.LowerBetter = v, unit, lowerBetter
	return m
}

// norm maps v onto 0..1 across [lo,hi] (clamped).
func norm(v, lo, hi float64) float64 {
	if hi <= lo {
		return 0
	}
	f := (v - lo) / (hi - lo)
	switch {
	case f < 0:
		return 0
	case f > 1:
		return 1
	}
	return f
}

// normLog is norm on a log10 scale, good for quantities spanning decades
// (throughput, IOPS). lo must be > 0.
func normLog(v, lo, hi float64) float64 {
	if v < lo {
		v = lo
	}
	return norm(math.Log10(v), math.Log10(lo), math.Log10(hi))
}

// Result is the outcome of a completed test.
type Result struct {
	Kind    Kind
	Tool    string // "fio", "dd (inbyggd fallback)", ...
	Metrics []Metric
	Summary string // one or two sentences the user can act on
}

// Run executes cfg and pushes Events to out, closing out when done.
// It always sends exactly one Finished as the final event before closing.
func Run(ctx context.Context, cfg Config, out chan<- Event) {
	defer close(out)
	var res Result
	var err error
	switch cfg.Kind {
	case Disk:
		res, err = runDisk(ctx, cfg, out)
	case CPU:
		res, err = runCPU(ctx, cfg, out)
	case Mem:
		res, err = runMem(ctx, cfg, out)
	case Net:
		res, err = runNet(ctx, cfg, out)
	case GPU:
		res, err = runGPU(ctx, cfg, out)
	}
	res.Kind = cfg.Kind
	out <- Finished{Result: res, Err: err}
}
