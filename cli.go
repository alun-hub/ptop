package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"ptop/internal/bench"
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

	rc := 0
	for _, k := range kinds {
		c := cfg
		c.Kind = k
		if !runOne(c) {
			rc = 1
		}
	}
	return rc
}

func runOne(c bench.Config) bool {
	fmt.Printf("\n== %s ==\n", c.Kind)
	ch := make(chan bench.Event, 128)
	go bench.Run(context.Background(), c, ch)
	for ev := range ch {
		switch e := ev.(type) {
		case bench.LogLine:
			fmt.Printf("  %s\n", e.Text)
		case bench.Finished:
			if e.Err != nil {
				fmt.Printf("  ERROR: %v\n", e.Err)
				return false
			}
			printResult(e.Result)
		}
	}
	return true
}

func printResult(r bench.Result) {
	if r.Tool != "" {
		fmt.Printf("  (tool: %s)\n", r.Tool)
	}
	for _, m := range r.Metrics {
		v := ""
		if m.Verdict != bench.VNeutral {
			v = " [" + m.Verdict.Label() + "]"
		}
		fmt.Printf("  %-30s %s%s\n", m.Name, m.Display, v)
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
