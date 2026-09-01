package bench

import "testing"

func hasTitle(adv []Advice, substr string) *Advice {
	for i := range adv {
		if containsFold(adv[i].Title, substr) {
			return &adv[i]
		}
	}
	return nil
}

func containsFold(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexFold(s, sub) >= 0)
}

func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if eqFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func eqFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 32
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func TestAdviseGovernor(t *testing.T) {
	adv := Advise(Inventory{Governor: "powersave", MemTotalGB: 16})
	a := hasTitle(adv, "governor is powersave")
	if a == nil {
		t.Fatal("expected governor advice")
	}
	if a.Severity != SevWarn || a.Fix == "" {
		t.Errorf("got sev=%d fix=%q", a.Severity, a.Fix)
	}
	if hasTitle(Advise(Inventory{Governor: "performance"}), "governor") != nil {
		t.Error("performance governor should not be flagged")
	}
}

func TestAdviseTHPandSwappiness(t *testing.T) {
	adv := Advise(Inventory{THP: "always", Swappiness: 60, MemTotalGB: 32})
	if hasTitle(adv, "huge pages set to 'always'") == nil {
		t.Error("expected THP advice")
	}
	sw := hasTitle(adv, "swappiness is 60")
	if sw == nil || sw.Severity != SevWarn {
		t.Errorf("expected swappiness warn, got %+v", sw)
	}
	// low swappiness or little RAM: no advice
	if hasTitle(Advise(Inventory{Swappiness: 10, MemTotalGB: 32}), "swappiness") != nil {
		t.Error("swappiness 10 should not be flagged")
	}
	if hasTitle(Advise(Inventory{Swappiness: 60, MemTotalGB: 2}), "swappiness") != nil {
		t.Error("swappiness on tiny-RAM host should not be flagged")
	}
}

func TestAdviseDiskScheduler(t *testing.T) {
	hdd := Advise(Inventory{Disks: []DiskInfo{{Device: "sda", SizeGB: 500, Rotational: true, Scheduler: "none"}}})
	if hasTitle(hdd, "spinning HDD") == nil || hasTitle(hdd, "scheduler 'none' on HDD") == nil {
		t.Errorf("expected HDD advice, got %d items", len(hdd))
	}
	ssd := Advise(Inventory{Disks: []DiskInfo{{Device: "nvme0n1", SizeGB: 1000, Scheduler: "bfq"}}})
	if hasTitle(ssd, "'bfq' on SSD") == nil {
		t.Error("expected bfq-on-SSD advice")
	}
}

func TestAdviseOrderedBySeverity(t *testing.T) {
	adv := Advise(Inventory{
		Governor: "powersave", MemTotalGB: 16, // warn
		NUMANodes: 2, // info
	})
	if len(adv) < 2 {
		t.Fatalf("want >=2 advices, got %d", len(adv))
	}
	for i := 1; i < len(adv); i++ {
		if adv[i-1].Severity < adv[i].Severity {
			t.Errorf("advice not sorted by severity desc: %v", adv)
		}
	}
}
