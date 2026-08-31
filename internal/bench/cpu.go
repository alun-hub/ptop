package bench

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var evPerSecRe = regexp.MustCompile(`events per second:\s*([0-9.]+)`)

func runCPU(ctx context.Context, cfg Config, out chan<- Event) (Result, error) {
	if have("sysbench") {
		return cpuSysbench(ctx, cfg, out)
	}
	out <- LogLine{Text: "sysbench not found - using the built-in prime-number test"}
	return cpuNative(ctx, cfg, out)
}

func cpuSysbench(ctx context.Context, cfg Config, out chan<- Event) (Result, error) {
	res := Result{Tool: "sysbench"}
	secs := cfg.Depth.Seconds()
	n := cfg.numCPU()

	run := func(threads int, label string) (float64, error) {
		stop := timeProgress(out, time.Duration(secs)*time.Second, label)
		raw, err := streamCmd(ctx, out, "sysbench", "cpu",
			"--cpu-max-prime=20000", "--threads="+strconv.Itoa(threads),
			"--time="+strconv.Itoa(secs), "run")
		stop()
		if err != nil {
			return 0, err
		}
		m := evPerSecRe.FindStringSubmatch(raw)
		if m == nil {
			return 0, fmt.Errorf("could not parse sysbench output")
		}
		v, _ := strconv.ParseFloat(m[1], 64)
		return v, nil
	}

	single, err := run(1, "1 thread")
	if err != nil {
		return res, err
	}
	out <- Progress{Frac: 0.5, Label: "1 thread done"}
	multi, err := run(n, fmt.Sprintf("%d threads", n))
	if err != nil {
		return res, err
	}
	out <- Progress{Frac: 0.9, Label: "crypto & compression"}
	r := cpuResult(res, single, multi, n)
	r.Metrics = append(r.Metrics, cpuThroughputMetrics(ctx)...)
	out <- Progress{Frac: 0.95, Label: "scheduler, fork/exec & steal-time checks"}
	r.Metrics = append(r.Metrics, cpuExtras(ctx, cfg)...)
	out <- Progress{Frac: 1, Label: "done"}
	return r, nil
}

func cpuNative(ctx context.Context, cfg Config, out chan<- Event) (Result, error) {
	res := Result{Tool: "built-in prime-number test"}
	secs := cfg.Depth.Seconds()
	if secs > 20 {
		secs = 20
	}
	n := cfg.numCPU()

	work := func(d time.Duration, threads int) float64 {
		var total int64
		var wg sync.WaitGroup
		deadline := time.Now().Add(d)
		for i := 0; i < threads; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var local int64
				for time.Now().Before(deadline) {
					countPrimes(20000)
					local++
				}
				atomic.AddInt64(&total, local)
			}()
		}
		wg.Wait()
		return float64(total) / d.Seconds()
	}

	stop := timeProgress(out, time.Duration(secs)*time.Second, "1 thread")
	single := work(time.Duration(secs)*time.Second, 1)
	stop()
	out <- Progress{Frac: 0.5, Label: "1 thread done"}

	stop = timeProgress(out, time.Duration(secs)*time.Second, fmt.Sprintf("%d threads", n))
	multi := work(time.Duration(secs)*time.Second, n)
	stop()

	out <- Progress{Frac: 0.9, Label: "crypto & compression"}
	r := cpuResult(res, single, multi, n)
	r.Metrics = append(r.Metrics, cpuThroughputMetrics(ctx)...)
	out <- Progress{Frac: 0.95, Label: "scheduler, fork/exec & steal-time checks"}
	r.Metrics = append(r.Metrics, cpuExtras(ctx, cfg)...)
	out <- Progress{Frac: 1, Label: "done"}
	return r, nil
}

func countPrimes(max int) int {
	c := 0
	for i := 2; i < max; i++ {
		p := true
		for j := 2; j*j <= i; j++ {
			if i%j == 0 {
				p = false
				break
			}
		}
		if p {
			c++
		}
	}
	return c
}

func cpuResult(res Result, single, multi float64, threads int) Result {
	phys := physicalCores()
	if phys < 1 || phys > threads {
		phys = threads
	}
	smt := phys < threads

	scaling := 0.0
	if single > 0 {
		scaling = multi / single
	}
	// Efficiency is measured against physical cores: SMT threads add maybe
	// 20-30%, so scoring against logical threads unfairly punishes SMT chips.
	eff := 0.0
	if phys > 0 {
		eff = scaling / float64(phys) * 100
	}
	if eff > 100 { // SMT / turbo can nudge it just over; don't show 101%
		eff = 100
	}
	var v Verdict
	switch {
	case eff >= 85:
		v = VGood
	case eff >= 60:
		v = VOkay
	case eff > 0:
		v = VPoor
	}
	relSingle := 0.0
	if multi > 0 {
		relSingle = single / multi
	}

	coreDesc := fmt.Sprintf("%d cores", phys)
	if smt {
		coreDesc = fmt.Sprintf("%d cores / %d threads (SMT)", phys, threads)
	}
	res.Metrics = []Metric{
		Metric{Name: "Single-threaded", Display: fmt.Sprintf("%.0f ops/s", single),
			Note: "higher = faster per core (web servers, single scripts)",
			Bar:  relSingle, HasBar: true, ScaleLo: "1 core", ScaleHi: "whole machine"}.cmp(single, "ops/s", false),
		Metric{Name: fmt.Sprintf("Multi-threaded (%d threads)", threads), Display: fmt.Sprintf("%.0f ops/s", multi),
			Note: "higher = more total capacity (builds, databases, batch jobs)",
			Bar:  1, HasBar: true, ScaleLo: "1 core", ScaleHi: "whole machine"}.cmp(multi, "ops/s", false),
		Metric{Name: "Scaling across cores", Display: fmt.Sprintf("%.1fx (%.0f%% efficiency vs %s)", scaling, eff, coreDesc), Verdict: v,
			Note: cpuScalingNote(eff, smt), Bar: eff / 100, HasBar: true,
			ScaleLo: "no benefit", ScaleHi: "full benefit"}.cmp(scaling, "x", false),
	}
	res.Summary = fmt.Sprintf("With all %d threads the server is about %.1fx faster than with a single one.", threads, scaling)
	return res
}

func cpuScalingNote(eff float64, smt bool) string {
	switch {
	case eff >= 85:
		return "cores work together well - full benefit from parallel jobs"
	case eff >= 60:
		s := "ok scaling - some loss to shared cache/turbo or other processes"
		if smt {
			s = "ok scaling - typical when relying on SMT (hyper-threading) for the last threads"
		}
		return s
	case eff > 0:
		return "weak scaling - cores may be shared (VPS), or the machine was busy"
	}
	return ""
}

func (c Config) numCPU() int {
	if n := runtime.NumCPU(); n > 1 {
		return n
	}
	return 1
}

// cpuExtras runs quick follow-up probes: hypervisor steal time, context-switch
// latency, and (for longer runs) thermal throttling.
func cpuExtras(ctx context.Context, cfg Config) []Metric {
	var out []Metric

	if steal, ok := stealPercent(ctx); ok {
		out = append(out, Metric{
			Name: "CPU steal (hypervisor)", Display: fmt.Sprintf("%.1f%%", steal),
			Verdict: stealVerdict(steal), Note: stealNote(steal),
			Bar: 1 - norm(steal, 0, 20), HasBar: true,
			ScaleLo: "starved", ScaleHi: "no contention",
		}.cmp(steal, "%", true))
	}

	if ns := ctxSwitchNs(ctx); ns > 0 {
		us := ns / 1000
		out = append(out, Metric{
			Name: "Context-switch latency", Display: durMS(us / 1000),
			Verdict: ctxSwitchVerdict(us), Note: ctxSwitchNote(us),
			Bar: 1 - normLog(clampLo(us, 0.2), 0.2, 50), HasBar: true,
			ScaleLo: "slow", ScaleHi: "fast",
		}.cmp(us, "us", true))
	}

	if r := forkExecRate(ctx); r > 0 {
		out = append(out, Metric{
			Name: "Process spawn rate (fork+exec)", Display: fmt.Sprintf("%.0f/s", r),
			Verdict: forkExecVerdict(r), Note: forkExecNote(r),
			Bar: normLog(clampLo(r, 100), 100, 20000), HasBar: true,
			ScaleLo: "slow", ScaleHi: "fast",
		}.cmp(r, "/s", false))
	}

	if cfg.Depth == Deep {
		if drop, ok := thermalDropPercent(ctx); ok {
			out = append(out, Metric{
				Name: "Clock held under load", Display: fmt.Sprintf("%.0f%% of peak", 100-drop),
				Verdict: thermalVerdict(drop), Note: thermalNote(drop),
				Bar: 1 - norm(drop, 0, 40), HasBar: true,
				ScaleLo: "throttling hard", ScaleHi: "no throttling",
			}.cmp(100-drop, "% of peak", false))
		}
	}
	return out
}

// stealPercent samples /proc/stat twice ~700ms apart and returns the share of
// CPU time the hypervisor stole from this guest.
func stealPercent(ctx context.Context) (float64, bool) {
	a, ok := readProcStatCPU()
	if !ok {
		return 0, false
	}
	ctxSleep(ctx, 700*time.Millisecond)
	b, ok := readProcStatCPU()
	if !ok {
		return 0, false
	}
	var dTotal, dSteal float64
	for i := range b {
		if i < len(a) {
			dTotal += b[i] - a[i]
		}
	}
	if len(a) > 8 && len(b) > 8 {
		dSteal = b[8] - a[8] // field index 8 = steal
	}
	if dTotal <= 0 {
		return 0, false
	}
	return dSteal / dTotal * 100, true
}

func readProcStatCPU() ([]float64, bool) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil, false
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(ln, "cpu ") {
			continue
		}
		fields := strings.Fields(ln)[1:]
		out := make([]float64, 0, len(fields))
		for _, f := range fields {
			v, _ := strconv.ParseFloat(f, 64)
			out = append(out, v)
		}
		return out, len(out) > 0
	}
	return nil, false
}

// ctxSwitchNs bounces a token between two goroutines and reports nanoseconds
// per context switch.
func ctxSwitchNs(ctx context.Context) float64 {
	select {
	case <-ctx.Done():
		return 0
	default:
	}
	const rounds = 200000
	ping := make(chan struct{})
	pong := make(chan struct{})
	done := make(chan struct{})
	go func() {
		// Pin to a dedicated OS thread so each hand-off is a real cross-thread
		// wakeup (futex), not just a same-thread goroutine switch.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		for i := 0; i < rounds; i++ {
			<-ping
			pong <- struct{}{}
		}
		close(done)
	}()
	runtime.LockOSThread()
	start := time.Now()
	for i := 0; i < rounds; i++ {
		ping <- struct{}{}
		<-pong
	}
	elapsed := time.Since(start)
	runtime.UnlockOSThread()
	<-done
	// two switches per round trip
	return float64(elapsed.Nanoseconds()) / float64(rounds) / 2
}

// thermalDropPercent runs all cores flat out for a few seconds and reports how
// far the average clock fell from its early peak.
func thermalDropPercent(ctx context.Context) (float64, bool) {
	peak := avgCPUMHz()
	if peak <= 0 {
		return 0, false
	}
	n := runtime.NumCPU()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					countPrimes(20000)
				}
			}
		}()
	}
	// let it heat up, tracking the peak, then sample the sustained clock
	for i := 0; i < 8; i++ {
		ctxSleep(ctx, 1*time.Second)
		if v := avgCPUMHz(); v > peak {
			peak = v
		}
	}
	sustained := avgCPUMHz()
	close(stop)
	wg.Wait()
	if peak <= 0 || sustained <= 0 {
		return 0, false
	}
	drop := (peak - sustained) / peak * 100
	if drop < 0 {
		drop = 0
	}
	return drop, true
}

func avgCPUMHz() float64 {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	var sum float64
	var n int
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "cpu MHz") {
			if _, v, ok := strings.Cut(ln, ":"); ok {
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					sum += f
					n++
				}
			}
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func stealVerdict(p float64) Verdict {
	switch {
	case p < 1:
		return VGood
	case p < 5:
		return VOkay
	default:
		return VPoor
	}
}
func stealNote(p float64) string {
	switch {
	case p < 1:
		return "bare metal, or a VM with no neighbour contention"
	case p < 5:
		return "some hypervisor contention - noisy neighbours on the host"
	default:
		return "heavy steal - the host is oversubscribed; expect uneven performance"
	}
}
func ctxSwitchVerdict(us float64) Verdict {
	switch {
	case us < 3:
		return VGood
	case us < 12:
		return VOkay
	default:
		return VPoor
	}
}
func ctxSwitchNote(us float64) string {
	switch {
	case us < 3:
		return "fast scheduler - good for many small tasks and lots of connections"
	case us < 12:
		return "ok - some overhead per switch (busy machine or nested virtualisation)"
	default:
		return "slow - a heavily threaded server will spend real time just switching"
	}
}
func thermalVerdict(drop float64) Verdict {
	switch {
	case drop < 5:
		return VGood
	case drop < 20:
		return VOkay
	default:
		return VPoor
	}
}
func thermalNote(drop float64) string {
	switch {
	case drop < 5:
		return "sustains its clock - cooling is adequate"
	case drop < 20:
		return "some throttling under sustained load - short bursts are faster than long jobs"
	default:
		return "throttles hard - long jobs run well below the advertised speed"
	}
}
