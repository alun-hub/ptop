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
		fmt.Println("\nptop history <#>   show a run (compared against the run before it)")
		return 0
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
