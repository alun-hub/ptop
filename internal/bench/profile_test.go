package bench

import "testing"

func TestDetectProfileStorage(t *testing.T) {
	hdd := DetectProfile(Inventory{Disks: []DiskInfo{{Device: "sda", SizeGB: 500, Rotational: true}}})
	if hdd.Storage != "spinning HDD" {
		t.Fatalf("Storage = %q", hdd.Storage)
	}
	nvme := DetectProfile(Inventory{Disks: []DiskInfo{{Device: "nvme0n1", SizeGB: 1000}}})
	if nvme.Storage != "NVMe SSD" {
		t.Fatalf("Storage = %q", nvme.Storage)
	}
	sata := DetectProfile(Inventory{Disks: []DiskInfo{{Device: "vda", SizeGB: 80}}})
	if sata.Storage != "SATA / cloud SSD" {
		t.Fatalf("Storage = %q", sata.Storage)
	}
	if DetectProfile(Inventory{}).Storage != "" {
		t.Error("no disk should give empty Storage")
	}
}

func TestDetectProfileCompute(t *testing.T) {
	cases := []struct {
		inv  Inventory
		want string
	}{
		{Inventory{Virt: "kvm", LogicalCPUs: 2}, "small cloud VM"},
		{Inventory{Virt: "kvm", LogicalCPUs: 16}, "cloud VM"},
		{Inventory{Virtualized: true, LogicalCPUs: 8}, "cloud VM"},
		{Inventory{CPUModel: "Xeon", Virt: "none", LogicalCPUs: 32}, "bare-metal server"},
	}
	for _, c := range cases {
		if got := DetectProfile(c.inv).Compute; got != c.want {
			t.Errorf("compute for %+v = %q, want %q", c.inv, got, c.want)
		}
	}
}

func TestProfileGradeHDD(t *testing.T) {
	p := DetectProfile(Inventory{Disks: []DiskInfo{{Device: "sda", Rotational: true, SizeGB: 500}}})

	// 140 MB/s sequential write: healthy for an HDD -> ok, not "low"
	v, note, ok := p.Grade("Sequential write", 140)
	if !ok || v != VOkay {
		t.Fatalf("140 MB/s HDD: v=%v ok=%v note=%q", v, ok, note)
	}
	// 900 MB/s on an HDD is impossible-good -> good
	if v, _, _ := p.Grade("Sequential write", 900); v != VGood {
		t.Errorf("900 MB/s: v=%v", v)
	}
	// 20 MB/s -> below class -> low
	if v, _, _ := p.Grade("Sequential write", 20); v != VPoor {
		t.Errorf("20 MB/s: v=%v", v)
	}
	// lower-better: 8 ms fsync is in-band, 1 ms is better, 40 ms is worse
	if v, _, _ := p.Grade("Commit latency (fsync)", 8); v != VOkay {
		t.Errorf("fsync 8ms: %v", v)
	}
	if v, _, _ := p.Grade("Commit latency (fsync)", 1); v != VGood {
		t.Errorf("fsync 1ms: %v", v)
	}
	if v, _, _ := p.Grade("Commit latency (fsync)", 40); v != VPoor {
		t.Errorf("fsync 40ms: %v", v)
	}
}

func TestProfileGradeUnknownMetric(t *testing.T) {
	p := DetectProfile(Inventory{Disks: []DiskInfo{{Device: "sda", Rotational: true}}})
	if _, _, ok := p.Grade("Single-threaded", 1234); ok {
		t.Error("no band for CPU metric, ok should be false")
	}
	// Annotate falls back to the metric's own verdict
	m := Metric{Name: "Single-threaded", Value: 1234, Verdict: VGood}
	if v, extra := p.Annotate(m); v != VGood || extra != "" {
		t.Errorf("Annotate fallback: v=%v extra=%q", v, extra)
	}
}
