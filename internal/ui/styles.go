package ui

import (
	"strings"

	"ptop/internal/bench"
	"ptop/internal/history"

	"github.com/charmbracelet/lipgloss"
)

// Colour tokens, filled by applyTheme. Three text tiers with deliberate
// contrast steps:
//
//	colHi   headings / metric names / primary values  (brightest)
//	colFg   body prose, notes                          (mid)
//	colDim  scale labels, meta lines, help             (dimmest)
var (
	colBg     lipgloss.Color
	colHi     lipgloss.Color
	colFg     lipgloss.Color
	colDim    lipgloss.Color
	colMuted  lipgloss.Color // alias of colFg, kept for older call sites
	colAccent lipgloss.Color
	colGood   lipgloss.Color
	colOkay   lipgloss.Color
	colPoor   lipgloss.Color
	colTrack  lipgloss.Color
)

var (
	styTitle       lipgloss.Style
	stySub         lipgloss.Style
	styKey         lipgloss.Style
	styHelp        lipgloss.Style
	styPanel       lipgloss.Style
	styPanelActive lipgloss.Style
	stySelected    lipgloss.Style
	styItem        lipgloss.Style
	styValue       lipgloss.Style
	styNote        lipgloss.Style
	styHead        lipgloss.Style
	styBody        lipgloss.Style
	styMetric      lipgloss.Style
)

// activeThemeKey is the key of the theme currently applied.
var activeThemeKey string

// applyTheme swaps every colour token and rebuilds the derived styles. Safe to
// call at runtime (the Appearance picker does, for a live preview).
func applyTheme(t Theme) {
	activeThemeKey = t.Key

	colBg = lipgloss.Color(t.Bg)
	colHi = lipgloss.Color(t.Hi)
	colFg = lipgloss.Color(t.Fg)
	colMuted = colFg
	colDim = lipgloss.Color(t.Dim)
	colAccent = lipgloss.Color(t.Accent)
	colGood = lipgloss.Color(t.Good)
	colOkay = lipgloss.Color(t.Okay)
	colPoor = lipgloss.Color(t.Poor)
	colTrack = lipgloss.Color(t.Track)

	styTitle = lipgloss.NewStyle().Bold(true).Foreground(colBg).Background(colAccent).Padding(0, 1)
	stySub = lipgloss.NewStyle().Foreground(colDim)
	styKey = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styHelp = lipgloss.NewStyle().Foreground(colDim)

	styPanel = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colDim).
		Padding(0, 1)
	styPanelActive = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(0, 1)

	stySelected = lipgloss.NewStyle().Foreground(colBg).Background(colAccent).Bold(true)
	styItem = lipgloss.NewStyle().Foreground(colHi)
	styValue = lipgloss.NewStyle().Foreground(colAccent)
	styNote = lipgloss.NewStyle().Foreground(colFg)
	styHead = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styBody = lipgloss.NewStyle().Foreground(colHi)
	styMetric = lipgloss.NewStyle().Bold(true).Foreground(colHi)
}

func init() { applyTheme(defaultTheme()) }

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
