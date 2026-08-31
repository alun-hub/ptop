package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func runDisk(ctx context.Context, cfg Config, out chan<- Event) (Result, error) {
	dir := cfg.Path
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return Result{}, fmt.Errorf("directory does not exist: %s", dir)
	}
	if have("fio") {
		return diskFio(ctx, cfg, dir, out)
	}
	out <- LogLine{Text: "fio not found - falling back to dd (rougher numbers, affected by cache)"}
	return diskDD(ctx, cfg, dir, out)
}

// ---- fio path -------------------------------------------------------------

type fioReport struct {
	Jobs []fioJob `json:"jobs"`
}
type fioJob struct {
	Read  fioDir `json:"read"`
	Write fioDir `json:"write"`
	Sync  struct {
		LatNs struct {
			Mean float64 `json:"mean"`
		} `json:"lat_ns"`
	} `json:"sync"`
}
type fioDir struct {
	BW   float64 `json:"bw"` // KiB/s
	IOPS float64 `json:"iops"`
	Clat struct {
		Mean       float64            `json:"mean"` // ns
		Percentile map[string]float64 `json:"percentile"`
	} `json:"clat_ns"`
}

func (d fioDir) p99ns() float64 {
	for _, k := range []string{"99.000000", "99.000000000000"} {
		if v, ok := d.Clat.Percentile[k]; ok {
			return v
		}
	}
	return d.Clat.Mean
}

// fioRun runs one fio job and returns jobs[0]. If timed is true the job runs for
// secs seconds; otherwise it runs to completion over its --size.
func fioRun(ctx context.Context, out chan<- Event, dir, name string, secs int, timed bool, extra ...string) (fioJob, error) {
	args := []string{
		"--name=" + name, "--directory=" + dir,
		"--size=" + strconv.Itoa(cfgSizeMB(secs)) + "M",
		"--group_reporting", "--output-format=json",
		// random buffer contents so filesystem compression/dedup (btrfs, zfs)
		// does not inflate the numbers
		"--refill_buffers", "--buffer_compress_percentage=0",
	}
	if timed {
		args = append(args, "--runtime="+strconv.Itoa(secs), "--time_based")
	}
	args = append(args, extra...)
	out <- LogLine{Text: "fio " + strings.Join(args, " ")}
	// fio names its data files "<jobname>.<n>.<m>" in --directory and does not
	// delete them; clean them up ourselves.
	defer func() {
		if files, _ := filepath.Glob(filepath.Join(dir, name+".*.*")); files != nil {
			for _, f := range files {
				_ = os.Remove(f)
			}
		}
	}()
	rawB, err := exec.CommandContext(ctx, "fio", args...).CombinedOutput()
	raw := string(rawB)
	if err != nil {
		return fioJob{}, fmt.Errorf("%v: %s", err, firstLineContaining(raw, "error"))
	}
	i := indexByte(raw, '{')
	if i < 0 {
		return fioJob{}, fmt.Errorf("could not parse fio output")
	}
	var rep fioReport
	if err := json.Unmarshal([]byte(raw[i:]), &rep); err != nil || len(rep.Jobs) == 0 {
		return fioJob{}, fmt.Errorf("could not parse fio output")
	}
	return rep.Jobs[0], nil
}

func diskFio(ctx context.Context, cfg Config, dir string, out chan<- Event) (Result, error) {
	secs := cfg.Depth.Seconds()
	res := Result{Tool: "fio"}

	stop := timeProgress(out, time.Duration(secs)*time.Second, "sequential write")
	jw, err := fioRun(ctx, out, dir, "seqwrite", secs, true, "--rw=write", "--bs=1M", "--direct=1", "--end_fsync=1")
	stop()
	if err != nil {
		return res, err
	}
	out <- Progress{Frac: 0.25, Label: "write done"}

	stop = timeProgress(out, time.Duration(secs)*time.Second, "sequential read")
	jr, err := fioRun(ctx, out, dir, "seqread", secs, true, "--rw=read", "--bs=1M", "--direct=1")
	stop()
	if err != nil {
		return res, err
	}
	out <- Progress{Frac: 0.5, Label: "read done"}

	stop = timeProgress(out, time.Duration(secs)*time.Second, "random 4 KiB I/O")
	jrr, randErr := fioRun(ctx, out, dir, "randrw", secs, true,
		"--rw=randrw", "--rwmixread=70", "--bs=4k", "--iodepth=16",
		"--ioengine=libaio", "--direct=1")
	stop()
	if randErr != nil {
		out <- LogLine{Text: "random test skipped: " + randErr.Error()}
	}
	out <- Progress{Frac: 0.78, Label: "random test done"}

	// fsync/commit latency: how long a durable write takes to hit stable
	// storage. This is what a database waits on for every COMMIT.
	stop = timeProgress(out, 15*time.Second, "commit latency (fsync)")
	jfs, fsErr := fioRun(ctx, out, dir, "fsync", secs, false,
		"--rw=write", "--bs=4k", "--size=128M", "--fdatasync=1", "--iodepth=1")
	stop()
	if fsErr != nil {
		out <- LogLine{Text: "fsync test skipped: " + fsErr.Error()}
	}
	out <- Progress{Frac: 0.80, Label: "fsync test done"}

	metaMetrics, metaErr := diskMetadata(ctx, cfg, dir, out)
	if metaErr != nil {
		out <- LogLine{Text: "metadata test skipped: " + metaErr.Error()}
	}
	out <- Progress{Frac: 1, Label: "done"}

	wMBs := jw.Write.BW / 1024
	rMBs := jr.Read.BW / 1024
	res.Metrics = []Metric{
		seqMetric("Sequential write", wMBs),
		seqMetric("Sequential read", rMBs),
	}
	if randErr == nil {
		res.Metrics = append(res.Metrics,
			iopsMetric("Random read (4 KiB)", jrr.Read.IOPS, noteIOPS(jrr.Read.IOPS)),
			iopsMetric("Random write (4 KiB)", jrr.Write.IOPS, ""),
			latMetric("Random read - worst 1%", jrr.Read.p99ns()),
		)
	}
	if fsErr == nil {
		lat := jfs.Sync.LatNs.Mean
		if lat == 0 {
			lat = jfs.Write.Clat.Mean
		}
		res.Metrics = append(res.Metrics, commitMetric(lat))
	}
	if metaErr == nil {
		res.Metrics = append(res.Metrics, metaMetrics...)
	}
	res.Summary = diskSummary(wMBs, rMBs)
	return res, nil
}

// ---- dd fallback ---------------------------------------------------------

var ddRe = regexp.MustCompile(`([0-9]+) bytes.*copied, ([0-9.,]+) s`)

func ddThroughput(combined string) float64 {
	m := ddRe.FindStringSubmatch(combined)
	if m == nil {
		return 0
	}
	bytes, _ := strconv.ParseFloat(m[1], 64)
	secs, _ := strconv.ParseFloat(replaceComma(m[2]), 64)
	if secs == 0 {
		return 0
	}
	return bytes / secs / 1e6 // MB/s
}

func diskDD(ctx context.Context, cfg Config, dir string, out chan<- Event) (Result, error) {
	res := Result{Tool: "dd (built-in fallback)"}
	sizeMB := cfgSizeMB(cfg.Depth.Seconds())
	if sizeMB > 2048 {
		sizeMB = 2048
	}
	file := filepath.Join(dir, fmt.Sprintf(".ptop-disk-%d.tmp", os.Getpid()))
	defer os.Remove(file)

	stop := timeProgress(out, 15*time.Second, "write")
	wout, werr := streamCmd(ctx, out, "dd", "if=/dev/zero", "of="+file,
		"bs=1M", "count="+strconv.Itoa(sizeMB), "conv=fdatasync", "oflag=direct")
	if werr != nil {
		wout, werr = streamCmd(ctx, out, "dd", "if=/dev/zero", "of="+file,
			"bs=1M", "count="+strconv.Itoa(sizeMB), "conv=fdatasync")
	}
	stop()
	if werr != nil {
		return res, werr
	}
	out <- Progress{Frac: 0.40, Label: "write done"}

	dropCaches(cfg)
	stop = timeProgress(out, 10*time.Second, "read")
	rout, rerr := streamCmd(ctx, out, "dd", "if="+file, "of=/dev/null", "bs=1M", "iflag=direct")
	if rerr != nil {
		rout, rerr = streamCmd(ctx, out, "dd", "if="+file, "of=/dev/null", "bs=1M")
	}
	stop()
	if rerr != nil {
		return res, rerr
	}
	out <- Progress{Frac: 0.80, Label: "read done"}

	metaMetrics, metaErr := diskMetadata(ctx, cfg, dir, out)
	if metaErr != nil {
		out <- LogLine{Text: "metadata test skipped: " + metaErr.Error()}
	}
	out <- Progress{Frac: 1, Label: "done"}

	wMBs := ddThroughput(wout)
	rMBs := ddThroughput(rout)
	res.Metrics = []Metric{
		seqMetric("Sequential write", wMBs),
		seqMetric("Sequential read", rMBs),
	}
	if !cfg.IsRoot {
		res.Metrics[1].Note = "run as root for a fair read test (otherwise the value may come from the RAM cache)"
	}
	if metaErr == nil {
		res.Metrics = append(res.Metrics, metaMetrics...)
	}
	res.Summary = diskSummary(wMBs, rMBs) + " Install fio for random I/O (IOPS) and more stable numbers."
	return res, nil
}

// ---- shared helpers ----------------------------------------------------

func cfgSizeMB(secs int) int {
	// Enough data that the run is I/O bound, capped so we never fill a disk.
	switch {
	case secs <= 10:
		return 2048
	case secs <= 30:
		return 4096
	default:
		return 8192
	}
}

func seqMetric(name string, mbps float64) Metric {
	return Metric{
		Name: name, Display: mbs(mbps), Verdict: verdictSeq(mbps), Note: noteSeq(mbps),
		Bar: normLog(clampLo(mbps, 40), 40, 5000), HasBar: true,
		ScaleLo: "hard disk", ScaleHi: "fast NVMe",
	}.cmp(mbps, "MB/s", false)
}

func iopsMetric(name string, v float64, note string) Metric {
	return Metric{
		Name: name, Display: iops(v), Verdict: verdictIOPS(v), Note: note,
		Bar: normLog(clampLo(v, 100), 100, 200000), HasBar: true,
		ScaleLo: "hard disk", ScaleHi: "fast NVMe",
	}.cmp(v, "IOPS", false)
}

// latMetric renders an I/O latency (nanoseconds in) as a "lower is better" gauge.
func latMetric(name string, ns float64) Metric {
	ms := ns / 1e6
	var v Verdict
	var note string
	switch {
	case ms <= 1:
		v, note = VGood, "very consistent - no stalls"
	case ms <= 10:
		v, note = VOkay, "acceptable - occasional delays"
	case ms > 0:
		v, note = VPoor, "high tail latency - can cause stuttering under load"
	}
	return Metric{
		Name: name, Display: durMS(ms), Verdict: v, Note: note,
		Bar: 1 - normLog(clampLo(ms, 0.05), 0.05, 100), HasBar: true,
		ScaleLo: "slow", ScaleHi: "fast",
	}.cmp(ms, "ms", true)
}

// commitMetric interprets fsync/fdatasync latency (nanoseconds in).
func commitMetric(ns float64) Metric {
	ms := ns / 1e6
	var v Verdict
	var note string
	switch {
	case ms <= 1:
		v, note = VGood, "excellent for databases - fast, durable COMMIT"
	case ms <= 5:
		v, note = VOkay, "fine for most databases"
	case ms > 0:
		v, note = VPoor, "slow durable writes - bottlenecks database COMMIT and logging"
	}
	return Metric{
		Name: "Commit latency (fsync)", Display: durMS(ms), Verdict: v, Note: note,
		Bar: 1 - normLog(clampLo(ms, 0.05), 0.05, 50), HasBar: true,
		ScaleLo: "slow", ScaleHi: "fast",
	}.cmp(ms, "ms", true)
}

func durMS(ms float64) string {
	us := ms * 1000
	switch {
	case us < 1:
		return fmt.Sprintf("%.2f µs", us)
	case us < 10:
		return fmt.Sprintf("%.1f µs", us)
	case ms < 1:
		return fmt.Sprintf("%.0f µs", us)
	case ms < 10:
		return fmt.Sprintf("%.2f ms", ms)
	default:
		return fmt.Sprintf("%.1f ms", ms)
	}
}

func clampLo(v, lo float64) float64 {
	if v < lo {
		return lo
	}
	return v
}

func mbs(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("%.2f GB/s", v/1000)
	}
	return fmt.Sprintf("%.0f MB/s", v)
}
func iops(v float64) string { return fmt.Sprintf("%.0f IOPS", v) }

func verdictSeq(mbps float64) Verdict {
	switch {
	case mbps >= 900:
		return VGood
	case mbps >= 200:
		return VOkay
	case mbps > 0:
		return VPoor
	}
	return VNeutral
}
func noteSeq(mbps float64) string {
	switch {
	case mbps >= 2000:
		return "comparable to a fast NVMe SSD"
	case mbps >= 900:
		return "comparable to an NVMe SSD"
	case mbps >= 400:
		return "comparable to a SATA SSD"
	case mbps >= 150:
		return "comparable to a good SATA SSD or fast hard disk"
	case mbps > 0:
		return "comparable to a hard disk or network storage - low for a local SSD"
	}
	return ""
}
func verdictIOPS(v float64) Verdict {
	switch {
	case v >= 20000:
		return VGood
	case v >= 2000:
		return VOkay
	case v > 0:
		return VPoor
	}
	return VNeutral
}
func noteIOPS(v float64) string {
	switch {
	case v >= 50000:
		return "excellent - fast NVMe"
	case v >= 20000:
		return "good - NVMe class"
	case v >= 2000:
		return "ok - SSD class"
	case v > 0:
		return "low - typical for a hard disk; poor for databases"
	}
	return ""
}
func diskSummary(w, r float64) string {
	if w == 0 && r == 0 {
		return "Could not measure disk speed."
	}
	return fmt.Sprintf("The disk sustains about %s write and %s read sequentially.", mbs(w), mbs(r))
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
func replaceComma(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] == ',' {
			out[i] = '.'
		}
	}
	return string(out)
}
