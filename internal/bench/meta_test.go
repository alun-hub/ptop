package bench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMetaFileCount(t *testing.T) {
	if got := metaFileCount(Quick); got != 2500 {
		t.Errorf("metaFileCount(Quick) = %d, want 2500", got)
	}
	if got := metaFileCount(Normal); got != 10000 {
		t.Errorf("metaFileCount(Normal) = %d, want 10000", got)
	}
	if got := metaFileCount(Deep); got != 25000 {
		t.Errorf("metaFileCount(Deep) = %d, want 25000", got)
	}
	if got := metaFileCount(Depth(999)); got != 10000 {
		t.Errorf("metaFileCount(unknown) = %d, want 10000", got)
	}
}

func TestMetricVerdictsAndNotes(t *testing.T) {
	// Small file creation: >= 10000: VGood, >= 2000: VOkay, else VPoor
	mGood := fileCreateMetric(15000)
	if mGood.Verdict != VGood || mGood.Name != "Small file creation" {
		t.Errorf("expected VGood for 15000 create ops, got %v", mGood.Verdict)
	}
	if mGood.Note != "speed of creating files - matters for git, node_modules, build tools" {
		t.Errorf("unexpected note: %q", mGood.Note)
	}
	if mGood.ScaleLo != "slow" || mGood.ScaleHi != "fast" {
		t.Errorf("unexpected scales: lo=%q, hi=%q", mGood.ScaleLo, mGood.ScaleHi)
	}
	mOkay := fileCreateMetric(5000)
	if mOkay.Verdict != VOkay {
		t.Errorf("expected VOkay for 5000 create ops, got %v", mOkay.Verdict)
	}
	mPoor := fileCreateMetric(1000)
	if mPoor.Verdict != VPoor {
		t.Errorf("expected VPoor for 1000 create ops, got %v", mPoor.Verdict)
	}
	mZero := fileCreateMetric(0)
	if mZero.Verdict != VNeutral {
		t.Errorf("expected VNeutral for 0 create ops, got %v", mZero.Verdict)
	}

	// Small file metadata (stat): >= 50000: VGood, >= 10000: VOkay, else VPoor
	sGood := fileStatMetric(60000)
	if sGood.Verdict != VGood || sGood.Name != "Small file metadata (stat)" {
		t.Errorf("expected VGood for 60000 stat ops, got %v", sGood.Verdict)
	}
	if sGood.Note != "directory traversal and stat speed - affects find, ls, web servers" {
		t.Errorf("unexpected note: %q", sGood.Note)
	}
	sOkay := fileStatMetric(20000)
	if sOkay.Verdict != VOkay {
		t.Errorf("expected VOkay for 20000 stat ops, got %v", sOkay.Verdict)
	}
	sPoor := fileStatMetric(5000)
	if sPoor.Verdict != VPoor {
		t.Errorf("expected VPoor for 5000 stat ops, got %v", sPoor.Verdict)
	}
	sZero := fileStatMetric(0)
	if sZero.Verdict != VNeutral {
		t.Errorf("expected VNeutral for 0 stat ops, got %v", sZero.Verdict)
	}

	// Small file deletion: >= 10000: VGood, >= 2000: VOkay, else VPoor
	dGood := fileDeleteMetric(12000)
	if dGood.Verdict != VGood || dGood.Name != "Small file deletion" {
		t.Errorf("expected VGood for 12000 delete ops, got %v", dGood.Verdict)
	}
	if dGood.Note != "directory unlinking speed - matters for temp file and cache cleanup" {
		t.Errorf("unexpected note: %q", dGood.Note)
	}
	dOkay := fileDeleteMetric(3000)
	if dOkay.Verdict != VOkay {
		t.Errorf("expected VOkay for 3000 delete ops, got %v", dOkay.Verdict)
	}
	dPoor := fileDeleteMetric(800)
	if dPoor.Verdict != VPoor {
		t.Errorf("expected VPoor for 800 delete ops, got %v", dPoor.Verdict)
	}
	dZero := fileDeleteMetric(0)
	if dZero.Verdict != VNeutral {
		t.Errorf("expected VNeutral for 0 delete ops, got %v", dZero.Verdict)
	}
}

func TestDiskMetadata(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Depth: Quick}
	out := make(chan Event, 200)

	done := make(chan struct{})
	var events []Event
	go func() {
		for ev := range out {
			events = append(events, ev)
		}
		close(done)
	}()

	ctx := context.Background()
	metrics, err := diskMetadata(ctx, cfg, dir, out)
	close(out)
	<-done

	if err != nil {
		t.Fatalf("diskMetadata failed: %v", err)
	}

	if len(metrics) != 3 {
		t.Fatalf("expected 3 metrics, got %d", len(metrics))
	}

	expectedNames := []string{
		"Small file creation",
		"Small file metadata (stat)",
		"Small file deletion",
	}

	for i, name := range expectedNames {
		m := metrics[i]
		if m.Name != name {
			t.Errorf("metric[%d] name = %q, want %q", i, m.Name, name)
		}
		if m.Unit != "ops/s" {
			t.Errorf("metric[%d] unit = %q, want 'ops/s'", i, m.Unit)
		}
		if m.Value <= 0 {
			t.Errorf("metric[%d] value = %f, want > 0", i, m.Value)
		}
		if !m.HasBar {
			t.Errorf("metric[%d] expected HasBar to be true", i)
		}
		if m.Verdict == VNeutral {
			t.Errorf("metric[%d] expected non-neutral verdict, got %v", i, m.Verdict)
		}
		if m.Note == "" {
			t.Errorf("metric[%d] expected non-empty note", i)
		}
	}

	// Verify temp directory was cleaned up
	metaDir := filepath.Join(dir, fmt.Sprintf(".ptop-meta-%d", os.Getpid()))
	if _, err := os.Stat(metaDir); !os.IsNotExist(err) {
		t.Errorf("expected temp dir %s to be cleaned up, stat err: %v", metaDir, err)
	}
}

func TestDiskMetadataNilOut(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Depth: Quick}
	ctx := context.Background()
	metrics, err := diskMetadata(ctx, cfg, dir, nil)
	if err != nil {
		t.Fatalf("diskMetadata with nil out failed: %v", err)
	}
	if len(metrics) != 3 {
		t.Fatalf("expected 3 metrics, got %d", len(metrics))
	}
}

func TestDiskMetadataContextCancel(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Depth: Deep}
	out := make(chan Event, 200)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := diskMetadata(ctx, cfg, dir, out)
	close(out)

	if err == nil {
		t.Fatalf("expected error on cancelled context, got nil")
	}

	metaDir := filepath.Join(dir, fmt.Sprintf(".ptop-meta-%d", os.Getpid()))
	if _, err := os.Stat(metaDir); !os.IsNotExist(err) {
		t.Errorf("expected temp dir %s to be cleaned up on cancel, stat err: %v", metaDir, err)
	}
}
