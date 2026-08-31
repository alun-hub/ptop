package bench

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSqliteDuration(t *testing.T) {
	if got := sqliteDuration(Quick); got != 3*time.Second {
		t.Errorf("sqliteDuration(Quick) = %v, want 3s", got)
	}
	if got := sqliteDuration(Normal); got != 5*time.Second {
		t.Errorf("sqliteDuration(Normal) = %v, want 5s", got)
	}
	if got := sqliteDuration(Deep); got != 10*time.Second {
		t.Errorf("sqliteDuration(Deep) = %v, want 10s", got)
	}
	if got := sqliteDuration(Depth(999)); got != 5*time.Second {
		t.Errorf("sqliteDuration(unknown) = %v, want 5s", got)
	}
}

func TestSqliteMetric(t *testing.T) {
	// >= 2000: VGood
	mGood := sqliteMetric(2500)
	if mGood.Name != "Database transactions (SQLite ACID)" {
		t.Errorf("unexpected name: %q", mGood.Name)
	}
	if mGood.Display != "2500 txn/s" {
		t.Errorf("unexpected display: %q", mGood.Display)
	}
	if mGood.Verdict != VGood {
		t.Errorf("expected VGood for 2500 txn/s, got %v", mGood.Verdict)
	}
	if mGood.Note != "synchronous ACID commit rate in WAL mode - bottlenecks relational databases (PostgreSQL, MySQL, SQLite)" {
		t.Errorf("unexpected note: %q", mGood.Note)
	}
	if mGood.ScaleLo != "slow HDD" || mGood.ScaleHi != "fast NVMe" {
		t.Errorf("unexpected scales: lo=%q, hi=%q", mGood.ScaleLo, mGood.ScaleHi)
	}
	if mGood.Unit != "txn/s" {
		t.Errorf("unexpected unit: %q", mGood.Unit)
	}
	if mGood.LowerBetter {
		t.Errorf("expected LowerBetter=false")
	}
	if !mGood.HasBar {
		t.Errorf("expected HasBar=true")
	}

	// >= 400: VOkay
	mOkay := sqliteMetric(1000)
	if mOkay.Verdict != VOkay {
		t.Errorf("expected VOkay for 1000 txn/s, got %v", mOkay.Verdict)
	}

	// > 0: VPoor
	mPoor := sqliteMetric(100)
	if mPoor.Verdict != VPoor {
		t.Errorf("expected VPoor for 100 txn/s, got %v", mPoor.Verdict)
	}

	// <= 0: VNeutral
	mZero := sqliteMetric(0)
	if mZero.Verdict != VNeutral {
		t.Errorf("expected VNeutral for 0 txn/s, got %v", mZero.Verdict)
	}

	// Bar checks
	m50 := sqliteMetric(50)
	if m50.Bar != 0 {
		t.Errorf("expected Bar=0 for 50 txn/s, got %f", m50.Bar)
	}
	m10k := sqliteMetric(10000)
	if m10k.Bar != 1 {
		t.Errorf("expected Bar=1 for 10000 txn/s, got %f", m10k.Bar)
	}
}

func TestDiskSQLitePureGoFallback(t *testing.T) {
	dir := t.TempDir()
	out := make(chan Event, 50)
	done := make(chan struct{})
	go func() {
		for range out {
		}
		close(done)
	}()

	ctx := context.Background()
	txnsPerSec, err := diskSQLitePureGo(ctx, 100*time.Millisecond, dir, out)
	close(out)
	<-done

	if err != nil {
		t.Fatalf("diskSQLitePureGo failed: %v", err)
	}
	if txnsPerSec <= 0 {
		t.Fatalf("expected txnsPerSec > 0, got %f", txnsPerSec)
	}

	// Verify temp files are cleaned up
	walFiles, _ := filepath.Glob(filepath.Join(dir, ".ptop-wal-*"))
	if len(walFiles) > 0 {
		t.Errorf("expected temp wal files to be cleaned up, found: %v", walFiles)
	}
}

func TestDiskSQLiteCLI(t *testing.T) {
	if !have("sqlite3") {
		t.Skip("sqlite3 not installed, skipping CLI test")
	}

	dir := t.TempDir()
	out := make(chan Event, 50)
	done := make(chan struct{})
	go func() {
		for range out {
		}
		close(done)
	}()

	ctx := context.Background()
	txnsPerSec, err := diskSQLiteCLI(ctx, 100*time.Millisecond, dir, out)
	close(out)
	<-done

	if err != nil {
		t.Fatalf("diskSQLiteCLI failed: %v", err)
	}
	if txnsPerSec <= 0 {
		t.Fatalf("expected txnsPerSec > 0, got %f", txnsPerSec)
	}

	// Verify temp files are cleaned up
	sqliteFiles, _ := filepath.Glob(filepath.Join(dir, ".ptop-sqlite-*"))
	if len(sqliteFiles) > 0 {
		t.Errorf("expected temp sqlite files to be cleaned up, found: %v", sqliteFiles)
	}
}

func TestDiskSQLite(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Depth: Quick}
	out := make(chan Event, 50)
	done := make(chan struct{})
	var events []Event
	go func() {
		for ev := range out {
			events = append(events, ev)
		}
		close(done)
	}()

	ctx := context.Background()
	metric, err := diskSQLite(ctx, cfg, dir, out)
	close(out)
	<-done

	if err != nil {
		t.Fatalf("diskSQLite returned unexpected error: %v", err)
	}

	if metric.Name != "Database transactions (SQLite ACID)" {
		t.Errorf("metric.Name = %q, want 'Database transactions (SQLite ACID)'", metric.Name)
	}
	if metric.Unit != "txn/s" {
		t.Errorf("metric.Unit = %q, want 'txn/s'", metric.Unit)
	}
	if metric.Value <= 0 {
		t.Errorf("metric.Value = %f, want > 0", metric.Value)
	}
	if !metric.HasBar {
		t.Errorf("expected metric.HasBar to be true")
	}
	if len(events) == 0 {
		t.Errorf("expected events to be emitted on out channel")
	}

	// Verify all temp files cleaned up
	walFiles, _ := filepath.Glob(filepath.Join(dir, ".ptop-wal-*"))
	if len(walFiles) > 0 {
		t.Errorf("expected temp wal files to be cleaned up, found: %v", walFiles)
	}
	sqliteFiles, _ := filepath.Glob(filepath.Join(dir, ".ptop-sqlite-*"))
	if len(sqliteFiles) > 0 {
		t.Errorf("expected temp sqlite files to be cleaned up, found: %v", sqliteFiles)
	}
}

func TestDiskSQLiteNilOut(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Depth: Quick}
	ctx := context.Background()

	metric, err := diskSQLite(ctx, cfg, dir, nil)
	if err != nil {
		t.Fatalf("diskSQLite with nil out failed: %v", err)
	}
	if metric.Name != "Database transactions (SQLite ACID)" {
		t.Errorf("metric.Name = %q, want 'Database transactions (SQLite ACID)'", metric.Name)
	}
}

func TestDiskSQLiteContextCancel(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Depth: Deep}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := diskSQLite(ctx, cfg, dir, nil)
	if err == nil {
		t.Fatalf("expected error on cancelled context, got nil")
	}

	walFiles, _ := filepath.Glob(filepath.Join(dir, ".ptop-wal-*"))
	if len(walFiles) > 0 {
		t.Errorf("expected temp wal files to be cleaned up on cancel, found: %v", walFiles)
	}
	sqliteFiles, _ := filepath.Glob(filepath.Join(dir, ".ptop-sqlite-*"))
	if len(sqliteFiles) > 0 {
		t.Errorf("expected temp sqlite files to be cleaned up on cancel, found: %v", sqliteFiles)
	}
}
