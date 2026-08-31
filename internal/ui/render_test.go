package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		scrHistArea, scrHistory, scrHistoryView, scrHistDiff, scrHistOverview, scrHistMetric}
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
		if s == scrHistDiff {
			dummy := history.Session{ID: "s1", Host: "h1"}
			mm.diffBase = &dummy
			mm.diffTarget = &dummy
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

func TestViewHistDiff(t *testing.T) {
	m := New()
	m.w, m.h = 100, 30
	s1 := history.Session{
		ID:    "s1",
		Host:  "server-a",
		Depth: "normal",
		Time:  time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC),
		Tag:   "Before SSD",
		Records: []history.Record{
			{
				Kind: "Disk",
				Metrics: []history.Metric{
					{Name: "Sequential write", Display: "450 MB/s", Value: 450, Unit: "MB/s", LowerBetter: false},
				},
			},
		},
	}
	s2 := history.Session{
		ID:    "s2",
		Host:  "server-a",
		Depth: "normal",
		Time:  time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC),
		Tag:   "After NVMe",
		Records: []history.Record{
			{
				Kind: "Disk",
				Metrics: []history.Metric{
					{Name: "Sequential write", Display: "1800 MB/s", Value: 1800, Unit: "MB/s", LowerBetter: false},
				},
			},
		},
	}
	m.hist = append(m.hist, s1.Records...)
	m.hist = append(m.hist, s2.Records...)
	m.diffBase = &s1
	m.diffTarget = &s2
	m.scr = scrHistDiff

	v := m.View()
	if !strings.Contains(v, "Diff") {
		t.Errorf("expected view to contain 'Diff', got:\n%s", v)
	}
	if !strings.Contains(v, "Before SSD") || !strings.Contains(v, "After NVMe") {
		t.Errorf("expected view to show tags, got:\n%s", v)
	}
	if !strings.Contains(v, "Sequential write") {
		t.Errorf("expected view to contain metric name, got:\n%s", v)
	}
	if !strings.Contains(v, "+300.0%") {
		t.Errorf("expected view to contain +300.0%% delta, got:\n%s", v)
	}
}

func TestViewHistoryWithTagsAndDiffBase(t *testing.T) {
	m := New()
	m.w, m.h = 100, 30
	m.scr = scrHistory

	s1 := history.Session{
		ID:    "s1",
		Host:  "server-a",
		Depth: "normal",
		Tag:   "baseline-v1",
		Time:  time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC),
		Records: []history.Record{
			{
				Session: "s1",
				Host:    "server-a",
				Depth:   "normal",
				Tag:     "baseline-v1",
				Kind:    "Disk",
				Time:    time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC),
				Metrics: []history.Metric{{Name: "Write", Display: "100 MB/s", Value: 100}},
			},
		},
	}
	s2 := history.Session{
		ID:    "s2",
		Host:  "server-a",
		Depth: "normal",
		Tag:   "release-v2",
		Time:  time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC),
		Records: []history.Record{
			{
				Session: "s2",
				Host:    "server-a",
				Depth:   "normal",
				Tag:     "release-v2",
				Kind:    "Disk",
				Time:    time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC),
				Metrics: []history.Metric{{Name: "Write", Display: "200 MB/s", Value: 200}},
			},
		},
	}

	m.hist = append(m.hist, s1.Records...)
	m.hist = append(m.hist, s2.Records...)
	m.diffBase = &s1

	v := m.View()
	if !strings.Contains(v, "baseline-v1") || !strings.Contains(v, "release-v2") {
		t.Errorf("expected view to contain session tags, got:\n%s", v)
	}
	if !strings.Contains(v, "[✓ Base]") {
		t.Errorf("expected view to contain [✓ Base] badge, got:\n%s", v)
	}
	if !strings.Contains(v, "compare with selected") {
		t.Errorf("expected footer to contain diffBase prompt, got:\n%s", v)
	}

	// Now test inline tag editing mode
	m.diffBase = nil
	m.editingTag = true
	m.hcur = 0
	m.tagInput.SetValue("my-new-tag")

	vEditing := m.View()
	if !strings.Contains(vEditing, "my-new-tag") {
		t.Errorf("expected view to show tag input value, got:\n%s", vEditing)
	}
	if !strings.Contains(vEditing, "save tag") || !strings.Contains(vEditing, "esc cancel") {
		t.Errorf("expected footer to show tag editing help, got:\n%s", vEditing)
	}
}

func TestHistoryTagEditingKeyFlow(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess := history.NewSession()
	r := bench.Result{Kind: bench.CPU, Tool: "test", Metrics: []bench.Metric{{Name: "X", Value: 1}}}
	_ = history.Save(sess, "host", "normal", "initial-tag", r, nil)

	m := New()
	m.hist, _ = history.Load()
	m.scr = scrHistory

	// Press 't' to start editing
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = nm.(Model)
	if !m.editingTag {
		t.Fatalf("expected editingTag to be true")
	}
	if m.tagSessionID != sess {
		t.Fatalf("expected tagSessionID %s, got %s", sess, m.tagSessionID)
	}
	if m.tagInput.Value() != "initial-tag" {
		t.Fatalf("expected tagInput value 'initial-tag', got %s", m.tagInput.Value())
	}

	// Change value and press enter to save
	m.tagInput.SetValue("saved-new-tag")
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if m.editingTag {
		t.Fatalf("expected editingTag to be false after enter")
	}
	recs, err := history.Load()
	if err != nil || len(recs) != 1 {
		t.Fatalf("failed to reload history: %v", err)
	}
	if recs[0].Tag != "saved-new-tag" {
		t.Fatalf("expected tag to be 'saved-new-tag', got %s", recs[0].Tag)
	}

	// Press 't', change value and press esc to cancel
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = nm.(Model)
	m.tagInput.SetValue("cancelled-tag")
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = nm.(Model)
	if m.editingTag {
		t.Fatalf("expected editingTag to be false after esc")
	}
	recs, _ = history.Load()
	if recs[0].Tag != "saved-new-tag" {
		t.Fatalf("expected tag to remain 'saved-new-tag', got %s", recs[0].Tag)
	}
}

func TestHistoryDiffSelectionKeyFlow(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess1 := history.NewSession()
	r1 := bench.Result{Kind: bench.CPU, Tool: "test", Metrics: []bench.Metric{{Name: "X", Value: 100}}}
	_ = history.Save(sess1, "host", "normal", "run-1", r1, nil)

	sess2 := history.NewSession()
	r2 := bench.Result{Kind: bench.CPU, Tool: "test", Metrics: []bench.Metric{{Name: "X", Value: 200}}}
	_ = history.Save(sess2, "host", "normal", "run-2", r2, nil)

	m := New()
	m.hist, _ = history.Load()
	m.scr = scrHistory

	// Press 'c' on first session (hcur=0) -> sets diffBase
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = nm.(Model)
	if m.diffBase == nil || m.diffBase.ID != sess2 { // note newest session is sess2 at index 0
		t.Fatalf("expected diffBase to be %s, got %v", sess2, m.diffBase)
	}

	// Press 'c' again on same session -> toggles off
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = nm.(Model)
	if m.diffBase != nil {
		t.Fatalf("expected diffBase to be toggled off (nil)")
	}

	// Set diffBase again using space
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = nm.(Model)
	if m.diffBase == nil {
		t.Fatalf("expected diffBase to be set")
	}

	// Move cursor down to second session (hcur=1)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nm.(Model)
	if m.hcur != 1 {
		t.Fatalf("expected hcur 1, got %d", m.hcur)
	}

	// Press 'c' -> diffs diffBase and target, navigates to scrHistDiff
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = nm.(Model)
	if m.scr != scrHistDiff {
		t.Fatalf("expected scrHistDiff, got %d", m.scr)
	}
	if m.diffTarget == nil || m.diffTarget.ID != sess1 {
		t.Fatalf("expected diffTarget to be %s", sess1)
	}

	// In scrHistDiff, press esc -> returns to scrHistory
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = nm.(Model)
	if m.scr != scrHistory {
		t.Fatalf("expected scrHistory after esc, got %d", m.scr)
	}

	// In scrHistory with diffBase set, press esc -> clears diffBase
	if m.diffBase == nil {
		t.Fatalf("expected diffBase still set before esc")
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = nm.(Model)
	if m.diffBase != nil {
		t.Fatalf("expected diffBase to be cleared after esc")
	}
}

func TestHistoryViewTagAndDiffKeyFlow(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess := history.NewSession()
	r := bench.Result{Kind: bench.CPU, Tool: "test", Metrics: []bench.Metric{{Name: "X", Value: 1}}}
	_ = history.Save(sess, "host", "normal", "orig-tag", r, nil)

	m := New()
	m.hist, _ = history.Load()
	sessions := history.Sessions(m.hist)
	m.hview = &sessions[0]
	m.scr = scrHistoryView

	// Press 't' in history view -> switches to history list in editing mode
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = nm.(Model)
	if m.scr != scrHistory || !m.editingTag {
		t.Fatalf("expected scrHistory with editingTag, got scr=%d editing=%v", m.scr, m.editingTag)
	}
	if m.tagInput.Value() != "orig-tag" {
		t.Fatalf("expected tagInput value 'orig-tag', got %s", m.tagInput.Value())
	}

	// Reset to history view, press 'c' -> sets diffBase and switches to scrHistory
	m.editingTag = false
	m.scr = scrHistoryView
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = nm.(Model)
	if m.scr != scrHistory || m.diffBase == nil || m.diffBase.ID != sess {
		t.Fatalf("expected scrHistory with diffBase set, got scr=%d diffBase=%v", m.scr, m.diffBase)
	}
}
