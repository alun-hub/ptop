package bench

import "fmt"

// Profile is the expected-performance envelope for the class of hardware ptop
// is running on. It lets the UI grade a metric against what is normal for, say,
// a spinning disk instead of against an absolute "fast NVMe" scale - so a
// healthy HDD is not perpetually labelled "low".
type Profile struct {
	Storage string // "spinning HDD", "SATA / cloud SSD", "NVMe SSD", "" if unknown
	Compute string // "small cloud VM", "cloud VM", "bare-metal server", "" if unknown
	Summary string // one-line, human: what to expect from this machine

	bands map[string]Band // keyed by exact bench Metric.Name
}

// Band is the range considered normal for one metric, in that metric's stable
// Unit (see Metric.Unit). A value inside [Lo,Hi] grades as "ok"; better than
// the range grades "good"; worse grades "low".
type Band struct {
	Lo, Hi      float64
	LowerBetter bool
	Unit        string
}

// DetectProfile picks the closest matching profile from the inventory.
func DetectProfile(inv Inventory) Profile {
	p := Profile{bands: map[string]Band{}}

	if d := primaryDisk(inv); d != nil {
		switch {
		case d.Rotational:
			p.Storage = "spinning HDD"
			p.merge(map[string]Band{
				"Sequential write":       {60, 200, false, "MB/s"},
				"Sequential read":        {80, 220, false, "MB/s"},
				"Random read (4 KiB)":    {70, 400, false, "IOPS"},
				"Random write (4 KiB)":   {70, 400, false, "IOPS"},
				"Random read - worst 1%": {5, 40, true, "ms"},
				"Commit latency (fsync)": {3, 25, true, "ms"},
			})
		case hasPrefix(d.Device, "nvme"):
			p.Storage = "NVMe SSD"
			p.merge(map[string]Band{
				"Sequential write":       {800, 7000, false, "MB/s"},
				"Sequential read":        {1500, 7000, false, "MB/s"},
				"Random read (4 KiB)":    {20000, 900000, false, "IOPS"},
				"Random write (4 KiB)":   {20000, 600000, false, "IOPS"},
				"Random read - worst 1%": {0.02, 1.0, true, "ms"},
				"Commit latency (fsync)": {0.02, 1.5, true, "ms"},
			})
		default:
			p.Storage = "SATA / cloud SSD"
			p.merge(map[string]Band{
				"Sequential write":       {200, 600, false, "MB/s"},
				"Sequential read":        {300, 600, false, "MB/s"},
				"Random read (4 KiB)":    {2000, 100000, false, "IOPS"},
				"Random write (4 KiB)":   {2000, 90000, false, "IOPS"},
				"Random read - worst 1%": {0.1, 5, true, "ms"},
				"Commit latency (fsync)": {0.1, 6, true, "ms"},
			})
		}
	}

	virtualized := inv.Virtualized || (inv.Virt != "" && inv.Virt != "none")
	switch {
	case virtualized && inv.LogicalCPUs > 0 && inv.LogicalCPUs <= 2:
		p.Compute = "small cloud VM"
	case virtualized:
		p.Compute = "cloud VM"
	case inv.CPUModel != "":
		p.Compute = "bare-metal server"
	}

	p.Summary = profileSummary(p)
	return p
}

func (p *Profile) merge(b map[string]Band) {
	for k, v := range b {
		p.bands[k] = v
	}
}

// Grade compares value (in the metric's stable Unit) against the class band for
// metricName. ok is false when there is no band for that metric.
func (p Profile) Grade(metricName string, value float64) (verdict Verdict, note string, ok bool) {
	b, has := p.bands[metricName]
	if !has || value <= 0 {
		return VNeutral, "", false
	}
	unit := ""
	if b.Unit != "" {
		unit = " " + b.Unit
	}
	rng := fmt.Sprintf("%s class: %s-%s%s", firstNonEmptyProfile(p.Storage, "this hardware"),
		trimNum(b.Lo), trimNum(b.Hi), unit)

	within := value >= b.Lo && value <= b.Hi
	switch {
	case within:
		return VOkay, "normal for " + rng, true
	case b.LowerBetter && value < b.Lo:
		return VGood, "better than " + rng, true
	case !b.LowerBetter && value > b.Hi:
		return VGood, "better than " + rng, true
	default:
		return VPoor, "below " + rng, true
	}
}

// Annotate returns the verdict to show for m given this profile, plus a short
// class note (empty when the profile has nothing to say about m). When there is
// no class band the metric's own absolute verdict is returned unchanged.
func (p Profile) Annotate(m Metric) (Verdict, string) {
	if v, note, ok := p.Grade(m.Name, m.Value); ok {
		return v, note
	}
	return m.Verdict, ""
}

func profileSummary(p Profile) string {
	switch {
	case p.Storage != "" && p.Compute != "":
		return fmt.Sprintf("%s on a %s - verdicts below are graded for this class", p.Storage, p.Compute)
	case p.Storage != "":
		return p.Storage + " - disk verdicts are graded for this class"
	case p.Compute != "":
		return p.Compute
	}
	return ""
}

func hasPrefix(s, pre string) bool { return len(s) >= len(pre) && s[:len(pre)] == pre }

func firstNonEmptyProfile(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// trimNum formats a band edge without trailing noise: "200", "0.02", "900000".
func trimNum(v float64) string {
	if v >= 100 || v == float64(int64(v)) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%g", v)
}
