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

func TestSeriesAndSummarize(t *testing.T) {
	base := time.Now().Add(-72 * time.Hour)
	mk := func(v float64) Record {
		return Record{Time: base, Kind: "CPU", Host: "h", Depth: "normal",
			Metrics: []Metric{{Name: "AES (x)", Value: v, Unit: "MB/s"}}}
	}
	recs := []Record{}
	for i, v := range []float64{100, 110, 90, 120} {
		r := mk(v)
		r.Time = base.Add(time.Duration(i) * time.Hour)
		recs = append(recs, r)
	}
	pts := Series(recs, "CPU", "AES (something else)", "h") // Key() ignores parens
	if len(pts) != 4 || pts[0].Value != 100 || pts[3].Value != 120 {
		t.Fatalf("series: %+v", pts)
	}
	st := Summarize(pts, false)
	if st.Max.Value != 120 || st.Min.Value != 90 {
		t.Fatalf("minmax: %+v", st)
	}
	if st.OverWindowPct < 19 || st.OverWindowPct > 21 { // 100 -> 120
		t.Fatalf("window pct: %v", st.OverWindowPct)
	}
	if sp := Sparkline(pts, 10); len([]rune(sp)) != 4 {
		t.Fatalf("sparkline %q", sp)
	}
	if AreaFromArg("net") != "Network" || AreaFromArg("bogus") != "" {
		t.Fatal("AreaFromArg")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess := NewSession()
	r := bench.Result{Kind: bench.CPU, Tool: "sysbench", Summary: "ok",
		Metrics: []bench.Metric{{Name: "AES", Display: "1 GB/s", Value: 1000, Unit: "MB/s"}}}
	if err := Save(sess, "host", "quick", "", r, nil); err != nil {
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

func TestDeleteSession(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess1 := NewSession()
	sess2 := NewSession()
	r1 := bench.Result{Kind: bench.CPU, Tool: "test", Metrics: []bench.Metric{{Name: "M1", Value: 10}}}
	r2 := bench.Result{Kind: bench.Disk, Tool: "test", Metrics: []bench.Metric{{Name: "M2", Value: 20}}}

	if err := Save(sess1, "host1", "quick", "", r1, nil); err != nil {
		t.Fatal(err)
	}
	if err := Save(sess2, "host1", "quick", "", r2, nil); err != nil {
		t.Fatal(err)
	}

	recs, err := Load()
	if err != nil || len(recs) != 2 {
		t.Fatalf("expected 2 records before delete, got %d (err: %v)", len(recs), err)
	}

	if err := DeleteSession(sess1); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	recs, err = Load()
	if err != nil {
		t.Fatalf("Load after delete failed: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record after delete, got %d", len(recs))
	}
	if recs[0].Session != sess2 {
		t.Fatalf("expected sess2 to remain, got session %s", recs[0].Session)
	}

	// Deleting a nonexistent session should be a no-op and succeed
	if err := DeleteSession("nonexistent"); err != nil {
		t.Fatalf("DeleteSession nonexistent error: %v", err)
	}
}

func TestRecordTagAndSetTag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PTOP_HISTORY", filepath.Join(dir, "history.jsonl"))

	res := bench.Result{
		Kind: bench.Disk,
		Metrics: []bench.Metric{
			{Name: "Sequential write", Display: "500 MB/s", Value: 500, Unit: "MB/s"},
		},
	}
	sessID := "20260831T200000-abcdef"
	if err := Save(sessID, "testhost", "normal", "initial-tag", res, nil); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	recs, err := Load()
	if err != nil || len(recs) != 1 {
		t.Fatalf("Load failed: %v, len=%d", err, len(recs))
	}
	if recs[0].Tag != "initial-tag" {
		t.Errorf("expected tag 'initial-tag', got %q", recs[0].Tag)
	}

	sessions := Sessions(recs)
	if len(sessions) != 1 || sessions[0].Tag != "initial-tag" {
		t.Errorf("expected session tag 'initial-tag', got %q", sessions[0].Tag)
	}

	if err := SetTag(sessID, "updated-tag"); err != nil {
		t.Fatalf("SetTag failed: %v", err)
	}

	recs2, err := Load()
	if err != nil || len(recs2) != 1 {
		t.Fatalf("Load after SetTag failed: %v", err)
	}
	if recs2[0].Tag != "updated-tag" {
		t.Errorf("expected updated tag 'updated-tag', got %q", recs2[0].Tag)
	}

	// Clearing tag
	if err := SetTag(sessID, ""); err != nil {
		t.Fatalf("SetTag to clear tag failed: %v", err)
	}
	recs3, err := Load()
	if err != nil || len(recs3) != 1 {
		t.Fatalf("Load after clear tag failed: %v", err)
	}
	if recs3[0].Tag != "" {
		t.Errorf("expected empty tag, got %q", recs3[0].Tag)
	}

	// Non-existent session
	if err := SetTag("nonexistent", "tag"); err != nil {
		t.Fatalf("SetTag nonexistent session error: %v", err)
	}
}

func TestDiffSessions(t *testing.T) {
	s1 := Session{
		ID:   "s1",
		Host: "testhost",
		Time: time.Now().Add(-1 * time.Hour),
		Tag:  "base",
		Records: []Record{
			{
				Kind: "Disk",
				Metrics: []Metric{
					{Name: "Sequential write", Display: "500 MB/s", Value: 500, Unit: "MB/s", LowerBetter: false},
					{Name: "Commit latency", Display: "4.0 ms", Value: 4.0, Unit: "ms", LowerBetter: true},
					{Name: "Old only", Display: "10 ops/s", Value: 10, Unit: "ops/s"},
				},
			},
			{
				Kind: "CPU",
				Metrics: []Metric{
					{Name: "Single-thread", Display: "1000", Value: 1000, Unit: "events/s"},
				},
			},
		},
	}
	s2 := Session{
		ID:   "s2",
		Host: "testhost",
		Time: time.Now(),
		Tag:  "upgrade",
		Records: []Record{
			{
				Kind: "Disk",
				Metrics: []Metric{
					{Name: "Sequential write", Display: "1000 MB/s", Value: 1000, Unit: "MB/s", LowerBetter: false},
					{Name: "Commit latency", Display: "2.0 ms", Value: 2.0, Unit: "ms", LowerBetter: true},
					{Name: "New only", Display: "20 ops/s", Value: 20, Unit: "ops/s"},
				},
			},
			{
				Kind: "Memory",
				Metrics: []Metric{
					{Name: "Bandwidth", Display: "25 GB/s", Value: 25, Unit: "GB/s"},
				},
			},
		},
	}

	diff := DiffSessions(s1, s2)
	if diff.Base.ID != "s1" || diff.Target.ID != "s2" {
		t.Fatalf("expected DiffSessions to preserve base and target sessions")
	}

	// Items should include:
	// Disk: Sequential write, Commit latency, Old only, New only (4)
	// CPU: Single-thread (1)
	// Memory: Bandwidth (1)
	// Total: 6 items
	if len(diff.Items) != 6 {
		t.Fatalf("expected 6 diff items, got %d: %+v", len(diff.Items), diff.Items)
	}

	// Sequential write: 500 -> 1000 is +100%
	if diff.Items[0].Delta.Pct != 100 || !diff.Items[0].Delta.Valid {
		t.Errorf("expected +100%% valid for seq write, got %+v", diff.Items[0].Delta)
	}
	if diff.Items[0].BaseDisplay != "500 MB/s" || diff.Items[0].TargDisplay != "1000 MB/s" {
		t.Errorf("expected display values for seq write, got base=%q, targ=%q", diff.Items[0].BaseDisplay, diff.Items[0].TargDisplay)
	}

	// Commit latency: 4.0 -> 2.0 (lower is better) is +50% better
	if diff.Items[1].Delta.Pct != 50 || !diff.Items[1].Delta.Valid {
		t.Errorf("expected +50%% valid for commit latency, got %+v", diff.Items[1].Delta)
	}

	// Old only: base has it, target doesn't
	if diff.Items[2].Name != "Old only" || diff.Items[2].BaseDisplay != "10 ops/s" || diff.Items[2].TargDisplay != "" || diff.Items[2].Delta.Valid {
		t.Errorf("unexpected Old only item: %+v", diff.Items[2])
	}

	// New only: target has it, base doesn't
	if diff.Items[3].Name != "New only" || diff.Items[3].BaseDisplay != "" || diff.Items[3].TargDisplay != "20 ops/s" || diff.Items[3].Delta.Valid {
		t.Errorf("unexpected New only item: %+v", diff.Items[3])
	}

	// Single-thread: base only
	if diff.Items[4].Kind != "CPU" || diff.Items[4].Name != "Single-thread" {
		t.Errorf("unexpected CPU item: %+v", diff.Items[4])
	}

	// Bandwidth: memory only
	if diff.Items[5].Kind != "Memory" || diff.Items[5].Name != "Bandwidth" {
		t.Errorf("unexpected Memory item: %+v", diff.Items[5])
	}
}

