# Delete History Run Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to delete historical benchmark runs with the `r` key in both session list and run detail views with an inline footer confirmation prompt, and add CLI support via `ptop history rm`.

**Architecture:** Add `history.DeleteSession` for atomic JSONL file rewriting. Extend Bubble Tea `ui.Model` with a `confirmDel string` state to handle inline confirmation on `scrHistory` and `scrHistoryView`. Update footer rendering to display the prompt and hotkeys. Add `rm` argument handling in `cli.go`.

**Tech Stack:** Go, Charm Bubble Tea, Lipgloss.

---

### Task 1: Add `DeleteSession` to `internal/history`

**Files:**
- Modify: `internal/history/history.go`
- Test: `internal/history/history_test.go`

- [ ] **Step 1: Write failing unit test for `DeleteSession`**

In `internal/history/history_test.go`, add `TestDeleteSession`:

```go
func TestDeleteSession(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess1 := NewSession()
	sess2 := NewSession()
	r1 := bench.Result{Kind: bench.CPU, Tool: "test", Metrics: []bench.Metric{{Name: "M1", Value: 10}}}
	r2 := bench.Result{Kind: bench.Disk, Tool: "test", Metrics: []bench.Metric{{Name: "M2", Value: 20}}}

	if err := Save(sess1, "host1", "quick", r1, nil); err != nil {
		t.Fatal(err)
	}
	if err := Save(sess2, "host1", "quick", r2, nil); err != nil {
		t.Fatal(err)
	}

	recs, err := Load()
	if err != nil || len(recs) != 2 {
		t.Fatalf("expected 2 records before delete, got %d (err: %v)", len(recs), err)
	}

	if err := DeleteSession(sess1); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	recs, err = Load()
	if err != nil {
		t.Fatalf("Load after delete failed: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record after delete, got %d", len(recs))
	}
	if recs[0].Session != sess2 {
		t.Fatalf("expected sess2 to remain, got session %s", recs[0].Session)
	}

	// Deleting a nonexistent session should be a no-op and succeed
	if err := DeleteSession("nonexistent"); err != nil {
		t.Fatalf("DeleteSession nonexistent error: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/history -run TestDeleteSession`
Expected: FAIL with `undefined: DeleteSession`

- [ ] **Step 3: Implement `DeleteSession` in `internal/history/history.go`**

Add `DeleteSession` function to `internal/history/history.go`:

```go
// DeleteSession removes all records for the given session ID from the history file.
func DeleteSession(sessionID string) error {
	recs, err := Load()
	if err != nil {
		return err
	}
	p := Path()
	var kept []Record
	for _, r := range recs {
		if r.Session != sessionID {
			kept = append(kept, r)
		}
	}
	if len(kept) == len(recs) {
		return nil
	}

	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(dir, "history-*.jsonl.tmp")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
	}()

	for _, r := range kept {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err := tmpFile.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	if err := tmpFile.Sync(); err != nil {
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	return os.Rename(tmpName, p)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/history -run TestDeleteSession`
Expected: PASS

- [ ] **Step 5: Commit changes**

```bash
git add internal/history/history.go internal/history/history_test.go
git commit -m "feat(history): add DeleteSession to remove runs from history storage"
```

---

### Task 2: Implement Deletion Key Handling in `internal/ui`

**Files:**
- Modify: `internal/ui/model.go`
- Test: `internal/ui/render_test.go`

- [ ] **Step 1: Write failing UI tests for 'r' key and confirmation**

In `internal/ui/render_test.go`, add `TestHistoryDeleteKeyFlow`:

```go
func TestHistoryDeleteKeyFlow(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess := history.NewSession()
	r := bench.Result{Kind: bench.CPU, Tool: "test", Metrics: []bench.Metric{{Name: "X", Value: 1}}}
	_ = history.Save(sess, "host", "normal", r, nil)

	m := New()
	m.hist, _ = history.Load()
	m.scr = scrHistory

	// Pressing 'r' should set confirmDel
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
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

	// Pressing 'r' then 'y' deletes the session
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
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
	_ = history.Save(sess, "host", "normal", r, nil)

	m := New()
	m.hist, _ = history.Load()
	sessions := history.Sessions(m.hist)
	m.hview = &sessions[0]
	m.scr = scrHistoryView

	// Pressing 'r' sets confirmDel
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
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
		t.Fatalf("expected m.hist to be empty, got %d", len(m.hist))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/ui -run TestHistoryDeleteKeyFlow`
Expected: FAIL with `m.confirmDel undefined`

- [ ] **Step 3: Update `Model`, `updateHistory`, and `updateHistoryView` in `internal/ui/model.go`**

In `internal/ui/model.go`:
1. Add `confirmDel string` field to `Model` struct:
```go
	// history
	session    string
	hist       []history.Record
	hcur       int // cursor on the history-area chooser / session list
	hview      *history.Session
	harea      string // selected test area
	haCur      int    // selected metric row in the area overview
	hmName     string // selected metric for the chart screen
	hAllHosts  bool
	confirmDel string // session ID pending deletion, or ""
```

2. Update `updateHistory(msg tea.KeyMsg)`:
```go
func (m Model) updateHistory(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sessions := history.Sessions(m.hist)

	if m.confirmDel != "" {
		switch msg.String() {
		case "y", "Y":
			_ = history.DeleteSession(m.confirmDel)
			m.hist, _ = history.Load()
			m.confirmDel = ""
			newSessions := history.Sessions(m.hist)
			if m.hcur >= len(newSessions) {
				m.hcur = len(newSessions) - 1
			}
			if m.hcur < 0 {
				m.hcur = 0
			}
			return m, nil
		default:
			m.confirmDel = ""
			return m, nil
		}
	}

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
	case "r":
		if m.hcur < len(sessions) {
			m.confirmDel = sessions[m.hcur].ID
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
```

3. Update `updateHistoryView(msg tea.KeyMsg)`:
```go
func (m Model) updateHistoryView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirmDel != "" {
		switch msg.String() {
		case "y", "Y":
			_ = history.DeleteSession(m.confirmDel)
			m.hist, _ = history.Load()
			m.confirmDel = ""
			m.hview = nil
			m.scr = scrHistory
			newSessions := history.Sessions(m.hist)
			if m.hcur >= len(newSessions) {
				m.hcur = len(newSessions) - 1
			}
			if m.hcur < 0 {
				m.hcur = 0
			}
			return m, nil
		default:
			m.confirmDel = ""
			return m, nil
		}
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "left", "h":
		m.scr = scrHistory
	case "r":
		if m.hview != nil {
			m.confirmDel = m.hview.ID
		}
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./internal/ui -run "TestHistoryDeleteKeyFlow|TestHistoryViewDeleteKeyFlow"`
Expected: PASS

- [ ] **Step 5: Commit changes**

```bash
git add internal/ui/model.go internal/ui/render_test.go
git commit -m "feat(ui): handle r key and confirmation in history list and view"
```

---

### Task 3: Render Footer Prompt & Update Footer Help in `internal/ui`

**Files:**
- Modify: `internal/ui/view.go`
- Test: `internal/ui/render_test.go`

- [ ] **Step 1: Write test for footer rendering when `confirmDel` is set**

In `internal/ui/render_test.go`, add `TestHistoryFooterConfirmRender`:

```go
func TestHistoryFooterConfirmRender(t *testing.T) {
	m := New()
	m.w, m.h = 100, 40
	m.scr = scrHistory
	m.confirmDel = "some-session-id"

	out := m.View()
	if !strings.Contains(out, "Delete run") || !strings.Contains(out, "y confirm") {
		t.Fatalf("expected delete confirmation in footer, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/ui -run TestHistoryFooterConfirmRender`
Expected: FAIL

- [ ] **Step 3: Update `footer()` in `internal/ui/view.go`**

In `internal/ui/view.go`, update `footer()`:

```go
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
		keys = "↑/↓ select   ⏎ open   r delete   esc back   q quit"
	case scrHistoryView:
		keys = "↑/↓ scroll   r delete   esc back to list   q quit"
	case scrHistOverview:
		keys = "↑/↓ metric   ⏎ chart   h hosts   esc back   q quit"
	case scrHistMetric:
		keys = "↑/↓ scroll   h hosts   esc back   q quit"
	}
	return stySub.Render(strings.Repeat("─", m.width())) + "\n" + styHelp.Render(keys)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/ui -run TestHistoryFooterConfirmRender`
Expected: PASS

- [ ] **Step 5: Commit changes**

```bash
git add internal/ui/view.go internal/ui/render_test.go
git commit -m "feat(ui): update footer with delete confirmation prompt and r delete help"
```

---

### Task 4: CLI Support for `ptop history rm` in `cli.go`

**Files:**
- Modify: `cli.go`
- Test: `cli_test.go` (or `internal/history/history_test.go`)

- [ ] **Step 1: Write test for CLI history rm**

Create or update `cli_test.go`:

```go
package main

import (
	"path/filepath"
	"testing"

	"ptop/internal/bench"
	"ptop/internal/history"
)

func TestCLIHistoryRM(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess := history.NewSession()
	r := bench.Result{Kind: bench.CPU, Tool: "test", Metrics: []bench.Metric{{Name: "X", Value: 1}}}
	_ = history.Save(sess, "host", "normal", r, nil)

	rc := runHistory([]string{"rm", "1"})
	if rc != 0 {
		t.Fatalf("expected exit code 0, got %d", rc)
	}

	recs, _ := history.Load()
	if len(recs) != 0 {
		t.Fatalf("expected 0 records after CLI rm, got %d", len(recs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v . -run TestCLIHistoryRM`
Expected: FAIL

- [ ] **Step 3: Add `rm` and `delete` handling to `runHistory` in `cli.go`**

In `cli.go`, in `runHistory`:
```go
	if args[0] == "rm" || args[0] == "delete" {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: ptop history rm <#|session-id>")
			return 2
		}
		var sel *history.Session
		if n, e := strconv.Atoi(args[1]); e == nil && n >= 1 && n <= len(sessions) {
			sel = &sessions[n-1]
		} else {
			for i := range sessions {
				if strings.HasPrefix(sessions[i].ID, args[1]) {
					sel = &sessions[i]
					break
				}
			}
		}
		if sel == nil {
			fmt.Fprintf(os.Stderr, "no such run: %s\n", args[1])
			return 2
		}
		if err := history.DeleteSession(sel.ID); err != nil {
			fmt.Fprintf(os.Stderr, "could not delete run: %v\n", err)
			return 1
		}
		fmt.Printf("Deleted run %s (%s, %s)\n", sel.ID, sel.Time.Local().Format("2006-01-02 15:04:05"), sel.Host)
		return 0
	}
```
And add `ptop history rm <#>             delete a run from history` to the help text in `runHistory` when no args are provided.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v . -run TestCLIHistoryRM`
Expected: PASS

- [ ] **Step 5: Commit changes**

```bash
git add cli.go cli_test.go
git commit -m "feat(cli): add ptop history rm command to delete runs"
```

---

### Task 5: Full Project Verification & Regression Testing

**Files:**
- Test: All packages (`./...`)

- [ ] **Step 1: Run all tests in the repository**

Run: `go test -v ./...`
Expected: All tests PASS.

- [ ] **Step 2: Build the binary**

Run: `go build -o bin/ptop .`
Expected: Builds without errors or warnings.

- [ ] **Step 3: Final verification commit if needed**

```bash
git status
```
