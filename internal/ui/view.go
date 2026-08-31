package ui

import (
	"fmt"
	"strings"

	"ptop/internal/bench"
	"ptop/internal/history"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	var body string
	switch m.scr {
	case scrMenu:
		body = m.viewMenu()
	case scrConfig:
		body = m.viewConfig()
	case scrPreflight:
		body = m.viewPreflight()
	case scrRunning:
		body = m.viewRunning()
	case scrResults:
		body = m.viewResults()
	case scrHistory:
		body = m.viewHistory()
	case scrHistoryView:
		body = m.viewHistoryView()
	}
	return m.header() + "\n" + body + "\n" + m.footer()
}

func (m Model) width() int {
	w := m.w - 2
	if w > 160 {
		w = 160
	}
	if w < 40 {
		w = 40
	}
	return w
}

// bodyHeight is how many content lines fit in the panel between header and
// footer (header 2 + footer 2 + 2 join newlines + 2 panel borders = 8).
func (m Model) bodyHeight() int {
	h := m.h - 8
	if h < 6 {
		h = 6
	}
	return h
}

func (m Model) header() string {
	title := styTitle.Render(" ptop ")
	sys := stySub.Render(fmt.Sprintf("%s  ·  %d cores  ·  %s RAM  ·  %s",
		trim(m.info.CPUModel, 34), m.info.NumCPU,
		fmtGB(m.info.MemTotalGB), rootTag(m.info.IsRoot)))
	line := lipgloss.JoinHorizontal(lipgloss.Center, title, "  ", sys)
	return line + "\n" + stySub.Render(strings.Repeat("─", m.width()))
}

func rootTag(root bool) string {
	if root {
		return "root"
	}
	return "regular user"
}

func (m Model) footer() string {
	var keys string
	switch m.scr {
	case scrMenu:
		keys = "↑/↓ select   ⏎ continue   q quit"
	case scrConfig:
		keys = "↑/↓ field   ←/→ change   ⏎ start   esc back"
	case scrPreflight:
		keys = "⏎ start the test   esc change   q quit"
	case scrRunning:
		keys = "esc cancel   q quit"
	case scrResults:
		keys = "←/→ switch test   ↑/↓ scroll   ⏎ back to menu   q quit"
	case scrHistory:
		keys = "↑/↓ select   ⏎ open   esc menu   q quit"
	case scrHistoryView:
		keys = "↑/↓ scroll   esc back to list   q quit"
	}
	return stySub.Render(strings.Repeat("─", m.width())) + "\n" + styHelp.Render(keys)
}

// ---- menu -----------------------------------------------------------

func (m Model) viewMenu() string {
	var rows []string
	rows = append(rows, stySub.Render("Choose what to measure. Every test has safe defaults -\nyou can fine-tune them in the next step.")+"\n")
	for i, it := range m.menu {
		marker := "  "
		nameSty := styItem
		if i == m.menuCur {
			marker = styKey.Render("▶ ")
			nameSty = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
		}
		head := fmt.Sprintf("%s%s  %s", marker, nameSty.Render(it.title), stySub.Render(it.time))
		rows = append(rows, head)
		if i == m.menuCur {
			rows = append(rows, stySub.Render(indent(it.desc, "     ")))
		}
	}
	return styPanel.Width(m.width()).Render(strings.Join(rows, "\n"))
}

// ---- config ---------------------------------------------------------

func (m Model) viewConfig() string {
	fields := m.configFields()
	var rows []string
	rows = append(rows, lipgloss.NewStyle().Bold(true).Foreground(colAccent).
		Render(m.cfgHeadline()), "")

	for i, f := range fields {
		active := i == m.cfgCur
		rows = append(rows, m.fieldRow(f, active)...)
		rows = append(rows, "")
	}
	rows = append(rows, stySub.Render(m.cfgHint()))
	return styPanelActive.Width(m.width()).Render(strings.Join(rows, "\n"))
}

func (m Model) cfgHeadline() string {
	if m.isAll {
		return "Settings · all tests"
	}
	return "Settings · " + kindTitle(m.kind)
}

func (m Model) fieldRow(f cfgField, active bool) []string {
	prefix := "  "
	labSty := stySub
	if active {
		prefix = styKey.Render("▶ ")
		labSty = lipgloss.NewStyle().Foreground(colFg).Bold(true)
	}
	switch f {
	case fieldTarget:
		val := styValue.Render(m.targets[m.targetCur])
		free := stySub.Render(fmt.Sprintf("  (%.0f GB free)", bench.FreeSpaceGB(m.targets[m.targetCur])))
		return []string{
			prefix + labSty.Render("Target directory for the disk test") + "   " + val + free,
			"     " + stySub.Render("the test file is written here and deleted afterwards · ←/→ to change"),
		}
	case fieldDepth:
		return []string{
			prefix + labSty.Render("Thoroughness") + "   " + m.depthOptions(),
			"     " + stySub.Render("longer test = more stable numbers · Normal is enough for most"),
		}
	case fieldHost:
		box := m.host.View()
		if active && m.host.Focused() {
			box = lipgloss.NewStyle().Foreground(colAccent).Render("[ ") + box + lipgloss.NewStyle().Foreground(colAccent).Render(" ]")
		} else {
			shown := m.host.Value()
			if shown == "" {
				shown = stySub.Render("(none - uses a public download test)")
			} else {
				shown = styValue.Render(shown)
			}
			box = shown
		}
		return []string{
			prefix + labSty.Render("Your own server to test against") + "   " + box,
			"     " + stySub.Render("⏎ to type · needs 'iperf3 -s' running on that server"),
		}
	}
	return nil
}

func (m Model) cfgHint() string {
	switch {
	case m.isAll:
		return "Runs all four tests in sequence."
	case m.kind == bench.Disk && !m.info.IsRoot:
		return "Tip: run ptop as root for a fair read test (otherwise the value may come from the RAM cache)."
	case m.kind == bench.Net:
		return "Without your own server, latency is measured against 1.1.1.1 and download from a public CDN."
	}
	return ""
}

// ---- preflight -----------------------------------------------------

func (m Model) viewPreflight() string {
	q := m.buildQueue()
	var rows []string
	rows = append(rows, lipgloss.NewStyle().Bold(true).Foreground(colAccent).Render("Ready to run"), "")
	for _, c := range q {
		tool, warn := planFor(c)
		rows = append(rows, fmt.Sprintf("  %s  %s", styItem.Render(padRight(c.Kind.String(), 9)), stySub.Render(tool)))
		if warn != "" {
			rows = append(rows, "  "+verdictStyle(bench.VOkay).Render("  ! "+warn))
		}
	}
	rows = append(rows, "", stySub.Render("The test loads the server heavily while it runs. Prefer to run it\nwhen the server is not in production use."))
	return styPanelActive.Width(m.width()).Render(strings.Join(rows, "\n"))
}

func planFor(c bench.Config) (tool string, warn string) {
	switch c.Kind {
	case bench.Disk:
		if has("fio") {
			return "fio · writes up to a few GB to " + c.Path, ""
		}
		return "dd (fio not installed) · writes up to 2 GB to " + c.Path,
			"install fio for IOPS measurement and more stable numbers"
	case bench.CPU:
		base := "built-in prime test"
		if has("sysbench") {
			base = "sysbench"
		}
		return base + " · 1+all cores, AES, compression, fork/exec, scheduler", ""
	case bench.Mem:
		if has("sysbench") {
			return "sysbench bandwidth + built-in latency & NUMA check", ""
		}
		return "built-in copy + latency & NUMA check (sysbench not installed)", ""
	case bench.GPU:
		if has("nvidia-smi") {
			return "nvidia-smi", ""
		}
		t := "sysfs (/sys/class/drm) - status only"
		if has("glmark2") {
			t += " + glmark2"
		}
		return t, ""
	case bench.Net:
		if c.Host != "" && has("iperf3") {
			return "ping + iperf3 to " + c.Host, ""
		}
		if c.Host != "" {
			return "ping to " + c.Host + " + public download test",
				"iperf3 not installed - cannot measure your own link"
		}
		return "ping to 1.1.1.1 + public download test", ""
	}
	return "", ""
}

// ---- running ------------------------------------------------------

func (m Model) viewRunning() string {
	done := len(m.results)
	total := done + 1 + len(m.queue)
	head := fmt.Sprintf("Running %s", kindTitle(m.cur.Kind))
	if total > 1 {
		head += stySub.Render(fmt.Sprintf("   (test %d of %d)", done+1, total))
	}
	bar := m.prog.View()
	status := fmt.Sprintf("%s %s", m.spin.View(), stySub.Render(m.label))

	logRoom := m.bodyHeight() - 6
	if logRoom < 4 {
		logRoom = 4
	}
	if logRoom > 24 {
		logRoom = 24
	}
	logs := m.logs
	if len(logs) > logRoom {
		logs = logs[len(logs)-logRoom:]
	}
	logBox := stySub.Render(strings.Join(logs, "\n"))
	if strings.TrimSpace(logBox) == "" {
		logBox = stySub.Render("(waiting for output...)")
	}

	inner := strings.Join([]string{
		styHead.Render(head),
		"", bar, status, "",
		stySub.Render("── raw output " + strings.Repeat("─", max(0, m.width()-17))),
		logBox,
	}, "\n")
	return styPanelActive.Width(m.width()).Render(inner)
}

// ---- results -----------------------------------------------------

func (m Model) viewResults() string {
	if len(m.results) == 0 {
		return styPanel.Width(m.width()).Render(stySub.Render("No results."))
	}
	rows := m.resultLines()

	// vertical scrolling for long result sets
	avail := m.bodyHeight()
	scrollHint := ""
	if len(rows) > avail {
		maxScroll := len(rows) - avail
		s := m.scroll
		if s > maxScroll {
			s = maxScroll
		}
		up, down := s > 0, s < maxScroll
		rows = rows[s : s+avail]
		switch {
		case up && down:
			scrollHint = fmt.Sprintf("  ▲▼  %d–%d of %d lines", s+1, s+avail, len(m.resultLines()))
		case up:
			scrollHint = "  ▲  top hidden"
		case down:
			scrollHint = "  ▼  more below"
		}
	}
	body := styPanel.Width(m.width()).Render(strings.Join(rows, "\n"))
	if scrollHint != "" {
		body += "\n" + stySub.Render(scrollHint)
	}
	return body
}

// resultLines builds the full (unscrolled) content of the current result tab.
func (m Model) resultLines() []string {
	resCur := m.resCur
	if resCur >= len(m.results) {
		resCur = len(m.results) - 1
	}
	rr := m.results[resCur]
	var rows []string

	if len(m.results) > 1 {
		tabs := make([]string, len(m.results))
		for i, r := range m.results {
			name := r.res.Kind.String()
			if i == resCur {
				tabs[i] = stySelected.Render(" " + name + " ")
			} else {
				tabs[i] = stySub.Render(" " + name + " ")
			}
		}
		rows = append(rows, strings.Join(tabs, ""), "")
	}

	base := history.Baseline(m.hist, rr.res.Kind.String(), m.info.Hostname, m.session)

	rows = append(rows, styHead.Render(kindTitle(rr.res.Kind)))
	if rr.res.Tool != "" {
		rows = append(rows, stySub.Render("measured with "+rr.res.Tool))
	}
	hint := "Bars run from left (worst) to right (best); the ↳ line says what the value means."
	if base != nil {
		hint += " Compared with your run on " + base.Time.Local().Format("Jan 2 15:04") + "."
	}
	rows = append(rows, stySub.Render(hint), "")

	if rr.err != nil {
		rows = append(rows, verdictStyle(bench.VPoor).Render("Test failed: "+rr.err.Error()))
	}

	anchorW := 12
	if m.width() > 90 {
		anchorW = 16
	}
	measure := m.width() - 4
	gaugeW := measure - 2*anchorW - 6
	if gaugeW < 14 {
		gaugeW = 14
	}
	if gaugeW > 96 {
		gaugeW = 96
	}
	for _, mt := range rr.res.Metrics {
		head := styMetric.Render("  "+mt.Name) + "   " + verdictStyle(mt.Verdict).Render(mt.Display)
		if b := verdictBadge(mt.Verdict); b != "" {
			head += "   " + b
		}
		if d := history.Compare(base, mt.Name, mt.Value, mt.LowerBetter); d.Valid {
			head += "   " + deltaStyle(d).Render(d.Label())
		}
		rows = append(rows, head)
		if mt.HasBar {
			lo := stySub.Render(padLeft(trim(mt.ScaleLo, anchorW), anchorW))
			hi := stySub.Render(trim(mt.ScaleHi, anchorW))
			rows = append(rows, "  "+lo+" "+gauge(mt.Bar, mt.Verdict, gaugeW)+" "+hi)
		}
		if mt.Note != "" {
			for i, ln := range strings.Split(wrap(mt.Note, measure-4), "\n") {
				pre := "  ↳ "
				if i > 0 {
					pre = "    "
				}
				rows = append(rows, styNote.Render(pre+ln))
			}
		}
		rows = append(rows, "")
	}
	if rr.res.Summary != "" {
		rows = append(rows, styBody.Render(wrap("→ "+rr.res.Summary, measure)))
	}
	if len(m.results) == 1 {
		rows = append(rows, "", stySub.Render("Press ⏎ for the menu - you can run the CPU, memory and network tests there too."))
	}
	return rows
}

// ---- history --------------------------------------------------

func (m Model) viewHistory() string {
	sessions := history.Sessions(m.hist)
	if len(sessions) == 0 {
		return styPanel.Width(m.width()).Render(stySub.Render(
			"No history yet.\n\nRun a test - every run is saved to\n" + history.Path()))
	}
	var rows []string
	rows = append(rows, styHead.Render("Past runs"),
		stySub.Render("Newest first. Open one to see how each number moved since the run before it."), "")
	for i, s := range sessions {
		marker := "  "
		nameSty := styItem
		if i == m.hcur {
			marker = styKey.Render("▶ ")
			nameSty = styMetric
		}
		line := fmt.Sprintf("%s%s  %s  %s",
			marker,
			nameSty.Render(s.Time.Local().Format("2006-01-02 15:04")),
			stySub.Render(padRight(trim(s.Host, 16), 16)),
			stySub.Render(strings.Join(s.Kinds(), ", ")))
		rows = append(rows, line)
	}
	return styPanel.Width(m.width()).Render(strings.Join(rows, "\n"))
}

func (m Model) viewHistoryView() string {
	rows := m.historyLines()
	avail := m.bodyHeight()
	hint := ""
	if len(rows) > avail {
		maxS := len(rows) - avail
		s := m.scroll
		if s > maxS {
			s = maxS
		}
		rows = rows[s : s+avail]
		if s > 0 && s < maxS {
			hint = "  ▲▼ scroll"
		} else if s < maxS {
			hint = "  ▼ more below"
		} else {
			hint = "  ▲ top hidden"
		}
	}
	body := styPanel.Width(m.width()).Render(strings.Join(rows, "\n"))
	if hint != "" {
		body += "\n" + stySub.Render(hint)
	}
	return body
}

func (m Model) historyLines() []string {
	if m.hview == nil {
		return []string{stySub.Render("nothing selected")}
	}
	s := *m.hview
	var older []history.Record
	for _, r := range m.hist {
		if r.Time.Before(s.Time) {
			older = append(older, r)
		}
	}
	var rows []string
	rows = append(rows, styHead.Render("Run "+s.Time.Local().Format("2006-01-02 15:04:05")),
		stySub.Render(s.Host+" · depth "+s.Depth), "")
	for _, r := range s.Records {
		rows = append(rows, styMetric.Render(strings.ToUpper(r.Kind)))
		if r.Tool != "" {
			rows = append(rows, stySub.Render("  measured with "+r.Tool))
		}
		if r.Failed {
			rows = append(rows, verdictStyle(bench.VPoor).Render("  failed: "+r.Error))
		}
		base := history.Baseline(older, r.Kind, r.Host, s.ID)
		for _, mt := range r.Metrics {
			line := "  " + styBody.Render(padRight(mt.Name, 34)) + " " + mt.Display
			if mt.Verdict != "" {
				line += "  " + histVerdictStyle(mt.Verdict).Render("● "+mt.Verdict)
			}
			if d := history.Compare(base, mt.Name, mt.Value, mt.LowerBetter); d.Valid {
				line += "  " + deltaStyle(d).Render(d.Label())
			}
			rows = append(rows, line)
		}
		if r.Summary != "" {
			rows = append(rows, stySub.Render(wrap("  → "+r.Summary, m.width()-6)))
		}
		rows = append(rows, "")
	}
	return rows
}

func histVerdictStyle(label string) lipgloss.Style {
	switch label {
	case "good":
		return lipgloss.NewStyle().Foreground(colGood)
	case "ok":
		return lipgloss.NewStyle().Foreground(colOkay)
	case "low":
		return lipgloss.NewStyle().Foreground(colPoor)
	}
	return lipgloss.NewStyle().Foreground(colDim)
}

// ---- small helpers ---------------------------------------------

func has(name string) bool { return bench.HasTool(name) }

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
func padRight(s string, n int) string {
	if lipgloss.Width(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-lipgloss.Width(s))
}
func padLeft(s string, n int) string {
	if lipgloss.Width(s) >= n {
		return s
	}
	return strings.Repeat(" ", n-lipgloss.Width(s)) + s
}
func indent(s, pad string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func wrap(s string, width int) string {
	if width < 20 {
		width = 20
	}
	words := strings.Fields(s)
	var b strings.Builder
	ll := 0
	for _, w := range words {
		if ll+len(w)+1 > width && ll > 0 {
			b.WriteByte('\n')
			ll = 0
		} else if ll > 0 {
			b.WriteByte(' ')
			ll++
		}
		b.WriteString(w)
		ll += len(w)
	}
	return b.String()
}
