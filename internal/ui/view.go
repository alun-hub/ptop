package ui

import (
	"fmt"
	"strings"

	"ptop/internal/bench"
	"ptop/internal/history"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
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
	case scrHistArea:
		body = m.viewHistArea()
	case scrHistory:
		body = m.viewHistory()
	case scrHistoryView:
		body = m.viewHistoryView()
	case scrHistOverview:
		body = m.viewHistOverview()
	case scrHistMetric:
		body = m.viewHistMetric()
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
	if m.confirmDel != "" && (m.scr == scrHistory || m.scr == scrHistoryView) {
		timeStr := ""
		for _, r := range m.hist {
			if r.Session == m.confirmDel {
				timeStr = r.Time.Local().Format("2006-01-02 15:04:05")
				break
			}
		}
		prompt := "Delete run " + timeStr + "?   " + styKey.Render("y") + " confirm   ·   " + styKey.Render("n") + "/" + styKey.Render("esc") + " cancel"
		return stySub.Render(strings.Repeat("─", m.width())) + "\n" + lipgloss.NewStyle().Foreground(colPoor).Bold(true).Render("▶ ") + prompt
	}

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
	case scrHistArea:
		keys = "↑/↓ select   ⏎ open   esc menu   q quit"
	case scrHistory:
		keys = "↑/↓ select   ⏎ open   d delete   esc back   q quit"
	case scrHistoryView:
		keys = "↑/↓ scroll   d delete   esc back to list   q quit"
	case scrHistOverview:
		keys = "↑/↓ metric   ⏎ chart   h hosts   esc back   q quit"
	case scrHistMetric:
		keys = "↑/↓ scroll   h hosts   esc back   q quit"
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

// ---- history: categorised (by test area) ---------------------

func (m Model) viewHistArea() string {
	if len(m.hist) == 0 {
		return styPanel.Width(m.width()).Render(stySub.Render(
			"No history yet.\n\nRun a test - every run is saved to\n" + history.Path()))
	}
	choices := m.areaChoices()
	var rows []string
	rows = append(rows, styHead.Render("History"),
		stySub.Render(fmt.Sprintf("%d runs recorded. Pick a test area to see its numbers over time.", len(m.hist))), "")
	for i, c := range choices {
		marker, ns := "  ", styItem
		if i == m.hcur {
			marker, ns = styKey.Render("▶ "), styMetric
		}
		desc := ""
		if i == 0 {
			desc = stySub.Render("   full results of each individual run")
		} else {
			n := 0
			for _, r := range m.hist {
				if !r.Failed && strings.EqualFold(r.Kind, c) {
					n++
				}
			}
			desc = stySub.Render(fmt.Sprintf("   %d runs", n))
		}
		rows = append(rows, marker+ns.Render(padRight(c, 16))+desc)
	}
	return styPanel.Width(m.width()).Render(strings.Join(rows, "\n"))
}

func (m Model) viewHistOverview() string {
	host := m.hostFilter()
	metrics := history.MetricNames(m.hist, m.harea, host)
	var rows []string
	scope := "host: " + m.info.Hostname
	if m.hAllHosts {
		scope = "all hosts"
	}
	rows = append(rows, styHead.Render(m.harea+" history"))
	if len(metrics) == 0 {
		rows = append(rows, "", stySub.Render("No comparable "+m.harea+" metrics recorded for "+scope+"."))
		return styPanel.Width(m.width()).Render(strings.Join(rows, "\n"))
	}
	// window span from the first metric's series
	span := ""
	if s := history.Series(m.hist, m.harea, metrics[0].Name, host); len(s) > 0 {
		span = s[0].Time.Local().Format("Jan 2") + " → " + s[len(s)-1].Time.Local().Format("Jan 2")
	}
	rows = append(rows, stySub.Render(span+"  ·  "+scope+"  ·  h toggles hosts  ·  sparkline = oldest→newest"), "")

	nameW := 26
	sparkW := m.width() - nameW - 32
	if sparkW < 8 {
		sparkW = 8
	}
	if sparkW > 34 {
		sparkW = 34
	}
	for i, mi := range metrics {
		pts := history.Series(m.hist, m.harea, mi.Name, host)
		st := history.Summarize(pts, mi.LowerBetter)
		marker := "  "
		nameSty := styBody
		if i == m.haCur {
			marker = styKey.Render("▶ ")
			nameSty = styMetric
		}
		latest := ""
		vsty := stySub
		if st.N > 0 {
			latest = st.Last.Display
			vsty = histVerdictStyle(st.Last.Verdict)
		}
		spark := history.Sparkline(pts, sparkW)
		row := marker + nameSty.Render(padRight(trim(mi.Name, nameW), nameW)) +
			vsty.Render(padLeft(latest, 13)) + "  " +
			vsty.Render(spark) + strings.Repeat(" ", max(0, sparkW-lipgloss.Width(spark))) + "  " +
			trendCell(st)
		rows = append(rows, strings.TrimRight(row, " "))
	}
	rows = append(rows, "", stySub.Render("⏎ opens a full chart for the selected metric."))
	return styPanel.Width(m.width()).Render(strings.Join(rows, "\n"))
}

func trendCell(st history.Stats) string {
	if !st.HasWindow {
		return stySub.Render("first run")
	}
	p := st.OverWindowPct
	txt := fmt.Sprintf("%+.0f%%", p)
	if p > -1 && p < 1 {
		txt = "flat"
	}
	sty := stySub
	switch {
	case p >= 3:
		sty = lipgloss.NewStyle().Foreground(colGood)
	case p <= -10:
		sty = lipgloss.NewStyle().Foreground(colPoor)
		txt += " ⚠"
	case p <= -3:
		sty = lipgloss.NewStyle().Foreground(colOkay)
	}
	return sty.Render(txt)
}

func (m Model) viewHistMetric() string {
	rows := m.histMetricLines()
	avail := m.bodyHeight()
	hint := ""
	if len(rows) > avail {
		maxS := len(rows) - avail
		s := m.scroll
		if s > maxS {
			s = maxS
		}
		rows = rows[s : s+avail]
		if s < maxS {
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

func (m Model) histMetricLines() []string {
	host := m.hostFilter()
	metrics := history.MetricNames(m.hist, m.harea, host)
	var mi history.MetricInfo
	for _, x := range metrics {
		if x.Name == m.hmName {
			mi = x
		}
	}
	pts := history.Series(m.hist, m.harea, m.hmName, host)
	st := history.Summarize(pts, mi.LowerBetter)

	scope := "host: " + m.info.Hostname
	if m.hAllHosts {
		scope = "all hosts"
	}
	unit := mi.Unit
	if unit == "" {
		unit = "value"
	}
	var rows []string
	rows = append(rows, styHead.Render(m.harea+" · "+m.hmName),
		stySub.Render(unit+"  ·  "+scope+"  ·  lower is better: "+yesno(mi.LowerBetter)), "")

	rows = append(rows, m.metricChart(pts, mi.LowerBetter)...)
	rows = append(rows, "")

	if st.N == 0 {
		rows = append(rows, stySub.Render("No data for this metric on "+scope+"."))
		return rows
	}
	rows = append(rows,
		styBody.Render(fmt.Sprintf("latest %s   ·   best %s (%s)   ·   worst %s (%s)",
			st.Last.Display, st.Max.Display, st.Max.Time.Local().Format("Jan 2"),
			st.Min.Display, st.Min.Time.Local().Format("Jan 2"))))
	if mi.LowerBetter {
		// for lower-better, "best" is the min
		rows[len(rows)-1] = styBody.Render(fmt.Sprintf("latest %s   ·   best %s (%s)   ·   worst %s (%s)",
			st.Last.Display, st.Min.Display, st.Min.Time.Local().Format("Jan 2"),
			st.Max.Display, st.Max.Time.Local().Format("Jan 2")))
	}
	deltas := []string{}
	if st.HasWindow {
		deltas = append(deltas, fmt.Sprintf("%+.1f%% over the window", st.OverWindowPct))
	}
	if st.HasPrevious {
		deltas = append(deltas, fmt.Sprintf("%+.1f%% vs previous run (%s)",
			st.VsPreviousPct, pts[len(pts)-2].Time.Local().Format("Jan 2")))
	}
	if len(deltas) > 0 {
		rows = append(rows, styNote.Render(strings.Join(deltas, "   ·   ")))
	}
	if st.Last.Note != "" {
		rows = append(rows, histVerdictStyle(st.Last.Verdict).Render("● "+st.Last.Verdict)+" "+styNote.Render(st.Last.Note))
	}
	rows = append(rows, "",
		stySub.Render(fmt.Sprintf("  %-17s %-14s %-9s %-8s %s", "run", "value", "Δ prev", "verdict", "depth")))
	for i := len(pts) - 1; i >= 0; i-- {
		p := pts[i]
		dprev := ""
		if i > 0 && pts[i-1].Value != 0 {
			pc := (p.Value - pts[i-1].Value) / pts[i-1].Value * 100
			if mi.LowerBetter {
				pc = -pc
			}
			dprev = fmt.Sprintf("%+.1f%%", pc)
		}
		rows = append(rows, fmt.Sprintf("  %-17s %-14s %s %s %s",
			p.Time.Local().Format("Jan 2 15:04"),
			p.Display,
			padRight(histDeltaStyle(dprev).Render(dprev), 9),
			histVerdictStyle(p.Verdict).Render(padRight(p.Verdict, 8)),
			stySub.Render(p.Depth)))
	}
	return rows
}

func (m Model) metricChart(pts []history.Point, lowerBetter bool) []string {
	if len(pts) < 2 {
		return []string{stySub.Render("  (need at least two runs to draw a chart)")}
	}
	w := m.width() - 4
	if w > 120 {
		w = 120
	}
	h := m.bodyHeight() - 15
	if h < 6 {
		h = 6
	}
	if h > 13 {
		h = 13
	}

	// Y range padded around the data so the line uses the whole height instead
	// of being pinned near a zero baseline.
	lo, hi := pts[0].Value, pts[0].Value
	for _, p := range pts {
		lo = min64(lo, p.Value)
		hi = max64(hi, p.Value)
	}
	pad := (hi - lo) * 0.12
	if pad == 0 {
		pad = hi*0.05 + 1
	}
	lo, hi = lo-pad, hi+pad

	tc := timeserieslinechart.New(w, h,
		timeserieslinechart.WithYLabelFormatter(func(_ int, v float64) string { return compactNum(v) }),
	)
	tc.SetStyle(lipgloss.NewStyle().Foreground(colAccent))
	tc.AxisStyle = lipgloss.NewStyle().Foreground(colDim)
	tc.LabelStyle = lipgloss.NewStyle().Foreground(colDim)
	for _, p := range pts {
		tc.Push(timeserieslinechart.TimePoint{Time: p.Time, Value: p.Value})
	}
	tc.SetYRange(lo, hi)
	tc.SetViewYRange(lo, hi)
	tc.DrawBraille()
	return strings.Split(tc.View(), "\n")
}

func compactNum(v float64) string {
	a := v
	if a < 0 {
		a = -a
	}
	switch {
	case a >= 1e9:
		return fmt.Sprintf("%.1fG", v/1e9)
	case a >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case a >= 1e4:
		return fmt.Sprintf("%.0fk", v/1e3)
	case a >= 100:
		return fmt.Sprintf("%.0f", v)
	case a >= 1:
		return fmt.Sprintf("%.1f", v)
	default:
		return fmt.Sprintf("%.2f", v)
	}
}

func histDeltaStyle(s string) lipgloss.Style {
	switch {
	case strings.HasPrefix(s, "+"):
		return lipgloss.NewStyle().Foreground(colGood)
	case strings.HasPrefix(s, "-"):
		return lipgloss.NewStyle().Foreground(colPoor)
	}
	return lipgloss.NewStyle().Foreground(colDim)
}

func yesno(b bool) string {
	if b {
		return "yes"
	}
	return "no"
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
func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func max64(a, b float64) float64 {
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
