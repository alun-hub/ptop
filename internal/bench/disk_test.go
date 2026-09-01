package bench

import "testing"

func TestParallelismMetric(t *testing.T) {
	// NVMe-like: 12k -> 300k = 25x -> good
	if m := parallelismMetric(12000, 300000); m.Verdict != VGood || m.Value < 24 || m.Value > 26 {
		t.Errorf("deep parallelism: verdict=%v value=%v", m.Verdict, m.Value)
	}
	// SATA-ish: 8k -> 40k = 5x -> ok
	if m := parallelismMetric(8000, 40000); m.Verdict != VOkay {
		t.Errorf("moderate: verdict=%v", m.Verdict)
	}
	// HDD: 150 -> 200 = 1.3x -> poor (serialised)
	if m := parallelismMetric(150, 200); m.Verdict != VPoor {
		t.Errorf("serial: verdict=%v", m.Verdict)
	}
	// Degenerate input: no QD1 number -> neutral, no panic
	if m := parallelismMetric(0, 1000); m.Verdict != VNeutral || m.Value != 0 {
		t.Errorf("zero qd1: %+v", m)
	}
	if m := parallelismMetric(100, 300); m.Unit != "x" || m.Display != "3.0x" {
		t.Errorf("format: unit=%q display=%q", m.Unit, m.Display)
	}
}

func TestParallelismGradedByProfile(t *testing.T) {
	hdd := DetectProfile(Inventory{Disks: []DiskInfo{{Device: "sda", Rotational: true}}})
	// 1.5x parallelism on an HDD is normal, not "low"
	m := parallelismMetric(150, 225)
	v, note := hdd.Annotate(m)
	if v != VOkay || note == "" {
		t.Errorf("HDD 1.5x parallelism: v=%v note=%q", v, note)
	}
}
