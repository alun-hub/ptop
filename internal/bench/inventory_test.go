package bench

import (
	"testing"
	"time"
)

func TestOSReleasePretty(t *testing.T) {
	in := "NAME=Fedora\nPRETTY_NAME=\"Fedora Linux 44 (Server Edition)\"\nID=fedora\n"
	if got := osReleasePretty(in); got != "Fedora Linux 44 (Server Edition)" {
		t.Errorf("got %q", got)
	}
	if got := osReleasePretty("NAME=Foo\n"); got != "" {
		t.Errorf("missing PRETTY_NAME should yield empty, got %q", got)
	}
}

func TestParseUptime(t *testing.T) {
	if got := parseUptime("1234.56 9876.54"); got != 1234*time.Second {
		t.Errorf("got %v", got)
	}
	if got := parseUptime(""); got != 0 {
		t.Errorf("empty should be 0, got %v", got)
	}
	if got := parseUptime("garbage"); got != 0 {
		t.Errorf("garbage should be 0, got %v", got)
	}
}

func TestBracketChoice(t *testing.T) {
	if got := bracketChoice("mq-deadline kyber [bfq] none"); got != "bfq" {
		t.Errorf("got %q", got)
	}
	if got := bracketChoice("  always madvise never  "); got != "always madvise never" {
		t.Errorf("no brackets should return whole trimmed string, got %q", got)
	}
	if got := bracketChoice(""); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestPickFlags(t *testing.T) {
	got := pickFlags([]string{"fpu", "vme", "aes", "avx2", "hypervisor", "lm"})
	want := []string{"aes", "avx2", "hypervisor"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v (ordered by interestingCPUFlags)", got, want)
		}
	}
}

func TestCloudFromDMI(t *testing.T) {
	cases := []struct {
		fields []string
		want   string
	}{
		{[]string{"Amazon EC2", "t3.medium", "", ""}, "AWS"},
		{[]string{"", "Google Compute Engine", "", ""}, "GCP"},
		{[]string{"Microsoft Corporation", "Virtual Machine", "", ""}, "Azure"},
		{[]string{"", "", "", "7783-7084-3265-9085-8269-3286-77"}, "Azure"},
		{[]string{"Hetzner", "", "", ""}, "Hetzner"},
		{[]string{"HP", "EliteBook", "", ""}, ""},
	}
	for _, c := range cases {
		if got := cloudFromDMI(c.fields); got != c.want {
			t.Errorf("cloudFromDMI(%v) = %q, want %q", c.fields, got, c.want)
		}
	}
}

func TestInventorizeSmoke(t *testing.T) {
	inv := Inventorize()
	if inv.LogicalCPUs < 1 {
		t.Errorf("LogicalCPUs = %d", inv.LogicalCPUs)
	}
	if inv.Arch == "" {
		t.Error("Arch empty")
	}
}
