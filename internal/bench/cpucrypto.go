package bench

import (
	"compress/flate"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"time"
)

// cpuThroughputMetrics runs single-threaded real-work microbenchmarks: AES-GCM
// encryption (TLS, disk encryption) and deflate compression (log shipping,
// backups, zram).
func cpuThroughputMetrics(ctx context.Context) []Metric {
	var ms []Metric

	if v := aesThroughputMBs(ctx); v > 0 {
		ms = append(ms, Metric{
			Name: "AES-256-GCM encryption", Display: mbs(v),
			Verdict: aesVerdict(v), Note: aesNote(v),
			Bar: normLog(clampLo(v, 50), 50, 8000), HasBar: true,
			ScaleLo: "software AES", ScaleHi: "AES-NI",
		})
	}
	if v := deflateThroughputMBs(ctx); v > 0 {
		ms = append(ms, Metric{
			Name: "Compression (deflate, fast)", Display: mbs(v),
			Verdict: compVerdict(v), Note: "throughput of the input stream at zlib level 1 - matters for log shipping and backups",
			Bar: normLog(clampLo(v, 20), 20, 600), HasBar: true,
			ScaleLo: "slow", ScaleHi: "fast",
		})
	}
	return ms
}

func aesThroughputMBs(ctx context.Context) float64 {
	block, err := aes.NewCipher(make([]byte, 32))
	if err != nil {
		return 0
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return 0
	}
	nonce := make([]byte, gcm.NonceSize())
	plain := make([]byte, 1<<20)
	dst := make([]byte, 0, len(plain)+gcm.Overhead())

	start := time.Now()
	deadline := start.Add(1200 * time.Millisecond)
	var n int64
	for time.Now().Before(deadline) {
		dst = gcm.Seal(dst[:0], nonce, plain, nil)
		n += int64(len(plain))
		select {
		case <-ctx.Done():
			return 0
		default:
		}
	}
	return float64(n) / time.Since(start).Seconds() / 1e6
}

func deflateThroughputMBs(ctx context.Context) float64 {
	// semi-compressible: mostly structured with ~15% noise
	src := make([]byte, 4<<20)
	rng := rand.New(rand.NewSource(1))
	for i := range src {
		if i%7 == 0 {
			src[i] = byte(rng.Intn(256))
		} else {
			src[i] = byte(i)
		}
	}
	w, err := flate.NewWriter(io.Discard, flate.BestSpeed)
	if err != nil {
		return 0
	}
	start := time.Now()
	deadline := start.Add(1200 * time.Millisecond)
	var n int64
	for time.Now().Before(deadline) {
		if _, err := w.Write(src); err != nil {
			return 0
		}
		w.Flush()
		n += int64(len(src))
		select {
		case <-ctx.Done():
			return 0
		default:
		}
	}
	return float64(n) / time.Since(start).Seconds() / 1e6
}

// forkExecRate serially spawns /bin/true for ~1s and returns processes/second.
func forkExecRate(ctx context.Context) float64 {
	bin := ""
	for _, c := range []string{"/bin/true", "/usr/bin/true"} {
		if _, err := os.Stat(c); err == nil {
			bin = c
			break
		}
	}
	if bin == "" {
		return 0
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	start := time.Now()
	deadline := start.Add(1 * time.Second)
	var n int64
	for time.Now().Before(deadline) {
		if exec.CommandContext(c, bin).Run() != nil {
			break
		}
		n++
	}
	el := time.Since(start).Seconds()
	if el <= 0 {
		return 0
	}
	return float64(n) / el
}

func aesVerdict(mbps float64) Verdict {
	switch {
	case mbps >= 1500:
		return VGood
	case mbps >= 300:
		return VOkay
	default:
		return VPoor
	}
}
func aesNote(mbps float64) string {
	switch {
	case mbps >= 1500:
		return "hardware AES (AES-NI) - TLS and disk encryption are nearly free"
	case mbps >= 300:
		return "moderate - hardware AES present but a slower core, or a busy machine"
	default:
		return "software AES - no AES-NI; encryption will noticeably tax this CPU"
	}
}
func compVerdict(mbps float64) Verdict {
	switch {
	case mbps >= 150:
		return VGood
	case mbps >= 50:
		return VOkay
	default:
		return VPoor
	}
}
func forkExecVerdict(r float64) Verdict {
	switch {
	case r >= 2000:
		return VGood
	case r >= 500:
		return VOkay
	default:
		return VPoor
	}
}
func forkExecNote(r float64) string {
	switch {
	case r >= 2000:
		return "fast process creation - good for CI, build systems and shell-heavy work"
	case r >= 500:
		return "ok - noticeable cost for scripts that spawn many short-lived processes"
	default:
		return "slow - heavy virtualisation, a busy host, or a security module (auditd/SELinux) in the path"
	}
}
