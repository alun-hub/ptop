package history

import (
	"math"
	"sort"
	"strings"
	"time"
)

// canonical area order, matching bench.Kind.String().
var areaOrder = []string{"Disk", "CPU", "Memory", "Network", "GPU"}

// Areas returns the test areas that have at least one successful record, in
// canonical order.
func Areas(recs []Record) []string {
	seen := map[string]bool{}
	for _, r := range recs {
		if !r.Failed {
			seen[r.Kind] = true
		}
	}
	var out []string
	for _, a := range areaOrder {
		if seen[a] {
			out = append(out, a)
		}
	}
	return out
}

// AreaFromArg resolves a CLI token ("cpu", "net", "memory", ...) to the stored
// area name, or "" if it is not an area.
func AreaFromArg(s string) string {
	switch strings.ToLower(s) {
	case "disk":
		return "Disk"
	case "cpu":
		return "CPU"
	case "mem", "memory", "ram":
		return "Memory"
	case "net", "network":
		return "Network"
	case "gpu":
		return "GPU"
	}
	return ""
}

// MetricInfo names a metric for the overview list.
type MetricInfo struct {
	Name        string
	Unit        string
	LowerBetter bool
}

// MetricNames returns the comparable metrics for an area, ordered by the most
// recent run's order, with any metrics only seen in older runs appended.
func MetricNames(recs []Record, area, host string) []MetricInfo {
	var order []string
	info := map[string]MetricInfo{}
	// newest first so the latest run defines the order
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		if r.Failed || !strings.EqualFold(r.Kind, area) {
			continue
		}
		if host != "" && !strings.EqualFold(r.Host, host) {
			continue
		}
		for _, m := range r.Metrics {
			if m.Value == 0 {
				continue
			}
			k := Key(m.Name)
			if _, ok := info[k]; !ok {
				info[k] = MetricInfo{Name: m.Name, Unit: m.Unit, LowerBetter: m.LowerBetter}
				order = append(order, k)
			}
		}
	}
	out := make([]MetricInfo, 0, len(order))
	for _, k := range order {
		out = append(out, info[k])
	}
	return out
}

// Point is one historical measurement of a metric.
type Point struct {
	Time    time.Time
	Value   float64
	Verdict string
	Depth   string
	Display string
	Note    string
}

// Series returns the time-ordered points for one metric of one area. host ""
// means all hosts.
func Series(recs []Record, area, metric, host string) []Point {
	k := Key(metric)
	var out []Point
	for _, r := range recs {
		if r.Failed || !strings.EqualFold(r.Kind, area) {
			continue
		}
		if host != "" && !strings.EqualFold(r.Host, host) {
			continue
		}
		for _, m := range r.Metrics {
			if m.Value == 0 || Key(m.Name) != k {
				continue
			}
			out = append(out, Point{
				Time: r.Time, Value: m.Value, Verdict: m.Verdict,
				Depth: r.Depth, Display: m.Display, Note: m.Note,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

// Stats summarises a series.
type Stats struct {
	N             int
	First, Last   Point
	Min, Max      Point
	OverWindowPct float64 // signed, + = improved since the first point
	VsPreviousPct float64 // signed, + = improved since the previous point
	HasWindow     bool
	HasPrevious   bool
}

func Summarize(pts []Point, lowerBetter bool) Stats {
	if len(pts) == 0 {
		return Stats{}
	}
	s := Stats{N: len(pts), First: pts[0], Last: pts[len(pts)-1], Min: pts[0], Max: pts[0]}
	for _, p := range pts {
		if p.Value < s.Min.Value {
			s.Min = p
		}
		if p.Value > s.Max.Value {
			s.Max = p
		}
	}
	if len(pts) >= 2 && s.First.Value != 0 {
		s.OverWindowPct = pctChange(s.First.Value, s.Last.Value, lowerBetter)
		s.HasWindow = true
	}
	if len(pts) >= 2 {
		prev := pts[len(pts)-2]
		if prev.Value != 0 {
			s.VsPreviousPct = pctChange(prev.Value, s.Last.Value, lowerBetter)
			s.HasPrevious = true
		}
	}
	return s
}

func pctChange(old, new float64, lowerBetter bool) float64 {
	if old == 0 {
		return 0
	}
	if lowerBetter {
		return (old - new) / old * 100
	}
	return (new - old) / old * 100
}

// Sparkline renders values as a single row of block characters, scaled to the
// series' own min..max so relative movement is visible even for flat series.
func Sparkline(pts []Point, width int) string {
	if len(pts) == 0 {
		return ""
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	vals := pts
	if len(pts) > width && width > 0 {
		vals = pts[len(pts)-width:]
	}
	min, max := vals[0].Value, vals[0].Value
	for _, p := range vals {
		min = math.Min(min, p.Value)
		max = math.Max(max, p.Value)
	}
	var b strings.Builder
	for _, p := range vals {
		idx := 0
		if max > min {
			idx = int((p.Value - min) / (max - min) * float64(len(blocks)-1))
		}
		b.WriteRune(blocks[idx])
	}
	return b.String()
}
