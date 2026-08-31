// ptop - guided performance measurement for Linux servers.
//
// Run without arguments for the interactive interface:
//
//	ptop
//
// Non-interactive (for scripts / SSH without a TTY):
//
//	ptop run disk|cpu|mem|net|all [--depth quick|normal|deep] [--path DIR] [--host HOST]
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"ptop/internal/bench"
	"ptop/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

// version is set at build time by GoReleaser (-X main.version=...).
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "run":
			os.Exit(runCLI(os.Args[2:]))
		case "serve":
			os.Exit(runServe(os.Args[2:]))
		case "history", "hist":
			os.Exit(runHistory(os.Args[2:]))
		case "-h", "--help", "help":
			fmt.Print(usage)
			return
		case "--version", "version":
			fmt.Println("ptop " + version)
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown argument: %s\n\n%s", os.Args[1], usage)
			os.Exit(2)
		}
	}

	p := tea.NewProgram(ui.New(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runServe(args []string) int {
	addr := fmt.Sprintf(":%d", bench.ServePort)
	if len(args) > 0 {
		addr = args[0]
		if addr != "" && addr[0] != ':' && !containsColon(addr) {
			addr = ":" + addr // bare port
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Printf("ptop serve: throughput server listening on %s (Ctrl-C to stop)\n", addr)
	fmt.Printf("on another machine: ptop run net --host <this-host>\n")
	if err := bench.Serve(ctx, addr); err != nil {
		fmt.Fprintln(os.Stderr, "serve error:", err)
		return 1
	}
	return 0
}

func containsColon(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return true
		}
	}
	return false
}

var usage = `ptop - guided performance measurement for Linux servers

Usage:
  ptop                          start the interactive interface
  ptop run <test> [flags]       run one test directly and print the result
  ptop serve [addr]             run the throughput server for the network test
  ptop history [#]              list past runs, or show one (with % vs the run before)
  ptop history diff <#1> <#2>   compare two runs side-by-side
  ptop history tag <#> [text]   add, update, or clear a tag for a run

Tests:
  disk   read/write and random I/O
  cpu    one/all cores, AES, compression, fork/exec, scheduler
  mem    bandwidth, latency, free memory, NUMA topology
  net    latency, local-link checks and throughput
  gpu    detected GPU, VRAM, load and temperature
  all    all of the above

Flags:
  --depth quick|normal|deep   how long the test runs (default: normal)
  --tag "<text>"              tag or label for this run (stored in history)
  --path DIR                  directory for the disk test (default: current directory)
  --host HOST                 peer for the network test - runs 'ptop serve',
                              'iperf3 -s', or is reachable over ssh
  --url URL                   download this URL for the throughput test
  --port N                    port of 'ptop serve' on --host (default 5330)

ptop serve [addr] listens on ` + fmt.Sprint(bench.ServePort) + ` by default; pass ":9000" or "0.0.0.0:9000" to change.
`
