package bench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func metaFileCount(d Depth) int {
	switch d {
	case Quick:
		return 2500
	case Deep:
		return 25000
	default:
		return 10000
	}
}

// diskMetadata benchmarks small file creation, metadata/stat access, and deletion.
func diskMetadata(ctx context.Context, cfg Config, dir string, out chan<- Event) ([]Metric, error) {
	n := metaFileCount(cfg.Depth)
	tempDir := filepath.Join(dir, fmt.Sprintf(".ptop-meta-%d", os.Getpid()))
	_ = os.RemoveAll(tempDir)
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	payload := make([]byte, 512)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	paths := make([]string, n)
	for i := 0; i < n; i++ {
		paths[i] = filepath.Join(tempDir, strconv.Itoa(i))
	}

	// Phase 1: Create
	if out != nil {
		out <- LogLine{Text: fmt.Sprintf("metadata benchmark: creating %d small files...", n)}
		out <- Progress{Frac: 0.82, Label: "metadata: creating files"}
	}
	startCreate := time.Now()
	for i := 0; i < n; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := os.WriteFile(paths[i], payload, 0o644); err != nil {
			return nil, err
		}
	}
	createDur := time.Since(startCreate)
	if createDur <= 0 {
		createDur = time.Microsecond
	}
	createOps := float64(n) / createDur.Seconds()
	if out != nil {
		out <- LogLine{Text: fmt.Sprintf("metadata benchmark: created %d files in %v (%.0f files/s)", n, createDur.Round(time.Millisecond), createOps)}
	}

	// Phase 2: Stat & Read
	if out != nil {
		out <- LogLine{Text: fmt.Sprintf("metadata benchmark: stat and reading %d small files...", n)}
		out <- Progress{Frac: 0.88, Label: "metadata: reading files"}
	}
	startStat := time.Now()
	var oneByte [1]byte
	for i := 0; i < n; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := os.Stat(paths[i]); err != nil {
			return nil, err
		}
		f, err := os.Open(paths[i])
		if err != nil {
			return nil, err
		}
		_, _ = f.Read(oneByte[:])
		_ = f.Close()
	}
	statDur := time.Since(startStat)
	if statDur <= 0 {
		statDur = time.Microsecond
	}
	statOps := float64(n) / statDur.Seconds()
	if out != nil {
		out <- LogLine{Text: fmt.Sprintf("metadata benchmark: stat & read %d files in %v (%.0f ops/s)", n, statDur.Round(time.Millisecond), statOps)}
	}

	// Phase 3: Delete
	if out != nil {
		out <- LogLine{Text: fmt.Sprintf("metadata benchmark: deleting %d small files...", n)}
		out <- Progress{Frac: 0.94, Label: "metadata: deleting files"}
	}
	startDelete := time.Now()
	for i := 0; i < n; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := os.Remove(paths[i]); err != nil {
			return nil, err
		}
	}
	deleteDur := time.Since(startDelete)
	if deleteDur <= 0 {
		deleteDur = time.Microsecond
	}
	deleteOps := float64(n) / deleteDur.Seconds()
	if out != nil {
		out <- LogLine{Text: fmt.Sprintf("metadata benchmark: deleted %d files in %v (%.0f deletions/s)", n, deleteDur.Round(time.Millisecond), deleteOps)}
	}

	metrics := []Metric{
		fileCreateMetric(createOps),
		fileStatMetric(statOps),
		fileDeleteMetric(deleteOps),
	}
	return metrics, nil
}

func fileCreateMetric(v float64) Metric {
	var verdict Verdict
	switch {
	case v >= 10000:
		verdict = VGood
	case v >= 2000:
		verdict = VOkay
	case v > 0:
		verdict = VPoor
	default:
		verdict = VNeutral
	}
	return Metric{
		Name:    "Small file creation",
		Display: fmt.Sprintf("%.0f ops/s", v),
		Verdict: verdict,
		Note:    "speed of creating files - matters for git, node_modules, build tools",
		Bar:     normLog(clampLo(v, 500), 500, 50000),
		HasBar:  true,
		ScaleLo: "slow",
		ScaleHi: "fast",
	}.cmp(v, "ops/s", false)
}

func fileStatMetric(v float64) Metric {
	var verdict Verdict
	switch {
	case v >= 50000:
		verdict = VGood
	case v >= 10000:
		verdict = VOkay
	case v > 0:
		verdict = VPoor
	default:
		verdict = VNeutral
	}
	return Metric{
		Name:    "Small file metadata (stat)",
		Display: fmt.Sprintf("%.0f ops/s", v),
		Verdict: verdict,
		Note:    "directory traversal and stat speed - affects find, ls, web servers",
		Bar:     normLog(clampLo(v, 2000), 2000, 200000),
		HasBar:  true,
		ScaleLo: "slow",
		ScaleHi: "fast",
	}.cmp(v, "ops/s", false)
}

func fileDeleteMetric(v float64) Metric {
	var verdict Verdict
	switch {
	case v >= 10000:
		verdict = VGood
	case v >= 2000:
		verdict = VOkay
	case v > 0:
		verdict = VPoor
	default:
		verdict = VNeutral
	}
	return Metric{
		Name:    "Small file deletion",
		Display: fmt.Sprintf("%.0f ops/s", v),
		Verdict: verdict,
		Note:    "directory unlinking speed - matters for temp file and cache cleanup",
		Bar:     normLog(clampLo(v, 500), 500, 50000),
		HasBar:  true,
		ScaleLo: "slow",
		ScaleHi: "fast",
	}.cmp(v, "ops/s", false)
}
