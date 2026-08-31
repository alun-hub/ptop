# ptop

Guided performance measurement for Linux servers - a `btop`-style terminal UI
that walks you through disk, CPU, memory and network tests with sensible
defaults and explains what the numbers mean.

## Build

```sh
go build -o bin/ptop .
```

The result is a single static binary with no dependencies. Copy it to any
server (`scp bin/ptop server:/usr/local/bin/`) and run it.

## Use

Interactive (needs a terminal):

```sh
ptop
```

Non-interactive, for scripts or SSH without a TTY:

```sh
ptop run disk --depth quick --path /var/lib/mysql
ptop run cpu
ptop run net --host 10.0.0.5
ptop run all
```

`--depth` is `quick` (~10s/sub-test), `normal` (default, ~30s) or `deep` (~60s).

### Network throughput without iperf3

Run the built-in server on one machine and point the test at it from the other:

```sh
# on host A
ptop serve

# on host B
ptop run net --host A
```

If `ptop serve` is not running, ptop falls back to `iperf3 -s` (if installed),
then to an ssh-based test (`--host` reachable with key auth), then to a public
HTTP download. Either way the local-link checks always run, and when ptop runs
as root the `net` test also prints a rough flood-ping throughput estimate to the
default gateway (round-trip, so it under-reads fast links) - no peer needed.

## What it measures

Each test uses a well-known tool when present, otherwise a built-in fallback so
ptop works on a bare server:

| Test | Preferred | Fallback | Metrics |
|------|-----------|----------|---------|
| disk | `fio` | `dd` with O_DIRECT | sequential read/write, 4 KiB random IOPS, p99 read latency, fsync/commit latency |
| cpu  | `sysbench` | built-in prime test | single- & multi-thread ops/s, scaling vs physical cores (SMT-aware), AES-256-GCM and deflate throughput, fork+exec rate, hypervisor steal, context-switch latency, thermal throttling (deep) |
| mem  | `sysbench` | built-in copy test | free memory, write bandwidth, random-access latency (pointer chase), NUMA topology |
| net  | `ping` + `ptop serve` / `iperf3` / ssh | `ping` + HTTP download | latency, jitter, packet loss, DNS lookup, TCP+TLS handshake; LAN latency to gateway, link speed/duplex, NIC error counters, TCP connect latency & handshake rate, path MTU, flood-ping throughput estimate (root); throughput |
| gpu  | `nvidia-smi` | `/sys/class/drm` (+ `glmark2` if present) | GPU model, VRAM, utilisation, temperature; OpenGL score when glmark2 is installed |

For the best numbers on a server: `dnf install fio sysbench iperf3` (or
`apt install ...`) and run as root (fair read test on disk).

## Layout

```
main.go            - argument handling; starts the TUI, the CLI, or `ptop serve`
cli.go             - "ptop run ..." (non-interactive)
internal/bench/    - the test engine: one runner per test, streams Events to the UI
  lan.go           - network checks that need nothing on the other end
  serve.go         - `ptop serve` and its throughput client
internal/ui/       - the Bubble Tea model (menu -> settings -> run -> results)
contrib/ptop.sh    - the original bash prototype
```

All in-app text is English.
