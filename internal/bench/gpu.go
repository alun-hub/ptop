package bench

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func runGPU(ctx context.Context, cfg Config, out chan<- Event) (Result, error) {
	out <- Progress{Frac: 0.2, Label: "detecting GPUs"}

	if have("nvidia-smi") {
		if r, ok := gpuNvidia(ctx, out); ok {
			return r, nil
		}
	}
	cards := drmCards()
	if len(cards) == 0 {
		return Result{
			Tool: "sysfs",
			Metrics: []Metric{{
				Name: "GPU", Display: "none detected",
				Note: "no discrete or integrated GPU is visible to this system",
			}},
			Summary: "No GPU detected. That is normal for a headless server.",
		}, nil
	}

	res := Result{Tool: "sysfs (/sys/class/drm)"}
	out <- Progress{Frac: 0.6, Label: "reading GPU status"}
	for i, c := range cards {
		res.Metrics = append(res.Metrics, drmCardMetrics(c, len(cards) > 1, i)...)
	}

	// Optional real workload if a benchmark tool is installed.
	if have("glmark2") && cfg.Depth != Quick {
		out <- Progress{Frac: 0.8, Label: "glmark2 (off-screen)"}
		if score := glmark2Score(ctx, out); score > 0 {
			res.Metrics = append(res.Metrics, Metric{
				Name: "glmark2 score (off-screen)", Display: fmt.Sprintf("%.0f", score),
				Verdict: glmarkVerdict(score), Note: "rough OpenGL throughput - higher is faster",
				Bar: normLog(clampLo(score, 50), 50, 20000), HasBar: true,
				ScaleLo: "slow", ScaleHi: "fast",
			}.cmp(score, "score", false))
		}
	}

	res.Summary = gpuSummary(res.Metrics)
	out <- Progress{Frac: 1, Label: "done"}
	return res, nil
}

// ---- nvidia-smi ---------------------------------------------------------

func gpuNvidia(ctx context.Context, out chan<- Event) (Result, bool) {
	raw, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=name,memory.total,memory.used,utilization.gpu,temperature.gpu,driver_version",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return Result{}, false
	}
	res := Result{Tool: "nvidia-smi"}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for gi, ln := range lines {
		f := splitCSV(ln)
		if len(f) < 6 {
			continue
		}
		pfx := ""
		if len(lines) > 1 {
			pfx = fmt.Sprintf("GPU %d ", gi)
		}
		memTot, _ := strconv.ParseFloat(f[1], 64)
		memUse, _ := strconv.ParseFloat(f[2], 64)
		util, _ := strconv.ParseFloat(f[3], 64)
		temp, _ := strconv.ParseFloat(f[4], 64)
		res.Metrics = append(res.Metrics,
			Metric{Name: pfx + "GPU", Display: f[0], Note: "driver " + f[5]},
			Metric{Name: pfx + "VRAM", Display: fmt.Sprintf("%.0f MiB free of %.0f MiB", memTot-memUse, memTot),
				Bar: 1 - norm(memUse, 0, memTot), HasBar: true, ScaleLo: "full", ScaleHi: "free"},
			Metric{Name: pfx + "Utilisation", Display: fmt.Sprintf("%.0f%%", util),
				Note: "how busy the GPU is right now"},
			Metric{Name: pfx + "Temperature", Display: fmt.Sprintf("%.0f C", temp),
				Verdict: tempVerdict(temp), Note: tempNote(temp)}.cmp(temp, "C", true),
		)
	}
	if len(res.Metrics) == 0 {
		return Result{}, false
	}
	res.Summary = gpuSummary(res.Metrics)
	out <- Progress{Frac: 1, Label: "done"}
	return res, true
}

// ---- sysfs / DRM ------------------------------------------------------

func drmCards() []string {
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		// keep "card0", "card1" - drop connectors like "card1-DP-1"
		if !strings.HasPrefix(name, "card") || strings.Contains(name, "-") {
			continue
		}
		if _, err := os.Stat("/sys/class/drm/" + name + "/device"); err == nil {
			out = append(out, "/sys/class/drm/"+name)
		}
	}
	return out
}

func drmCardMetrics(path string, multi bool, idx int) []Metric {
	dev := path + "/device"
	ue := parseKV(readFileTrim(dev + "/uevent"))
	vendor, device := "", ""
	if id := ue["PCI_ID"]; id != "" {
		p := strings.SplitN(id, ":", 2)
		vendor = pciVendor(p[0])
		if len(p) == 2 {
			device = p[1]
		}
	}
	driver := ue["DRIVER"]
	pfx := ""
	if multi {
		pfx = fmt.Sprintf("GPU %d ", idx)
	}

	name := strings.TrimSpace(vendor + " " + driver)
	if name == "" {
		name = "unknown"
	}
	if device != "" {
		name += " (PCI " + strings.ToLower(ue["PCI_ID"]) + ")"
	}
	ms := []Metric{{Name: pfx + "GPU", Display: name, Note: gpuClassNote(driver, dev)}}

	if v := readIntTrim(dev + "/mem_info_vram_total"); v > 0 {
		used := readIntTrim(dev + "/mem_info_vram_used")
		disp := fmt.Sprintf("%s VRAM", humanBytes(v))
		m := Metric{Name: pfx + "VRAM", Display: disp}
		if used > 0 {
			m.Display = fmt.Sprintf("%s free of %s", humanBytes(v-used), humanBytes(v))
			m.Bar = 1 - norm(float64(used), 0, float64(v))
			m.HasBar = true
			m.ScaleLo = "full"
			m.ScaleHi = "free"
		}
		ms = append(ms, m)
	}
	if busy := readFileTrim(dev + "/gpu_busy_percent"); busy != "" {
		ms = append(ms, Metric{Name: pfx + "Utilisation", Display: busy + "%",
			Note: "how busy the GPU is right now"})
	}
	if t := hwmonTemp(dev); t > 0 {
		ms = append(ms, Metric{Name: pfx + "Temperature", Display: fmt.Sprintf("%.0f C", t),
			Verdict: tempVerdict(t), Note: tempNote(t)}.cmp(t, "C", true))
	}
	return ms
}

func hwmonTemp(dev string) float64 {
	hw, err := filepath.Glob(dev + "/hwmon/hwmon*/temp1_input")
	if err != nil || len(hw) == 0 {
		return 0
	}
	v := readIntTrim(hw[0])
	if v <= 0 {
		return 0
	}
	return float64(v) / 1000
}

func gpuClassNote(driver, dev string) string {
	switch driver {
	case "i915", "xe":
		return "Intel integrated graphics"
	case "amdgpu":
		if readIntTrim(dev+"/mem_info_vram_total") < 2<<30 {
			return "AMD graphics - small VRAM suggests an integrated APU"
		}
		return "AMD graphics"
	case "nvidia", "nouveau":
		return "NVIDIA GPU"
	}
	if driver != "" {
		return "driver: " + driver
	}
	return ""
}

// ---- glmark2 --------------------------------------------------------

func glmark2Score(ctx context.Context, out chan<- Event) float64 {
	raw, err := streamCmd(ctx, out, "glmark2", "--off-screen", "-b", "build")
	if err != nil {
		raw2, err2 := streamCmd(ctx, out, "glmark2", "--off-screen")
		if err2 != nil {
			return 0
		}
		raw = raw2
	}
	for _, ln := range strings.Split(raw, "\n") {
		if i := strings.Index(strings.ToLower(ln), "glmark2 score:"); i >= 0 {
			s := strings.TrimSpace(ln[i+len("glmark2 score:"):])
			v, _ := strconv.ParseFloat(strings.Fields(s)[0], 64)
			return v
		}
	}
	return 0
}

func glmarkVerdict(s float64) Verdict {
	switch {
	case s >= 3000:
		return VGood
	case s >= 500:
		return VOkay
	default:
		return VPoor
	}
}

// ---- shared helpers -------------------------------------------------

func tempVerdict(c float64) Verdict {
	switch {
	case c == 0:
		return VNeutral
	case c < 70:
		return VGood
	case c < 85:
		return VOkay
	default:
		return VPoor
	}
}
func tempNote(c float64) string {
	switch {
	case c < 70:
		return "cool"
	case c < 85:
		return "warm - fine under load, check airflow if idle"
	default:
		return "hot - likely throttling; clean the cooler / improve airflow"
	}
}

func gpuSummary(ms []Metric) string {
	for _, m := range ms {
		if strings.HasSuffix(m.Name, "GPU") {
			return "Detected: " + m.Display + ". This test reports status only - it does not run a GPU compute benchmark."
		}
	}
	return "GPU status is shown above."
}

func pciVendor(id string) string {
	switch strings.ToLower(id) {
	case "10de":
		return "NVIDIA"
	case "1002", "1022":
		return "AMD"
	case "8086":
		return "Intel"
	}
	return "PCI " + id
}

func parseKV(s string) map[string]string {
	m := map[string]string{}
	for _, ln := range strings.Split(s, "\n") {
		if k, v, ok := strings.Cut(ln, "="); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

func readFileTrim(p string) string {
	b, _ := os.ReadFile(p)
	return strings.TrimSpace(string(b))
}
func readIntTrim(p string) int64 {
	v, _ := strconv.ParseInt(readFileTrim(p), 10, 64)
	return v
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
	}
	return strconv.FormatInt(n, 10) + " B"
}
