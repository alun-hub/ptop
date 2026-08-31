package ui

import (
	"strings"

	"ptop/internal/bench"
	"ptop/internal/history"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Three text tiers with deliberate contrast steps:
	//   colHi   headings / metric names / primary values  (brightest)
	//   colFg   body prose, notes                          (mid)
	//   colDim  scale labels, meta lines, help             (dimmest)
	colBg     = lipgloss.Color("#1c1f2e")
	colHi     = lipgloss.Color("#ffffff")
	colFg     = lipgloss.Color("#aab2d5")
	colDim    = lipgloss.Color("#6a7196")
	colMuted  = lipgloss.Color("#aab2d5") // kept as an alias for colFg
	colAccent = lipgloss.Color("#7ee0ff")
	colGood   = lipgloss.Color("#b6f05f")
	colOkay   = lipgloss.Color("#ffcf5c")
	colPoor   = lipgloss.Color("#ff7d99")
	colTrack  = lipgloss.Color("#333a57")

	styTitle = lipgloss.NewStyle().Bold(true).Foreground(colBg).Background(colAccent).Padding(0, 1)
	stySub   = lipgloss.NewStyle().Foreground(colDim)
	styKey   = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styHelp  = lipgloss.NewStyle().Foreground(colDim)

	styPanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colDim).
			Padding(0, 1)
	styPanelActive = styPanel.BorderForeground(colAccent)

	stySelected = lipgloss.NewStyle().Foreground(colBg).Background(colAccent).Bold(true)
	styItem     = lipgloss.NewStyle().Foreground(colHi)
	styValue    = lipgloss.NewStyle().Foreground(colAccent)
	styNote     = lipgloss.NewStyle().Foreground(colFg)
	styHead     = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styBody     = lipgloss.NewStyle().Foreground(colHi)
	styMetric   = lipgloss.NewStyle().Bold(true).Foreground(colHi)
)

// gauge renders a width-char bar for frac in 0..1, filled in the verdict colour
// and tracked in a dim rail.
func gauge(frac float64, v bench.Verdict, width int) string {
	if width < 4 {
		width = 4
	}
	switch {
	case frac < 0:
		frac = 0
	case frac > 1:
		frac = 1
	}
	fill := int(frac*float64(width) + 0.5)
	bar := verdictStyle(v).Render(strings.Repeat("█", fill))
	rail := lipgloss.NewStyle().Foreground(colTrack).Render(strings.Repeat("╌", width-fill))
	return bar + rail
}

func verdictStyle(v bench.Verdict) lipgloss.Style {
	switch v {
	case bench.VGood:
		return lipgloss.NewStyle().Foreground(colGood).Bold(true)
	case bench.VOkay:
		return lipgloss.NewStyle().Foreground(colOkay).Bold(true)
	case bench.VPoor:
		return lipgloss.NewStyle().Foreground(colPoor).Bold(true)
	}
	return lipgloss.NewStyle().Foreground(colHi)
}

func deltaStyle(d history.Delta) lipgloss.Style {
	switch {
	case d.Pct > 2:
		return lipgloss.NewStyle().Foreground(colGood)
	case d.Pct < -2:
		return lipgloss.NewStyle().Foreground(colPoor)
	}
	return lipgloss.NewStyle().Foreground(colDim)
}

func verdictBadge(v bench.Verdict) string {
	if v == bench.VNeutral {
		return ""
	}
	return verdictStyle(v).Render("● " + v.Label())
}
