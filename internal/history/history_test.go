package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ptop/internal/bench"
)

func TestKeyStripsParens(t *testing.T) {
	if got := Key("TCP connect latency (192.168.1.1:443)"); got != "tcp connect latency" {
		t.Fatalf("Key = %q", got)
	}
	if got := Key("Multi-threaded (12 threads)"); got != "multi-threaded" {
		t.Fatalf("Key = %q", got)
	}
}

func TestCompareDirection(t *testing.T) {
	base := &Record{Metrics: []Metric{
		{Name: "AES", Value: 100, LowerBetter: false},
		{Name: "Latency", Value: 10, LowerBetter: true},
	}}
	// higher-better, went up 20% -> +20 faster
	if d := Compare(base, "AES", 120, false); !d.Valid || d.Pct < 19 || d.Pct > 21 {
		t.Fatalf("AES delta = %+v", d)
	}
	// lower-better, went down to 8 -> improved 20%
	if d := Compare(base, "Latency", 8, true); !d.Valid || d.Pct < 19 || d.Pct > 21 {
		t.Fatalf("Latency delta = %+v", d)
	}
	// lower-better, went up to 12 -> -20 (slower)
	if d := Compare(base, "Latency", 12, true); !d.Valid || d.Pct > -19 || d.Pct < -21 {
		t.Fatalf("Latency worse delta = %+v", d)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess := NewSession()
	r := bench.Result{Kind: bench.CPU, Tool: "sysbench", Summary: "ok",
		Metrics: []bench.Metric{{Name: "AES", Display: "1 GB/s", Value: 1000, Unit: "MB/s"}}}
	if err := Save(sess, "host", "quick", r, nil); err != nil {
		t.Fatal(err)
	}
	recs, err := Load()
	if err != nil || len(recs) != 1 {
		t.Fatalf("load: %v %d", err, len(recs))
	}
	if recs[0].Session != sess || recs[0].Metrics[0].Value != 1000 {
		t.Fatalf("bad record: %+v", recs[0])
	}

	ss := Sessions(recs)
	if len(ss) != 1 || ss[0].Time.After(time.Now()) {
		t.Fatalf("sessions: %+v", ss)
	}
	_ = os.Remove(p)
}
