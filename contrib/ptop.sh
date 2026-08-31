#!/usr/bin/env bash
#
# ptop - enkla prestandatester för Linux-servrar
#
# Samlar vanliga sätt att mäta disk-, CPU/minne- och nätverksprestanda.
# Använder specialiserade verktyg när de finns (fio, sysbench, iperf3 ...)
# och faller annars tillbaka på verktyg som nästan alltid finns (dd, awk, curl).
#
# Användning:
#   ptop disk [sökväg]        Disk: genomströmning + latens
#   ptop cpu                  CPU: enkel- och flertrådsprestanda
#   ptop mem                  Minne: bandbredd + tillgängligt
#   ptop net [värd]           Nätverk: genomströmning + latens
#   ptop all [värd]           Kör allt
#
# Miljövariabler:
#   PTOP_SIZE     storlek på testfiler (default 1G)
#   PTOP_TIME     sekunder per deltest där det går (default 10)
#   PTOP_TMP      katalog för temporära testfiler (default aktuell katalog)

set -euo pipefail

PTOP_SIZE="${PTOP_SIZE:-1G}"
PTOP_TIME="${PTOP_TIME:-10}"
PTOP_TMP="${PTOP_TMP:-$PWD}"

# ---------------------------------------------------------------------------
# hjälpfunktioner
# ---------------------------------------------------------------------------

c_bold=$'\e[1m'; c_dim=$'\e[2m'; c_grn=$'\e[32m'; c_yel=$'\e[33m'; c_rst=$'\e[0m'
[ -t 1 ] || { c_bold=; c_dim=; c_grn=; c_yel=; c_rst=; }

have() { command -v "$1" >/dev/null 2>&1; }

section() { printf '\n%s== %s ==%s\n' "$c_bold" "$1" "$c_rst"; }
step()    { printf '%s-- %s%s\n' "$c_dim" "$1" "$c_rst"; }
note()    { printf '%s   %s%s\n' "$c_yel" "$1" "$c_rst"; }

# storlek i byte från t.ex. 1G, 512M
size_bytes() {
	local s="${1^^}" n unit
	n="${s%[KMGT]}"; unit="${s#$n}"
	case "$unit" in
		K) echo $(( n * 1024 )) ;;
		M) echo $(( n * 1024 * 1024 )) ;;
		G) echo $(( n * 1024 * 1024 * 1024 )) ;;
		T) echo $(( n * 1024 * 1024 * 1024 * 1024 )) ;;
		*) echo "$n" ;;
	esac
}

cleanup_files=()
cleanup() { local f; for f in "${cleanup_files[@]:-}"; do [ -n "$f" ] && rm -f "$f"; done; return 0; }
trap cleanup EXIT

# ---------------------------------------------------------------------------
# DISK
# ---------------------------------------------------------------------------

bench_disk() {
	local target="${1:-$PTOP_TMP}"
	local dir
	if [ -d "$target" ]; then dir="$target"; else dir="$(dirname "$target")"; fi
	section "DISK  ($dir)"

	local testfile="$dir/.ptop.disk.$$"
	cleanup_files+=("$testfile")
	local bytes; bytes="$(size_bytes "$PTOP_SIZE")"

	step "Monterad på / filsystem"
	df -hT "$dir" | sed 's/^/   /'

	if have fio; then
		step "fio - sekventiell skrivning (bäst mätvärde)"
		fio --name=seqwrite --directory="$dir" --rw=write --bs=1M \
			--size="$PTOP_SIZE" --runtime="$PTOP_TIME" --time_based \
			--end_fsync=1 --group_reporting --minimal 2>/dev/null \
			| awk -F';' '{printf "   skriv: %.1f MB/s\n", $48/1024}'

		step "fio - sekventiell läsning"
		fio --name=seqread --directory="$dir" --rw=read --bs=1M \
			--size="$PTOP_SIZE" --runtime="$PTOP_TIME" --time_based \
			--group_reporting --minimal 2>/dev/null \
			| awk -F';' '{printf "   läs:   %.1f MB/s\n", $7/1024}'

		step "fio - slumpvis 4k IOPS (läs/skriv 70/30)"
		fio --name=randrw --directory="$dir" --rw=randrw --rwmixread=70 \
			--bs=4k --size="$PTOP_SIZE" --runtime="$PTOP_TIME" --time_based \
			--iodepth=16 --ioengine=libaio --direct=1 --group_reporting --minimal 2>/dev/null \
			| awk -F';' '{printf "   läs:  %d IOPS   skriv: %d IOPS\n", $8, $49}'
	else
		note "fio saknas - använder dd (grovt, påverkas av cache)"
		step "dd - sekventiell skrivning (O_DIRECT om möjligt)"
		local ddflag="oflag=direct"
		dd if=/dev/zero of="$testfile" bs=1M count=$(( bytes / 1048576 )) \
			conv=fdatasync $ddflag status=progress 2>&1 | tail -1 | sed 's/^/   /' \
			|| dd if=/dev/zero of="$testfile" bs=1M count=$(( bytes / 1048576 )) \
			conv=fdatasync status=progress 2>&1 | tail -1 | sed 's/^/   /'

		step "dd - sekventiell läsning (töm cache om root)"
		[ "$(id -u)" = 0 ] && sync && echo 3 > /proc/sys/vm/drop_caches 2>/dev/null || true
		dd if="$testfile" of=/dev/null bs=1M iflag=direct status=progress 2>&1 | tail -1 | sed 's/^/   /' \
			|| dd if="$testfile" of=/dev/null bs=1M status=progress 2>&1 | tail -1 | sed 's/^/   /'
		rm -f "$testfile"
	fi

	if have ioping; then
		step "ioping - I/O-latens"
		ioping -c 10 "$dir" 2>/dev/null | tail -2 | sed 's/^/   /'
	fi

	if have hdparm && [ "$(id -u)" = 0 ]; then
		local dev; dev="$(df --output=source "$dir" | tail -1)"
		case "$dev" in
			/dev/*) step "hdparm - buffrad läsning från $dev"
				hdparm -t "$dev" 2>/dev/null | grep -i 'Timing' | sed 's/^/   /' || true ;;
		esac
	fi
}

# ---------------------------------------------------------------------------
# CPU
# ---------------------------------------------------------------------------

bench_cpu() {
	section "CPU"
	local ncpu; ncpu="$(nproc)"
	step "$ncpu logiska kärnor"
	grep -m1 'model name' /proc/cpuinfo | sed 's/^/   /' || true
	step "Aktuell load average"
	cut -d' ' -f1-3 /proc/loadavg | sed 's/^/   /'

	if have sysbench; then
		step "sysbench - 1 tråd"
		sysbench cpu --cpu-max-prime=20000 --threads=1 --time="$PTOP_TIME" run 2>/dev/null \
			| grep -E 'events per second|total time' | sed 's/^/   /'
		step "sysbench - $ncpu trådar"
		sysbench cpu --cpu-max-prime=20000 --threads="$ncpu" --time="$PTOP_TIME" run 2>/dev/null \
			| grep -E 'events per second' | sed 's/^/   /'
	elif have stress-ng; then
		step "stress-ng - $ncpu kärnor, $PTOP_TIME s (bogo-ops)"
		stress-ng --cpu "$ncpu" --timeout "${PTOP_TIME}s" --metrics-brief 2>&1 \
			| grep -E 'cpu ' | sed 's/^/   /'
	else
		note "sysbench/stress-ng saknas - använder awk-primtalstest"
		step "awk - primtal upp till 200000, 1 tråd"
		local t0 t1
		t0="$(date +%s.%N)"
		awk 'BEGIN{c=0;for(i=2;i<200000;i++){p=1;for(j=2;j*j<=i;j++)if(i%j==0){p=0;break}c+=p}print "   " c " primtal"}'
		t1="$(date +%s.%N)"
		awk -v a="$t0" -v b="$t1" 'BEGIN{printf "   tid: %.2f s (lägre = snabbare)\n", b-a}'
	fi
}

# ---------------------------------------------------------------------------
# MINNE
# ---------------------------------------------------------------------------

bench_mem() {
	section "MINNE"
	step "Tillgängligt minne"
	free -h | sed 's/^/   /'

	if have sysbench; then
		step "sysbench - minnesbandbredd (skriv, 1 tråd)"
		sysbench memory --memory-block-size=1M --memory-total-size=10G \
			--memory-oper=write --threads=1 run 2>/dev/null \
			| grep -E 'transferred|per second' | sed 's/^/   /'
	elif have mbw; then
		step "mbw - 256 MB"
		mbw -q 256 2>/dev/null | grep AVG | sed 's/^/   /'
	else
		note "sysbench/mbw saknas - använder dd mot tmpfs"
		local mtmp="/dev/shm/.ptop.mem.$$"
		cleanup_files+=("$mtmp")
		step "dd - skriv 1 GB till /dev/shm"
		dd if=/dev/zero of="$mtmp" bs=1M count=1024 2>&1 | tail -1 | sed 's/^/   /'
		step "dd - läs tillbaka"
		dd if="$mtmp" of=/dev/null bs=1M 2>&1 | tail -1 | sed 's/^/   /'
		rm -f "$mtmp"
	fi
}

# ---------------------------------------------------------------------------
# NÄTVERK
# ---------------------------------------------------------------------------

bench_net() {
	local host="${1:-}"
	section "NÄTVERK${host:+  ($host)}"

	if [ -n "$host" ]; then
		step "ping - latens mot $host"
		ping -c 10 -q "$host" 2>/dev/null | tail -2 | sed 's/^/   /' || note "ping misslyckades"

		if have iperf3; then
			step "iperf3 - genomströmning mot $host (kräver 'iperf3 -s' på värden)"
			iperf3 -c "$host" -t "$PTOP_TIME" 2>&1 | grep -E 'sender|receiver' | sed 's/^/   /' \
				|| note "iperf3 misslyckades - kör 'iperf3 -s' på $host"
		else
			note "iperf3 saknas - hoppar över genomströmning mot värd"
		fi
	else
		note "ingen värd angiven - kör allmänna tester"
		step "Nätverksgränssnitt"
		ip -br addr 2>/dev/null | sed 's/^/   /' || ifconfig -a | sed 's/^/   /'

		step "Latens mot 1.1.1.1 och 8.8.8.8"
		for h in 1.1.1.1 8.8.8.8; do
			ping -c 5 -q "$h" 2>/dev/null | grep -E 'rtt|packet loss' | sed "s/^/   $h: /" || true
		done

		if have curl; then
			local url="${PTOP_URL:-https://speed.hetzner.de/100MB.bin}"
			step "curl - nedladdningshastighet ($url)"
			step "(sätt PTOP_URL för en egen testfil)"
			curl -fsL --max-time 60 -o /dev/null \
				-w '%{http_code} %{speed_download} %{size_download} %{time_total}\n' "$url" 2>/dev/null \
				| awk '{ if ($1=="000" || $1+0>=400) exit 1; printf "   %.1f MB/s   (%d byte på %.1f s, HTTP %s)\n", $2/1048576, $3, $4, $1 }' \
				|| note "nedladdning misslyckades (nätverk/brandvägg?) - prova PTOP_URL=<egen fil>"
		fi
	fi
}

# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------

usage() { sed -n '3,/PTOP_TMP/p' "$0" | sed 's/^#\{1,\} \{0,1\}//;s/^#$//'; }

cmd="${1:-}"; shift || true
case "$cmd" in
	disk) bench_disk "${1:-}" ;;
	cpu)  bench_cpu ;;
	mem)  bench_mem ;;
	net)  bench_net "${1:-}" ;;
	all)  bench_disk; bench_cpu; bench_mem; bench_net "${1:-}" ;;
	-h|--help|help|"") usage ;;
	*) echo "Okänt kommando: $cmd" >&2; usage; exit 1 ;;
esac
