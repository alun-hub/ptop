package ui

import (
	"strings"
	"testing"

	"ptop/internal/bench"

	tea "github.com/charmbracelet/bubbletea"
)

// exercises every screen's View() for panics and empty output.
func TestScreensRender(t *testing.T) {
	m := New()
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = nm.(Model)

	screens := []screen{scrMenu, scrConfig, scrPreflight, scrRunning, scrResults, scrHistory, scrHistoryView}
	for _, s := range screens {
		mm := m
		mm.scr = s
		if s == scrRunning {
			mm.cur = bench.Config{Kind: bench.CPU}
		}
		if s == scrResults {
			mm.results = []runResult{{res: bench.Result{
				Kind: bench.CPU, Tool: "test",
				Metrics: []bench.Metric{{Name: "X", Display: "1", Verdict: bench.VGood, Note: "n"}},
				Summary: "summary",
			}}}
		}
		out := mm.View()
		if strings.TrimSpace(out) == "" {
			t.Fatalf("screen %d rendered empty", s)
		}
	}
}

func TestKeyFlowToPreflight(t *testing.T) {
	var mdl tea.Model = New()
	mdl, _ = mdl.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	// menu: down to CPU, enter
	mdl, _ = mdl.Update(tea.KeyMsg{Type: tea.KeyDown})
	mdl, _ = mdl.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if mdl.(Model).scr != scrConfig {
		t.Fatalf("expected config screen, got %d", mdl.(Model).scr)
	}
	mdl, _ = mdl.Update(tea.KeyMsg{Type: tea.KeyEnter}) // depth field -> preflight
	if mdl.(Model).scr != scrPreflight {
		t.Fatalf("expected preflight, got %d", mdl.(Model).scr)
	}
}
