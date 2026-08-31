package bench

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const probeHost = "cloudflare.com"

const defaultURL = "https://speed.hetzner.de/100MB.bin"

var (
	rttRe  = regexp.MustCompile(`=\s*([0-9.]+)/([0-9.]+)/([0-9.]+)/([0-9.]+)\s*ms`)
	lossRe = regexp.MustCompile(`([0-9.]+)% packet loss`)
)

func runNet(ctx context.Context, cfg Config, out chan<- Event) (Result, error) {
	res := Result{}
	var metrics []Metric

	// 1. Latency -------------------------------------------------------
	pingHost := cfg.Host
	if pingHost == "" {
		pingHost = "1.1.1.1"
	}
	if have("ping") {
		scope := "internet latency"
		if cfg.Host != "" {
			scope = "latency to " + pingHost
		}
		out <- Progress{Frac: 0.1, Label: "measuring " + scope}
		stop := timeProgress(out, 6*time.Second, "ping "+pingHost)
		raw, err := streamCmd(ctx, out, "ping", "-c", "10", "-i", "0.2", "-q", pingHost)
		stop()
		if err == nil {
			latName := "Internet latency (to " + pingHost + ")"
			if cfg.Host != "" {
				latName = "Latency (to " + pingHost + ")"
			}
			if m := rttRe.FindStringSubmatch(raw); m != nil {
				avg, _ := strconv.ParseFloat(m[2], 64)
				jitter, _ := strconv.ParseFloat(m[4], 64)
				metrics = append(metrics, Metric{
					Name: latName, Display: fmt.Sprintf("%.1f ms", avg),
					Verdict: pingVerdict(avg), Note: pingNote(avg),
					Bar: 1 - norm(avg, 0, 150), HasBar: true,
					ScaleLo: "high latency", ScaleHi: "low latency",
				})
				metrics = append(metrics, Metric{
					Name: "Jitter (latency variation)", Display: fmt.Sprintf("%.1f ms", jitter),
					Verdict: jitterVerdict(jitter), Note: jitterNote(jitter),
					Bar: 1 - norm(jitter, 0, 20), HasBar: true,
					ScaleLo: "erratic", ScaleHi: "stable",
				})
			}
			if m := lossRe.FindStringSubmatch(raw); m != nil {
				if loss, _ := strconv.ParseFloat(m[1], 64); loss > 0 {
					metrics = append(metrics, Metric{
						Name: "Packet loss", Display: fmt.Sprintf("%.0f%%", loss),
						Verdict: VPoor, Note: "packets are being dropped - unstable connection",
					})
				}
			}
		} else {
			out <- LogLine{Text: "ping failed: " + err.Error()}
		}
	}
	out <- Progress{Frac: 0.35, Label: "latency done"}

	// 2. Name resolution + secure connection setup -------------------
	out <- Progress{Frac: 0.45, Label: "DNS lookup"}
	if d, err := timeIt(ctx, func(c context.Context) error {
		var r net.Resolver
		_, e := r.LookupHost(c, probeHost)
		return e
	}); err == nil {
		ms := float64(d.Microseconds()) / 1000
		metrics = append(metrics, Metric{
			Name: "DNS lookup", Display: durMS(ms), Verdict: dnsVerdict(ms),
			Note: "time to resolve a domain name (" + probeHost + ")",
			Bar:  1 - normLog(clampLo(ms, 1), 1, 500), HasBar: true,
			ScaleLo: "slow", ScaleHi: "fast",
		})
	} else {
		out <- LogLine{Text: "DNS lookup failed: " + err.Error()}
	}

	out <- Progress{Frac: 0.5, Label: "TLS handshake"}
	if d, err := timeIt(ctx, func(c context.Context) error {
		dl := &tls.Dialer{}
		conn, e := dl.DialContext(c, "tcp", probeHost+":443")
		if e == nil {
			conn.Close()
		}
		return e
	}); err == nil {
		ms := float64(d.Microseconds()) / 1000
		metrics = append(metrics, Metric{
			Name: "Time to secure connection (TCP+TLS)", Display: durMS(ms), Verdict: tlsVerdict(ms),
			Note: "handshake for one HTTPS connection - every new connection pays this",
			Bar:  1 - normLog(clampLo(ms, 5), 5, 1000), HasBar: true,
			ScaleLo: "slow", ScaleHi: "fast",
		})
	} else {
		out <- LogLine{Text: "TLS handshake failed: " + err.Error()}
	}

	// 3. Local network checks (need nothing on the other end) --------
	metrics = append(metrics, lanMetrics(ctx, out, cfg)...)

	// 4. Throughput ------------------------------------------------
	res.Tool = "ping + local checks"
	secs := cfg.Depth.Seconds()
	measured := false
	if cfg.Host != "" {
		out <- Progress{Frac: 0.65, Label: "throughput (ptop serve)"}
		if down, up, terr := lanThroughput(ctx, cfg.Host, cfg.Port, secs); terr == nil {
			metrics = append(metrics,
				throughputMetric("Throughput - download (ptop serve)", down),
				throughputMetric("Throughput - upload (ptop serve)", up),
			)
			res.Tool = "ptop serve"
			measured = true
		} else if have("iperf3") {
			out <- Progress{Frac: 0.7, Label: "throughput (iperf3)"}
			if bps, e := iperf3(ctx, cfg, out); e == nil {
				metrics = append(metrics, throughputMetric("Throughput (iperf3)", bps/1e6))
				res.Tool = "iperf3"
				measured = true
			} else {
				out <- LogLine{Text: "iperf3 failed - run 'iperf3 -s' on " + cfg.Host}
			}
		}
		if !measured && have("ssh") && tcpReachable(ctx, cfg.Host, "22") {
			out <- Progress{Frac: 0.8, Label: "throughput (ssh)"}
			if mbit, e := sshThroughput(ctx, cfg.Host); e == nil {
				m := throughputMetric("Throughput - upload (via ssh)", mbit)
				m.Note = "sent through ssh - capped by encryption (~one core of AES); the raw link may be faster"
				metrics = append(metrics, m)
				res.Tool = "ssh"
				measured = true
			} else {
				out <- LogLine{Text: "ssh throughput test skipped: " + e.Error()}
			}
		}
		if !measured {
			out <- LogLine{Text: "no throughput server on " + cfg.Host +
				" - run 'ptop serve' there (or 'iperf3 -s'); measuring internet download instead"}
		}
	}
	if !measured {
		url := cfg.URL
		if url == "" {
			url = defaultURL
		}
		mbits, e := httpDownload(ctx, out, url)
		if e != nil {
			out <- LogLine{Text: "download test failed: " + e.Error()}
		} else {
			name := "Download (HTTP, internet)"
			if cfg.URL != "" {
				name = "Download (HTTP)"
			}
			m := throughputMetric(name, mbits)
			m.Display = fmt.Sprintf("%.0f Mbit/s (%.1f MB/s)", mbits, mbits/8)
			metrics = append(metrics, m)
		}
	}
	out <- Progress{Frac: 1, Label: "done"}

	if len(metrics) == 0 {
		return res, fmt.Errorf("no network measurements could be made")
	}
	res.Metrics = metrics
	res.Summary = netSummary(metrics)
	return res, nil
}

func throughputMetric(name string, mbits float64) Metric {
	return Metric{
		Name: name, Display: fmt.Sprintf("%.0f Mbit/s", mbits),
		Verdict: linkVerdict(mbits), Note: linkNote(mbits),
		Bar: normLog(clampLo(mbits, 10), 10, 10000), HasBar: true,
		ScaleLo: "slow", ScaleHi: "10 Gbit/s",
	}
}

// sshThroughput pipes 256 MiB of zeros through ssh to a remote 'cat >/dev/null'
// and reports the observed rate in Mbit/s. Needs key-based auth (BatchMode).
func sshThroughput(ctx context.Context, host string) (float64, error) {
	c, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	const bytes = 256 << 20
	cmd := exec.CommandContext(c, "sh", "-c",
		"dd if=/dev/zero bs=1M count=256 2>/dev/null | "+
			"ssh -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=accept-new "+
			shellArg(host)+" 'cat >/dev/null'")
	start := time.Now()
	if err := cmd.Run(); err != nil {
		return 0, err
	}
	secs := time.Since(start).Seconds()
	if secs <= 0 {
		return 0, fmt.Errorf("too fast to measure")
	}
	return float64(bytes) * 8 / secs / 1e6, nil
}

func shellArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

type iperfReport struct {
	End struct {
		SumReceived struct {
			BitsPerSecond float64 `json:"bits_per_second"`
		} `json:"sum_received"`
	} `json:"end"`
}

func iperf3(ctx context.Context, cfg Config, out chan<- Event) (float64, error) {
	secs := cfg.Depth.Seconds()
	stop := timeProgress(out, time.Duration(secs)*time.Second, "iperf3 to "+cfg.Host)
	raw, err := streamCmd(ctx, out, "iperf3", "-c", cfg.Host, "-t", strconv.Itoa(secs), "--json")
	stop()
	if err != nil {
		return 0, err
	}
	i := strings.IndexByte(raw, '{')
	if i < 0 {
		return 0, fmt.Errorf("empty iperf3 output")
	}
	var rep iperfReport
	if err := json.Unmarshal([]byte(raw[i:]), &rep); err != nil {
		return 0, err
	}
	return rep.End.SumReceived.BitsPerSecond, nil
}

func httpDownload(ctx context.Context, out chan<- Event, url string) (float64, error) {
	out <- LogLine{Text: "GET " + url}
	dctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(dctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "ptop/1.0")
	stop := timeProgress(out, 15*time.Second, "downloading")
	defer stop()
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	n, err := io.Copy(io.Discard, resp.Body)
	elapsed := time.Since(start).Seconds()
	if err != nil && n == 0 {
		return 0, err
	}
	if elapsed == 0 {
		return 0, fmt.Errorf("too fast to measure")
	}
	return float64(n) * 8 / elapsed / 1e6, nil
}

func pingVerdict(ms float64) Verdict {
	switch {
	case ms < 20:
		return VGood
	case ms < 80:
		return VOkay
	default:
		return VPoor
	}
}
func pingNote(ms float64) string {
	switch {
	case ms < 1:
		return "same network / very close - excellent"
	case ms < 20:
		return "low latency - good for everything including databases and games"
	case ms < 80:
		return "ok for web and APIs, noticeable for real-time"
	default:
		return "high latency - another continent or a congested link"
	}
}
func linkVerdict(mbits float64) Verdict {
	switch {
	case mbits >= 900:
		return VGood
	case mbits >= 90:
		return VOkay
	case mbits > 0:
		return VPoor
	}
	return VNeutral
}
func linkNote(mbits float64) string {
	switch {
	case mbits >= 9000:
		return "10-gigabit class"
	case mbits >= 900:
		return "gigabit class"
	case mbits >= 90:
		return "around 100 Mbit - or limited by the other end / distance"
	case mbits > 0:
		return "low - check the link, firewall, or whether the server was busy"
	}
	return ""
}

// timeIt runs fn with a 5s budget and reports how long it took.
func timeIt(ctx context.Context, fn func(context.Context) error) (time.Duration, error) {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := time.Now()
	err := fn(c)
	return time.Since(start), err
}

func jitterVerdict(ms float64) Verdict {
	switch {
	case ms < 2:
		return VGood
	case ms < 10:
		return VOkay
	default:
		return VPoor
	}
}
func jitterNote(ms float64) string {
	switch {
	case ms < 2:
		return "very stable connection"
	case ms < 10:
		return "slight variation - fine for web, noticeable for video calls"
	default:
		return "heavy variation - congested link or wifi"
	}
}
func dnsVerdict(ms float64) Verdict {
	switch {
	case ms < 20:
		return VGood
	case ms < 80:
		return VOkay
	default:
		return VPoor
	}
}
func tlsVerdict(ms float64) Verdict {
	switch {
	case ms < 60:
		return VGood
	case ms < 200:
		return VOkay
	default:
		return VPoor
	}
}

func netSummary(ms []Metric) string {
	pick := func(prefix string) string {
		for _, m := range ms {
			if strings.HasPrefix(m.Name, prefix) {
				return m.Display
			}
		}
		return ""
	}
	lat := pick("Latency")
	if lat == "" {
		lat = pick("Internet latency")
	}
	tp := pick("Throughput")
	if tp == "" {
		tp = pick("Download")
	}
	switch {
	case lat != "" && tp != "":
		return fmt.Sprintf("Latency %s, throughput %s. Jitter, DNS and TLS time are shown above.", lat, tp)
	case lat != "":
		return "Latency " + lat + ". Local-link checks are above. For a real throughput number, run 'ptop serve' on another machine and pass it with --host."
	default:
		return "See the measurements above."
	}
}
