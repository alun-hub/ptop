package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"ptop/internal/bench"
	"ptop/internal/history"
)

func runCLI(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	kinds, ok := parseKinds(args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown test: %s\n", args[0])
		return 2
	}

	cfg := bench.Config{Depth: bench.Normal, IsRoot: os.Geteuid() == 0}
	cfg.Path, _ = os.Getwd()
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--depth":
			i++
			switch get(args, i) {
			case "quick":
				cfg.Depth = bench.Quick
			case "deep":
				cfg.Depth = bench.Deep
			case "normal", "":
				cfg.Depth = bench.Normal
			default:
				fmt.Fprintln(os.Stderr, "invalid --depth")
				return 2
			}
		case "--path":
			i++
			cfg.Path = get(args, i)
		case "--host":
			i++
			cfg.Host = get(args, i)
		case "--url":
			i++
			cfg.URL = get(args, i)
		case "--port":
			i++
			cfg.Port, _ = strconv.Atoi(get(args, i))
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", args[i])
			return 2
		}
	}

	session := history.NewSession()
	host, _ := os.Hostname()
	depth := cfg.Depth.Token()
	hist, _ := history.Load()

	rc := 0
	for _, k := range kinds {
		c := cfg
		c.Kind = k
		if !runOne(c, session, host, depth, hist) {
			rc = 1
		}
	}
	return rc
}

func runOne(c bench.Config, session, host, depth string, hist []history.Record) bool {
	fmt.Printf("\n== %s ==\n", c.Kind)
	ch := make(chan bench.Event, 128)
	go bench.Run(context.Background(), c, ch)
	ok := true
	for ev := range ch {
		switch e := ev.(type) {
		case bench.LogLine:
			fmt.Printf("  %s\n", e.Text)
		case bench.Finished:
			if e.Err != nil {
				fmt.Printf("  ERROR: %v\n", e.Err)
				ok = false
			}
			base := history.Baseline(hist, c.Kind.String(), host, session)
			printResult(e.Result, base)
			if err := history.Save(session, host, depth, e.Result, e.Err); err != nil {
				fmt.Fprintf(os.Stderr, "  (could not save to history: %v)\n", err)
			}
		}
	}
	return ok
}

func printResult(r bench.Result, base *history.Record) {
	if r.Tool != "" {
		fmt.Printf("  (tool: %s)\n", r.Tool)
	}
	for _, m := range r.Metrics {
		v := ""
		if m.Verdict != bench.VNeutral {
			v = " [" + m.Verdict.Label() + "]"
		}
		delta := ""
		if d := history.Compare(base, m.Name, m.Value, m.LowerBetter); d.Valid {
			delta = "   (" + d.Label() + ")"
		}
		fmt.Printf("  %-30s %s%s%s\n", m.Name, m.Display, v, delta)
		if m.Note != "" {
			fmt.Printf("  %-30s   %s\n", "", m.Note)
		}
	}
	if r.Summary != "" {
		fmt.Printf("\n  → %s\n", r.Summary)
	}
}

func parseKinds(s string) ([]bench.Kind, bool) {
	switch s {
	case "disk":
		return []bench.Kind{bench.Disk}, true
	case "cpu":
		return []bench.Kind{bench.CPU}, true
	case "mem":
		return []bench.Kind{bench.Mem}, true
	case "net":
		return []bench.Kind{bench.Net}, true
	case "gpu":
		return []bench.Kind{bench.GPU}, true
	case "all":
		return []bench.Kind{bench.Disk, bench.CPU, bench.Mem, bench.Net, bench.GPU}, true
	}
	return nil, false
}

func get(a []string, i int) string {
	if i < len(a) {
		return a[i]
	}
	return ""
}

func runHistory(args []string) int {
	recs, err := history.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	sessions := history.Sessions(recs)
	if len(sessions) == 0 {
		fmt.Println("No history yet. Run a test - results are saved to")
		fmt.Println("  " + history.Path())
		return 0
	}

	if len(args) == 0 {
		fmt.Printf("%-3s  %-19s  %-16s  %-8s  %s\n", "#", "when", "host", "depth", "tests")
		for i, s := range sessions {
			fmt.Printf("%-3d  %-19s  %-16s  %-8s  %s\n", i+1,
				s.Time.Local().Format("2006-01-02 15:04:05"),
				cut(s.Host, 16), s.Depth, strings.Join(s.Kinds(), ", "))
		}
		fmt.Println("\nptop history <#>            show a run (vs the run before it)")
		fmt.Println("ptop history <area>        a test area's metrics over time  (cpu|disk|mem|net|gpu)")
		fmt.Println("ptop history <area> <name> one metric's full history")
		fmt.Println("ptop history rm <#>             delete a run from history")
		return 0
	}

	if args[0] == "rm" || args[0] == "delete" {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: ptop history rm <#|session-id>")
			return 2
		}
		var sel *history.Session
		if n, e := strconv.Atoi(args[1]); e == nil && n >= 1 && n <= len(sessions) {
			sel = &sessions[n-1]
		} else {
			for i := range sessions {
				if strings.HasPrefix(sessions[i].ID, args[1]) {
					sel = &sessions[i]
					break
				}
			}
		}
		if sel == nil {
			fmt.Fprintf(os.Stderr, "no such run: %s\n", args[1])
			return 2
		}
		if err := history.DeleteSession(sel.ID); err != nil {
			fmt.Fprintf(os.Stderr, "could not delete run: %v\n", err)
			return 1
		}
		fmt.Printf("Deleted run %s (%s, %s)\n", sel.ID, sel.Time.Local().Format("2006-01-02 15:04:05"), sel.Host)
		return 0
	}

	if area := history.AreaFromArg(args[0]); area != "" {
		return runHistoryArea(recs, area, args[1:])
	}

	var sel *history.Session
	if n, e := strconv.Atoi(args[0]); e == nil && n >= 1 && n <= len(sessions) {
		sel = &sessions[n-1]
	} else {
		for i := range sessions {
			if strings.HasPrefix(sessions[i].ID, args[0]) {
				sel = &sessions[i]
				break
			}
		}
	}
	if sel == nil {
		fmt.Fprintf(os.Stderr, "no such run: %s\n", args[0])
		return 2
	}

	var older []history.Record
	for _, r := range recs {
		if r.Time.Before(sel.Time) {
			older = append(older, r)
		}
	}
	fmt.Printf("Run %s  -  %s  %s  depth %s\n", sel.ID,
		sel.Time.Local().Format("2006-01-02 15:04:05"), sel.Host, sel.Depth)
	for _, r := range sel.Records {
		fmt.Printf("\n== %s ==\n", r.Kind)
		if r.Tool != "" {
			fmt.Printf("  (tool: %s)\n", r.Tool)
		}
		if r.Failed {
			fmt.Printf("  ERROR: %s\n", r.Error)
		}
		base := history.Baseline(older, r.Kind, r.Host, sel.ID)
		for _, m := range r.Metrics {
			v := ""
			if m.Verdict != "" {
				v = " [" + m.Verdict + "]"
			}
			delta := ""
			if d := history.Compare(base, m.Name, m.Value, m.LowerBetter); d.Valid {
				delta = "   (" + d.Label() + ")"
			}
			fmt.Printf("  %-30s %s%s%s\n", m.Name, m.Display, v, delta)
		}
		if r.Summary != "" {
			fmt.Printf("\n  → %s\n", r.Summary)
		}
	}
	return 0
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func runHistoryArea(recs []history.Record, area string, rest []string) int {
	host, _ := os.Hostname()
	// drop an "all" / "--all-hosts" trailing token
	if len(rest) > 0 && (rest[len(rest)-1] == "all" || rest[len(rest)-1] == "--all-hosts") {
		host = ""
		rest = rest[:len(rest)-1]
	}
	metrics := history.MetricNames(recs, area, host)
	if len(metrics) == 0 && host != "" {
		if all := history.MetricNames(recs, area, ""); len(all) > 0 {
			fmt.Printf("(no %s history for this host - showing all hosts)\n\n", area)
			host, metrics = "", all
		}
	}
	if len(metrics) == 0 {
		fmt.Printf("No %s history recorded.\n", area)
		return 0
	}

	// one metric requested
	if len(rest) > 0 {
		q := strings.ToLower(strings.Join(rest, " "))
		var sel *history.MetricInfo
		for i := range metrics {
			if strings.Contains(strings.ToLower(metrics[i].Name), q) {
				sel = &metrics[i]
				break
			}
		}
		if sel == nil {
			fmt.Fprintf(os.Stderr, "no %s metric matching %q\n", area, q)
			return 2
		}
		pts := history.Series(recs, area, sel.Name, host)
		st := history.Summarize(pts, sel.LowerBetter)
		fmt.Printf("%s · %s   (%s, %s)\n", area, sel.Name, orDash(sel.Unit), hostLabel(host))
		fmt.Printf("  %s   %s\n\n", history.Sparkline(pts, 60), trendText(st))
		fmt.Printf("  %-17s %-14s %-9s %-8s %s\n", "run", "value", "Δ prev", "verdict", "depth")
		for i := len(pts) - 1; i >= 0; i-- {
			p := pts[i]
			d := ""
			if i > 0 && pts[i-1].Value != 0 {
				pc := (p.Value - pts[i-1].Value) / pts[i-1].Value * 100
				if sel.LowerBetter {
					pc = -pc
				}
				d = fmt.Sprintf("%+.1f%%", pc)
			}
			fmt.Printf("  %-17s %-14s %-9s %-8s %s\n",
				p.Time.Local().Format("Jan 2 15:04"), p.Display, d, p.Verdict, p.Depth)
		}
		return 0
	}

	// area overview
	span := ""
	if s := history.Series(recs, area, metrics[0].Name, host); len(s) > 0 {
		span = s[0].Time.Local().Format("Jan 2") + " → " + s[len(s)-1].Time.Local().Format("Jan 2")
	}
	fmt.Printf("%s history · %s · %s\n\n", area, hostLabel(host), span)
	fmt.Printf("  %-28s %-14s %-22s %s\n", "metric", "latest", "trend", "")
	for _, mi := range metrics {
		pts := history.Series(recs, area, mi.Name, host)
		st := history.Summarize(pts, mi.LowerBetter)
		latest := ""
		if st.N > 0 {
			latest = st.Last.Display
		}
		fmt.Printf("  %-28s %-14s %-22s %s\n",
			cut(mi.Name, 28), latest, history.Sparkline(pts, 20), trendText(st))
	}
	fmt.Printf("\nptop history %s \"<metric>\"   full history of one metric\n", strings.ToLower(area))
	return 0
}

func trendText(st history.Stats) string {
	if !st.HasWindow {
		return "first run"
	}
	p := st.OverWindowPct
	if p > -1 && p < 1 {
		return fmt.Sprintf("flat over %d runs", st.N)
	}
	s := fmt.Sprintf("%+.0f%% over %d runs", p, st.N)
	if p <= -10 {
		s += "  (!)"
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func hostLabel(h string) string {
	if h == "" {
		return "all hosts"
	}
	return "host " + h
}
