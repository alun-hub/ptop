package bench

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// lanMetrics gathers the checks that need nothing running on the other end:
// gateway latency, link speed/duplex, NIC error counters, TCP connect latency
// and handshake rate, and path MTU.
func lanMetrics(ctx context.Context, out chan<- Event, cfg Config) []Metric {
	var ms []Metric
	gw, iface := defaultRoute()

	if gw != "" && have("ping") {
		out <- Progress{Frac: 0.55, Label: "LAN latency (gateway " + gw + ")"}
		if raw, err := streamCmd(ctx, out, "ping", "-c", "10", "-i", "0.2", "-q", gw); err == nil {
			if m := rttRe.FindStringSubmatch(raw); m != nil {
				avg, _ := strconv.ParseFloat(m[2], 64)
				jit, _ := strconv.ParseFloat(m[4], 64)
				ms = append(ms, Metric{
					Name: "LAN latency (to gateway " + gw + ")", Display: fmt.Sprintf("%.2f ms", avg),
					Verdict: lanLatVerdict(avg), Note: lanLatNote(avg, jit),
					Bar: 1 - normLog(clampLo(avg, 0.05), 0.05, 20), HasBar: true,
					ScaleLo: "slow", ScaleHi: "fast",
				}.cmp(avg, "ms", true))
			}
		}
	}

	if iface != "" {
		if m, ok := linkMetric(iface); ok {
			ms = append(ms, m)
		}
		if m, ok := nicErrorMetric(iface); ok {
			ms = append(ms, m)
		}
	}

	target := cfg.Host
	if target == "" {
		target = gw
	}
	if target != "" {
		out <- Progress{Frac: 0.58, Label: "TCP probes (" + target + ")"}
		// Avoid :22 - sshd's own accept throttling would dominate the rate.
		ports := []string{"443", "80", "53"}
		if p := cfg.Port; p != 0 {
			ports = append([]string{strconv.Itoa(p)}, ports...)
		} else {
			ports = append([]string{strconv.Itoa(ServePort)}, ports...)
		}
		if port := firstOpenPort(ctx, target, ports); port != "" {
			hp := target + ":" + port
			if rtt := tcpConnLatency(ctx, hp); rtt > 0 {
				ms = append(ms, Metric{
					Name: "TCP connect latency (" + hp + ")", Display: durMS(rtt),
					Verdict: tcpRTTVerdict(rtt), Note: "one full handshake round trip - compare with ICMP latency above",
					Bar: 1 - normLog(clampLo(rtt, 0.1), 0.1, 50), HasBar: true,
					ScaleLo: "slow", ScaleHi: "fast",
				}.cmp(rtt, "ms", true))
			}
			if cfg.Depth != Quick {
				if rate := tcpConnRate(ctx, hp); rate > 0 {
					ms = append(ms, Metric{
						Name: "TCP handshake rate (" + hp + ")", Display: fmt.Sprintf("%.0f/s", rate),
						Verdict: connRateVerdict(rate), Note: connRateNote(rate),
						Bar: normLog(clampLo(rate, 50), 50, 30000), HasBar: true,
						ScaleLo: "slow", ScaleHi: "fast",
					}.cmp(rate, "/s", false))
				}
			}
		}
	}

	if gw != "" && have("ping") && cfg.Depth != Quick {
		out <- Progress{Frac: 0.59, Label: "path MTU"}
		if mtu := pathMTU(ctx, gw); mtu > 0 {
			ms = append(ms, Metric{
				Name: "Path MTU (to gateway)", Display: fmt.Sprintf("%d bytes", mtu),
				Verdict: mtuVerdict(mtu), Note: mtuNote(mtu),
			}.cmp(float64(mtu), "bytes", false))
		}
	}

	// Rough throughput with no cooperating peer: a burst of large ICMP echoes
	// to the gateway. Needs root (large preload) and underestimates fast links
	// because it is still round-trip and lightly pipelined.
	if gw != "" && have("ping") && !cfg.IsRoot && cfg.Depth != Quick {
		out <- LogLine{Text: "run ptop as root for a rough throughput estimate (flood ping to the gateway)"}
	}
	if gw != "" && have("ping") && cfg.IsRoot && cfg.Depth != Quick {
		out <- Progress{Frac: 0.6, Label: "throughput estimate (flood ping)"}
		if mbits, ok := floodPingMbits(ctx, gw); ok {
			ms = append(ms, Metric{
				Name: "Throughput estimate (flood ping)", Display: fmt.Sprintf("~%.0f Mbit/s", mbits),
				Verdict: floodVerdict(mbits), Note: floodNote(mbits),
				Bar: normLog(clampLo(mbits, 10), 10, 1000), HasBar: true,
				ScaleLo: "slow", ScaleHi: "1 Gbit+",
			}.cmp(mbits, "Mbit/s", false))
		}
	}
	return ms
}

var (
	pingRecvRe = regexp.MustCompile(`(\d+) packets transmitted, (\d+) (?:packets )?received`)
	pingTimeRe = regexp.MustCompile(`time (\d+)ms`)
)

// floodPingMbits sends a burst of large ICMP echoes to host and estimates the
// link rate from bytes moved (request + reply both cross the local hop) over
// wall time. Root only.
func floodPingMbits(ctx context.Context, host string) (float64, bool) {
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	const count, size = 250, 65000
	// -f (flood) sends the next echo as soon as a reply arrives, keeping a
	// dozen-odd packets in flight - enough to be partly bandwidth-bound.
	raw, _ := exec.CommandContext(c, "ping", "-q", "-n", "-f",
		"-c", strconv.Itoa(count), "-s", strconv.Itoa(size),
		"-W", "1", host).CombinedOutput()

	rm := pingRecvRe.FindStringSubmatch(string(raw))
	tm := pingTimeRe.FindStringSubmatch(string(raw))
	if rm == nil || tm == nil {
		return 0, false
	}
	recv, _ := strconv.Atoi(rm[2])
	ms, _ := strconv.ParseFloat(tm[1], 64)
	if recv < count/4 || ms <= 0 {
		return 0, false
	}
	// each successful echo: (size + 28) bytes out and the same back
	bytes := float64(recv) * float64(size+28) * 2
	return bytes * 8 / (ms / 1000) / 1e6, true
}

func floodVerdict(mbits float64) Verdict {
	switch {
	case mbits >= 500:
		return VGood
	case mbits >= 80:
		return VOkay
	default:
		return VPoor
	}
}
func floodNote(mbits float64) string {
	base := "rough round-trip estimate - a real gigabit link often reads 400-700 here. "
	switch {
	case mbits >= 500:
		return base + "consistent with a gigabit-or-faster link"
	case mbits >= 80:
		return base + "looks like ~100 Mbit, a saturated link, or wifi"
	default:
		return base + "low - slow link, heavy loss, or a rate-limiting gateway"
	}
}

// defaultRoute reads /proc/net/route and returns the default gateway IP and its
// interface.
func defaultRoute() (gw, iface string) {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return "", ""
	}
	for _, ln := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(ln)
		if len(f) < 3 || f[1] != "00000000" {
			continue
		}
		v, err := strconv.ParseUint(f[2], 16, 32)
		if err != nil {
			continue
		}
		return fmt.Sprintf("%d.%d.%d.%d", byte(v), byte(v>>8), byte(v>>16), byte(v>>24)), f[0]
	}
	return "", ""
}

func sysNet(iface, name string) string {
	b, _ := os.ReadFile("/sys/class/net/" + iface + "/" + name)
	return strings.TrimSpace(string(b))
}
func sysNetInt(iface, name string) int64 {
	v, _ := strconv.ParseInt(sysNet(iface, name), 10, 64)
	return v
}

func linkMetric(iface string) (Metric, bool) {
	m := Metric{Name: "Link (" + iface + ")"}
	if _, err := os.Stat("/sys/class/net/" + iface + "/wireless"); err == nil {
		m.Display = "wireless"
		m.Verdict = VOkay
		m.Note = "wifi rate varies constantly - use a wired link for a stable baseline"
		return m, true
	}
	speed := sysNetInt(iface, "speed") // Mbit/s, negative/zero if unknown
	if speed <= 0 {
		return m, false
	}
	duplex := sysNet(iface, "duplex")
	m.Display = fmt.Sprintf("%d Mbit/s", speed)
	if duplex != "" && duplex != "unknown" {
		m.Display += ", " + duplex + " duplex"
	}
	m.Bar = normLog(clampLo(float64(speed), 100), 100, 25000)
	m.HasBar = true
	m.ScaleLo = "100 Mbit"
	m.ScaleHi = "25 Gbit"
	m.Value, m.Unit = float64(speed), "Mbit/s"
	switch {
	case duplex == "half":
		m.Verdict = VPoor
		m.Note = "half duplex - almost always a negotiation fault; kills throughput under load"
	case speed >= 10000:
		m.Verdict = VGood
		m.Note = "10 Gbit or faster link"
	case speed >= 1000:
		m.Verdict = VGood
		m.Note = "gigabit link"
	case speed >= 100:
		m.Verdict = VOkay
		m.Note = "100 Mbit link - fine for light use, a bottleneck for backups and file serving"
	default:
		m.Verdict = VPoor
		m.Note = "very slow link - check the cable and switch port"
	}
	return m, true
}

func nicErrorMetric(iface string) (Metric, bool) {
	base := "statistics/"
	total := sysNetInt(iface, base+"rx_packets") + sysNetInt(iface, base+"tx_packets")
	if total <= 0 {
		return Metric{}, false
	}
	var errs int64
	for _, k := range []string{"rx_errors", "rx_dropped", "rx_crc_errors", "rx_frame_errors",
		"tx_errors", "tx_dropped", "tx_carrier_errors", "collisions"} {
		if v := sysNetInt(iface, base+k); v > 0 {
			errs += v
		}
	}
	ppm := float64(errs) / float64(total) * 1e6
	m := Metric{
		Name:    "NIC errors / drops (" + iface + ")",
		Display: fmt.Sprintf("%d in %s packets", errs, humanCount(total)),
		Bar:     1 - normLog(clampLo(ppm+1, 1), 1, 1000), HasBar: true,
		ScaleLo: "many", ScaleHi: "clean",
		Value: float64(errs), Unit: "errors", LowerBetter: true,
	}
	switch {
	case errs == 0:
		m.Verdict, m.Note = VGood, "clean interface since boot"
	case ppm < 10:
		m.Verdict, m.Note = VOkay, "a few errors - watch it; often a marginal cable"
	default:
		m.Verdict, m.Note = VPoor, "frequent errors/drops - bad cable, duplex mismatch, or an overrun NIC"
	}
	return m, true
}

func firstOpenPort(ctx context.Context, host string, ports []string) string {
	for _, p := range ports {
		if tcpReachable(ctx, host, p) {
			return p
		}
	}
	return ""
}

func tcpReachable(ctx context.Context, host, port string) bool {
	c, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(c, "tcp", host+":"+port)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// tcpConnLatency returns the median connect time (ms) over 9 sequential dials.
func tcpConnLatency(ctx context.Context, hp string) float64 {
	var xs []float64
	for i := 0; i < 9; i++ {
		select {
		case <-ctx.Done():
			return 0
		default:
		}
		var d net.Dialer
		start := time.Now()
		c, err := d.DialContext(ctx, "tcp", hp)
		if err != nil {
			continue
		}
		xs = append(xs, float64(time.Since(start).Microseconds())/1000)
		c.Close()
	}
	if len(xs) == 0 {
		return 0
	}
	sortFloats(xs)
	return xs[len(xs)/2]
}

// tcpConnRate hammers hp with 16 workers for ~2s and returns successful
// handshakes per second.
func tcpConnRate(ctx context.Context, hp string) float64 {
	c, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	deadline := time.Now().Add(2 * time.Second)
	var count int64
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var d net.Dialer
			for time.Now().Before(deadline) {
				conn, err := d.DialContext(c, "tcp", hp)
				if err != nil {
					return
				}
				conn.Close()
				atomic.AddInt64(&count, 1)
			}
		}()
	}
	wg.Wait()
	return float64(count) / 2
}

// pathMTU binary-searches the largest ICMP payload that reaches host without
// fragmentation, and returns the resulting path MTU (payload + 28).
func pathMTU(ctx context.Context, host string) int {
	lo, hi := 1200, 8972
	if !pingSize(ctx, host, lo) {
		return 0
	}
	if pingSize(ctx, host, hi) {
		return hi + 28
	}
	for hi-lo > 8 {
		mid := (lo + hi) / 2
		if pingSize(ctx, host, mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo + 28
}

func pingSize(ctx context.Context, host string, payload int) bool {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return exec.CommandContext(c, "ping", "-c", "1", "-W", "1", "-M", "do",
		"-s", strconv.Itoa(payload), host).Run() == nil
}

func humanCount(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	}
	return strconv.FormatInt(n, 10)
}

func sortFloats(x []float64) {
	for i := 1; i < len(x); i++ {
		for j := i; j > 0 && x[j-1] > x[j]; j-- {
			x[j-1], x[j] = x[j], x[j-1]
		}
	}
}

func lanLatVerdict(ms float64) Verdict {
	switch {
	case ms < 1:
		return VGood
	case ms < 5:
		return VOkay
	default:
		return VPoor
	}
}
func lanLatNote(ms, jitter float64) string {
	base := fmt.Sprintf("jitter %.2f ms. ", jitter)
	switch {
	case ms < 1:
		return base + "healthy wired LAN"
	case ms < 5:
		return base + "a little high for a LAN - wifi, a busy switch, or a slow router"
	default:
		return base + "high for a local hop - congested link or an overloaded gateway"
	}
}
func tcpRTTVerdict(ms float64) Verdict {
	switch {
	case ms < 2:
		return VGood
	case ms < 15:
		return VOkay
	default:
		return VPoor
	}
}
func connRateVerdict(r float64) Verdict {
	switch {
	case r >= 3000:
		return VGood
	case r >= 300:
		return VOkay
	default:
		return VPoor
	}
}
func connRateNote(r float64) string {
	switch {
	case r >= 3000:
		return "plenty of headroom (also reflects the remote service's accept speed)"
	case r >= 300:
		return "ok - could limit a busy reverse proxy or load balancer"
	default:
		return "low - the path, or more likely the remote service's accept rate, is the limit"
	}
}
func mtuVerdict(mtu int) Verdict {
	switch {
	case mtu >= 1500:
		return VGood
	case mtu >= 1400:
		return VOkay
	default:
		return VPoor
	}
}
func mtuNote(mtu int) string {
	switch {
	case mtu >= 8000:
		return "jumbo frames are enabled end to end - good for storage networks"
	case mtu == 1500:
		return "standard Ethernet MTU"
	case mtu >= 1500:
		return "larger than standard - jumbo frames partially in play"
	case mtu >= 1400:
		return "slightly reduced - VPN or PPPoE overhead on the path"
	default:
		return "small MTU - tunnelling overhead; can hurt throughput and cause odd stalls"
	}
}
