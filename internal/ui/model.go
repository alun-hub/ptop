package ui

import (
	"context"
	"fmt"
	"strings"

	"ptop/internal/bench"
	"ptop/internal/history"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type screen int

const (
	scrMenu screen = iota
	scrConfig
	scrPreflight
	scrRunning
	scrResults
	scrHistArea
	scrHistory
	scrHistoryView
	scrHistOverview
	scrHistMetric
)

type menuItem struct {
	kind  bench.Kind
	all   bool
	hist  bool
	title string
	desc  string
	time  string
}

// ---- messages ------------------------------------------------------------

type benchStartedMsg struct{ ch chan bench.Event }
type eventMsg struct{ ev bench.Event }
type channelClosedMsg struct{}

func waitEvent(ch chan bench.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return channelClosedMsg{}
		}
		return eventMsg{ev}
	}
}

// ---- model -------------------------------------------------------------

type Model struct {
	w, h int
	info bench.SysInfo
	scr  screen

	menu    []menuItem
	menuCur int

	// config
	kind      bench.Kind
	isAll     bool
	depth     bench.Depth
	targets   []string
	targetCur int
	host      textinput.Model
	cfgCur    int

	// running
	queue  []bench.Config
	cur    bench.Config
	ch     chan bench.Event
	prog   progress.Model
	spin   spinner.Model
	logs   []string
	label  string
	cancel context.CancelFunc

	// results
	results []runResult
	resCur  int
	scroll  int

	// history
	session   string
	hist      []history.Record
	hcur      int // cursor on the history-area chooser / session list
	hview     *history.Session
	harea     string // selected test area
	haCur     int    // selected metric row in the area overview
	hmName    string // selected metric for the chart screen
	hAllHosts bool
}

type runResult struct {
	res bench.Result
	err error
}

func New() Model {
	info := bench.Info()
	targets := bench.DiskTargets()
	if len(targets) == 0 {
		targets = []string{"."}
	}
	ti := textinput.New()
	ti.Placeholder = "IP or hostname (optional - needs 'iperf3 -s' running on the other end)"
	ti.CharLimit = 120
	ti.Prompt = ""

	p := progress.New(progress.WithDefaultGradient(), progress.WithWidth(46))
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return Model{
		info:    info,
		scr:     scrMenu,
		depth:   bench.Normal,
		targets: targets,
		host:    ti,
		prog:    p,
		spin:    sp,
		menu: []menuItem{
			{kind: bench.Disk, title: "Disk performance", time: "~1-3 min",
				desc: "How fast the disk reads and writes large files, and how many small\nrandom operations it can handle (important for databases)."},
			{kind: bench.CPU, title: "Processor (CPU)", time: "~1 min",
				desc: "Compute power on one core and on all cores, and how well the\nwork scales across the cores."},
			{kind: bench.Mem, title: "Memory (RAM)", time: "~30 s",
				desc: "How much memory is free, how fast it can be read/written, and\nits random-access latency."},
			{kind: bench.Net, title: "Network", time: "~30 s",
				desc: "Latency and throughput - against your own server if you name one,\notherwise a public download test."},
			{kind: bench.GPU, title: "GPU", time: "~5 s",
				desc: "What GPU is present, its VRAM, load and temperature. Reports status\nonly - no compute benchmark unless glmark2 is installed."},
			{all: true, title: "Run all tests", time: "~3-6 min",
				desc: "Disk, CPU, memory, network and GPU one after another. Good for a\nfull picture of a new server."},
			{hist: true, title: "History", time: "",
				desc: "Browse earlier runs and see how much faster or slower this machine\nhas become since then."},
		},
	}
}

func (m Model) Init() tea.Cmd { return nil }

// ---- update ----------------------------------------------------------

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		switch m.scr {
		case scrMenu:
			return m.updateMenu(msg)
		case scrConfig:
			return m.updateConfig(msg)
		case scrPreflight:
			return m.updatePreflight(msg)
		case scrRunning:
			return m.updateRunning(msg)
		case scrResults:
			return m.updateResults(msg)
		case scrHistArea:
			return m.updateHistArea(msg)
		case scrHistory:
			return m.updateHistory(msg)
		case scrHistoryView:
			return m.updateHistoryView(msg)
		case scrHistOverview:
			return m.updateHistOverview(msg)
		case scrHistMetric:
			return m.updateHistMetric(msg)
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case progress.FrameMsg:
		pm, cmd := m.prog.Update(msg)
		m.prog = pm.(progress.Model)
		return m, cmd

	case benchStartedMsg:
		m.ch = msg.ch
		return m, tea.Batch(waitEvent(m.ch), m.spin.Tick)

	case eventMsg:
		return m.handleEvent(msg.ev)

	case channelClosedMsg:
		return m, nil
	}
	return m, nil
}

func (m Model) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.menuCur > 0 {
			m.menuCur--
		}
	case "down", "j":
		if m.menuCur < len(m.menu)-1 {
			m.menuCur++
		}
	case "enter", " ":
		it := m.menu[m.menuCur]
		if it.hist {
			m.hist, _ = history.Load()
			m.hcur = 0
			m.scr = scrHistArea
			return m, nil
		}
		m.isAll = it.all
		if !it.all {
			m.kind = it.kind
		}
		m.cfgCur = 0
		m.scr = scrConfig
		if m.configFields()[0] == fieldHost {
			m.host.Focus()
		}
	}
	return m, nil
}

func (m Model) updateHistory(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sessions := history.Sessions(m.hist)
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "left":
		m.scr = scrHistArea
		m.hcur = 0
	case "up", "k":
		if m.hcur > 0 {
			m.hcur--
		}
	case "down", "j":
		if m.hcur < len(sessions)-1 {
			m.hcur++
		}
	case "enter", "right", "l":
		if m.hcur < len(sessions) {
			s := sessions[m.hcur]
			m.hview = &s
			m.scroll = 0
			m.scr = scrHistoryView
		}
	}
	return m, nil
}

func (m Model) hostFilter() string {
	if m.hAllHosts {
		return ""
	}
	return m.info.Hostname
}

// areaChoices is the list shown on scrHistArea: "Recent runs" + areas with data.
func (m Model) areaChoices() []string {
	return append([]string{"Recent runs"}, history.Areas(m.hist)...)
}

func (m Model) updateHistArea(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	choices := m.areaChoices()
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.scr = scrMenu
	case "up", "k":
		if m.hcur > 0 {
			m.hcur--
		}
	case "down", "j":
		if m.hcur < len(choices)-1 {
			m.hcur++
		}
	case "enter", "right", "l":
		if m.hcur == 0 {
			m.scr = scrHistory
			return m, nil
		}
		m.harea = choices[m.hcur]
		m.haCur = 0
		m.scr = scrHistOverview
	}
	return m, nil
}

func (m Model) updateHistOverview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	metrics := history.MetricNames(m.hist, m.harea, m.hostFilter())
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "left":
		m.scr = scrHistArea
	case "up", "k":
		if m.haCur > 0 {
			m.haCur--
		}
	case "down", "j":
		if m.haCur < len(metrics)-1 {
			m.haCur++
		}
	case "h":
		m.hAllHosts = !m.hAllHosts
		m.haCur = 0
	case "enter", "right", "l":
		if m.haCur < len(metrics) {
			m.hmName = metrics[m.haCur].Name
			m.scroll = 0
			m.scr = scrHistMetric
		}
	}
	return m, nil
}

func (m Model) updateHistMetric(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "left":
		m.scr = scrHistOverview
	case "h":
		m.hAllHosts = !m.hAllHosts
	case "up", "k":
		m.scroll--
	case "down", "j":
		m.scroll++
	case "pgup", "b":
		m.scroll -= m.bodyHeight() - 2
	case "pgdown", " ", "f":
		m.scroll += m.bodyHeight() - 2
	}
	if maxS := len(m.histMetricLines()) - m.bodyHeight(); m.scroll > maxS {
		m.scroll = maxS
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	return m, nil
}

func (m Model) updateHistoryView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "left", "h":
		m.scr = scrHistory
	case "up", "k":
		m.scroll--
	case "down", "j":
		m.scroll++
	case "pgup", "b":
		m.scroll -= m.bodyHeight() - 2
	case "pgdown", " ", "f":
		m.scroll += m.bodyHeight() - 2
	}
	if maxS := len(m.historyLines()) - m.bodyHeight(); m.scroll > maxS {
		m.scroll = maxS
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	return m, nil
}

// config fields --------------------------------------------------------

type cfgField int

const (
	fieldTarget cfgField = iota
	fieldDepth
	fieldHost
)

func (m Model) configFields() []cfgField {
	if m.isAll {
		return []cfgField{fieldTarget, fieldHost, fieldDepth}
	}
	switch m.kind {
	case bench.Disk:
		return []cfgField{fieldTarget, fieldDepth}
	case bench.Net:
		return []cfgField{fieldHost, fieldDepth}
	default:
		return []cfgField{fieldDepth}
	}
}

func (m Model) updateConfig(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	fields := m.configFields()
	cur := fields[m.cfgCur]

	if cur == fieldHost && m.host.Focused() {
		switch msg.String() {
		case "esc":
			m.host.Blur()
			return m, nil
		case "enter", "tab", "down":
			m.host.Blur()
			if m.cfgCur < len(fields)-1 {
				m.cfgCur++
			}
			return m, nil
		case "up":
			m.host.Blur()
			if m.cfgCur > 0 {
				m.cfgCur--
			}
			return m, nil
		default:
			var cmd tea.Cmd
			m.host, cmd = m.host.Update(msg)
			return m, cmd
		}
	}

	switch msg.String() {
	case "esc":
		m.scr = scrMenu
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cfgCur > 0 {
			m.cfgCur--
		}
	case "down", "j", "tab":
		if m.cfgCur < len(fields)-1 {
			m.cfgCur++
		}
	case "left", "h":
		m.adjust(cur, -1)
	case "right", "l":
		m.adjust(cur, +1)
	case "enter":
		if cur == fieldHost {
			m.host.Focus()
			return m, textinput.Blink
		}
		m.scr = scrPreflight
	}
	return m, nil
}

func (m *Model) adjust(f cfgField, d int) {
	switch f {
	case fieldTarget:
		m.targetCur = (m.targetCur + d + len(m.targets)) % len(m.targets)
	case fieldDepth:
		nd := int(m.depth) + d
		if nd < 0 {
			nd = 0
		}
		if nd > int(bench.Deep) {
			nd = int(bench.Deep)
		}
		m.depth = bench.Depth(nd)
	}
}

func (m Model) updatePreflight(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.scr = scrConfig
	case "q", "ctrl+c":
		return m, tea.Quit
	case "enter", "y":
		return m.startRun()
	}
	return m, nil
}

func (m Model) buildQueue() []bench.Config {
	base := bench.Config{
		Depth:  m.depth,
		Path:   m.targets[m.targetCur],
		Host:   strings.TrimSpace(m.host.Value()),
		IsRoot: m.info.IsRoot,
	}
	kinds := []bench.Kind{m.kind}
	if m.isAll {
		kinds = []bench.Kind{bench.Disk, bench.CPU, bench.Mem, bench.Net, bench.GPU}
	}
	var q []bench.Config
	for _, k := range kinds {
		c := base
		c.Kind = k
		q = append(q, c)
	}
	return q
}

func (m Model) startRun() (tea.Model, tea.Cmd) {
	m.queue = m.buildQueue()
	m.results = nil
	m.session = history.NewSession()
	m.hist, _ = history.Load()
	m.scr = scrRunning
	return m.next()
}

func (m Model) next() (tea.Model, tea.Cmd) {
	if len(m.queue) == 0 {
		m.scr = scrResults
		m.resCur = 0
		m.scroll = 0
		return m, nil
	}
	m.cur = m.queue[0]
	m.queue = m.queue[1:]
	m.logs = nil
	m.label = "starting..."
	_ = m.prog.SetPercent(0)

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	cfg := m.cur
	start := func() tea.Msg {
		ch := make(chan bench.Event, 128)
		go bench.Run(ctx, cfg, ch)
		return benchStartedMsg{ch: ch}
	}
	return m, start
}

func (m Model) handleEvent(ev bench.Event) (tea.Model, tea.Cmd) {
	switch e := ev.(type) {
	case bench.LogLine:
		m.logs = append(m.logs, e.Text)
		if len(m.logs) > 200 {
			m.logs = m.logs[len(m.logs)-200:]
		}
		return m, waitEvent(m.ch)
	case bench.Progress:
		m.label = e.Label
		cmd := m.prog.SetPercent(e.Frac)
		return m, tea.Batch(cmd, waitEvent(m.ch))
	case bench.Finished:
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		m.results = append(m.results, runResult{res: e.Result, err: e.Err})
		_ = history.Save(m.session, m.info.Hostname, m.cur.Depth.Token(), e.Result, e.Err)
		return m.next()
	}
	return m, waitEvent(m.ch)
}

func (m Model) updateRunning(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		m.queue = nil
		m.scr = scrMenu
	case "q":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateResults(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "enter":
		m.scr = scrMenu
		m.scroll = 0
	case "left", "h":
		if m.resCur > 0 {
			m.resCur--
			m.scroll = 0
		}
	case "right", "l":
		if m.resCur < len(m.results)-1 {
			m.resCur++
			m.scroll = 0
		}
	case "up", "k":
		m.scroll--
	case "down", "j":
		m.scroll++
	case "pgup", "b":
		m.scroll -= m.bodyHeight() - 2
	case "pgdown", " ", "f":
		m.scroll += m.bodyHeight() - 2
	case "home", "g":
		m.scroll = 0
	case "end", "G":
		m.scroll = 1 << 20
	}
	if maxS := len(m.resultLines()) - m.bodyHeight(); m.scroll > maxS {
		m.scroll = maxS
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	return m, nil
}

// helper used by views
func kindTitle(k bench.Kind) string {
	switch k {
	case bench.Disk:
		return "Disk performance"
	case bench.CPU:
		return "Processor"
	case bench.Mem:
		return "Memory"
	case bench.Net:
		return "Network"
	case bench.GPU:
		return "GPU"
	}
	return "?"
}

func (m Model) depthOptions() string {
	var b strings.Builder
	for d := bench.Quick; d <= bench.Deep; d++ {
		s := d.String()
		if d == m.depth {
			b.WriteString(stySelected.Render(" " + s + " "))
		} else {
			b.WriteString(stySub.Render("  " + s + "  "))
		}
	}
	return b.String()
}

func fmtGB(v float64) string { return fmt.Sprintf("%.1f GB", v) }
