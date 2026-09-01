package ui

import (
	"os"
	"path/filepath"
	"strings"
)

// Theme is one named colour palette for the TUI. Hex strings are wrapped into
// lipgloss colours by applyTheme (styles.go).
type Theme struct {
	Key   string
	Name  string
	Blurb string

	Bg     string // panel / chip ground
	Hi     string // headings, metric names, values
	Fg     string // notes, body prose
	Dim    string // scale labels, help, breadcrumb
	Accent string // chip, keys, active border, values
	Good   string // verdict: good
	Okay   string // verdict: ok
	Poor   string // verdict: low
	Track  string // unfilled gauge rail
}

// themes are offered in the Appearance picker, in this order. The first entry
// that also matches defaultThemeKey is the out-of-the-box default.
var themes = []Theme{
	{
		Key: "signal", Name: "Signal",
		Blurb: "Punchy — saturated bars, a cold cyan accent, a deep ground. The gauge reads as an instrument.",
		Bg:    "#12141c", Hi: "#ffffff", Fg: "#e6ebf5", Dim: "#8892b0", Accent: "#22d3ee",
		Good: "#5ef38c", Okay: "#fbbf24", Poor: "#fb4d6a", Track: "#2b3350",
	},
	{
		Key: "faded-storm", Name: "Faded Storm",
		Blurb: "The original palette — a lightened Tokyo Night. Calm and even, lower contrast.",
		Bg:    "#1c1f2e", Hi: "#ffffff", Fg: "#dce2f7", Dim: "#9aa6cc", Accent: "#7ee0ff",
		Good: "#b6f05f", Okay: "#ffd166", Poor: "#ff7d99", Track: "#4e587d",
	},
	{
		Key: "tokyo-night", Name: "Tokyo Night",
		Blurb: "The real Tokyo Night — a blue accent instead of cyan, muted but properly saturated.",
		Bg:    "#1a1b26", Hi: "#ffffff", Fg: "#c0caf5", Dim: "#565f89", Accent: "#7aa2f7",
		Good: "#9ece6a", Okay: "#e0af68", Poor: "#f7768e", Track: "#3b4261",
	},
	{
		Key: "gruvbox", Name: "Gruvbox",
		Blurb: "Warm — cream text on a brown-black ground, retro amber accent. High saturation, low glare.",
		Bg:    "#1d2021", Hi: "#fbf1c7", Fg: "#ebdbb2", Dim: "#928374", Accent: "#fabd2f",
		Good: "#b8bb26", Okay: "#fe8019", Poor: "#fb4934", Track: "#504945",
	},
	{
		Key: "neon-grid", Name: "Neon Grid",
		Blurb: "Maximum punch — a near-black ground and electric bars. Built for a wall display.",
		Bg:    "#0a0e14", Hi: "#ffffff", Fg: "#d8e0f0", Dim: "#5c6a8a", Accent: "#00e5ff",
		Good: "#00ffa3", Okay: "#ffd400", Poor: "#ff3d6e", Track: "#1c2333",
	},
}

const defaultThemeKey = "signal"

func defaultTheme() Theme {
	t, _ := themeByKey(defaultThemeKey)
	return t
}

func themeByKey(k string) (Theme, bool) {
	for _, t := range themes {
		if t.Key == k {
			return t, true
		}
	}
	return Theme{}, false
}

func themeIndex(k string) int {
	for i, t := range themes {
		if t.Key == k {
			return i
		}
	}
	return 0
}

// themePrefPath is where the chosen theme key is remembered between runs.
// Override the whole path with PTOP_THEME_FILE (used by tests).
func themePrefPath() string {
	if p := os.Getenv("PTOP_THEME_FILE"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "ptop", "theme")
}

// loadThemeKey resolves the theme to use: PTOP_THEME wins, then the saved
// preference file, then the built-in default.
func loadThemeKey() string {
	if e := strings.TrimSpace(os.Getenv("PTOP_THEME")); e != "" {
		if _, ok := themeByKey(e); ok {
			return e
		}
	}
	if b, err := os.ReadFile(themePrefPath()); err == nil {
		if k := strings.TrimSpace(string(b)); k != "" {
			if _, ok := themeByKey(k); ok {
				return k
			}
		}
	}
	return defaultThemeKey
}

func saveThemeKey(k string) {
	p := themePrefPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(p, []byte(k+"\n"), 0o644)
}
