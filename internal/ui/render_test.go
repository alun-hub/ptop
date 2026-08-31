package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"ptop/internal/bench"
	"ptop/internal/history"

	tea "github.com/charmbracelet/bubbletea"
)

// exercises every screen's View() for panics and empty output.
func TestScreensRender(t *testing.T) {
	m := New()
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = nm.(Model)

	screens := []screen{scrMenu, scrConfig, scrPreflight, scrRunning, scrResults,
		scrHistArea, scrHistory, scrHistoryView, scrHistOverview, scrHistMetric}
	for _, s := range screens {
		mm := m
		mm.scr = s
		if s == scrRunning {
			mm.cur = bench.Config{Kind: bench.CPU}
		}
		if s == scrHistOverview || s == scrHistMetric {
			mm.harea = "CPU"
			mm.hmName = "Single-threaded"
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

func TestHistoryDeleteKeyFlow(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess := history.NewSession()
	r := bench.Result{Kind: bench.CPU, Tool: "test", Metrics: []bench.Metric{{Name: "X", Value: 1}}}
	_ = history.Save(sess, "host", "normal", "", r, nil)

	m := New()
	m.hist, _ = history.Load()
	m.scr = scrHistory

	// Pressing 'd' should set confirmDel
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = nm.(Model)
	if m.confirmDel != sess {
		t.Fatalf("expected confirmDel %s, got %s", sess, m.confirmDel)
	}

	// Pressing 'n' cancels confirmation
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = nm.(Model)
	if m.confirmDel != "" {
		t.Fatalf("expected confirmDel to be cleared, got %s", m.confirmDel)
	}

	// Pressing 'd' then 'y' deletes the session
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = nm.(Model)
	if m.confirmDel != "" {
		t.Fatalf("expected confirmDel to be cleared after y, got %s", m.confirmDel)
	}
	if len(m.hist) != 0 {
		t.Fatalf("expected m.hist to be empty, got %d records", len(m.hist))
	}
}

func TestHistoryViewDeleteKeyFlow(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess := history.NewSession()
	r := bench.Result{Kind: bench.CPU, Tool: "test", Metrics: []bench.Metric{{Name: "X", Value: 1}}}
	_ = history.Save(sess, "host", "normal", "", r, nil)

	m := New()
	m.hist, _ = history.Load()
	sessions := history.Sessions(m.hist)
	m.hview = &sessions[0]
	m.scr = scrHistoryView

	// Pressing 'd' sets confirmDel
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = nm.(Model)
	if m.confirmDel != sess {
		t.Fatalf("expected confirmDel %s, got %s", sess, m.confirmDel)
	}

	// Pressing 'y' deletes and navigates back to scrHistory
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = nm.(Model)
	if m.scr != scrHistory {
		t.Fatalf("expected scrHistory after deleting view, got %d", m.scr)
	}
	if m.confirmDel != "" {
		t.Fatalf("expected confirmDel cleared, got %s", m.confirmDel)
	}
	if len(m.hist) != 0 {
		t.Fatalf("expected m.hist to be empty, got %d records", len(m.hist))
	}
}

func TestHistoryFooterConfirmRender(t *testing.T) {
	m := New()
	m.w, m.h = 100, 40
	m.scr = scrHistory
	m.confirmDel = "some-session-id"

	out := m.View()
	if !strings.Contains(out, "Delete run") || !strings.Contains(out, "y confirm") {
		t.Fatalf("expected delete confirmation in footer, got:\n%s", out)
	}

	// Normal footer should contain "d delete"
	m.confirmDel = ""
	outNormal := m.View()
	if !strings.Contains(outNormal, "d delete") {
		t.Fatalf("expected d delete in history footer, got:\n%s", outNormal)
	}

	// In scrHistoryView with confirmDel
	m.scr = scrHistoryView
	m.confirmDel = "some-session-id"
	outView := m.View()
	if !strings.Contains(outView, "Delete run") || !strings.Contains(outView, "y confirm") {
		t.Fatalf("expected delete confirmation in history view footer, got:\n%s", outView)
	}

	// Normal footer for scrHistoryView
	m.confirmDel = ""
	outViewNormal := m.View()
	if !strings.Contains(outViewNormal, "d delete") {
		t.Fatalf("expected d delete in history view footer, got:\n%s", outViewNormal)
	}
}
