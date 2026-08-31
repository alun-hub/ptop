# Implementation Plan: Small File Benchmarks, SQLite ACID Test, Run Tagging, and Diff Mode

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add small file metadata testing (A3), SQLite ACID transaction testing (A4), run tagging (C2), and interactive diff mode (C1) to `ptop`.

**Architecture:**
- Extend `internal/history` with `Tag` persistence, `SetTag` mutation, and `DiffSessions` comparison logic.
- Add `internal/bench/meta.go` (file creation/stat/deletion benchmarks) and `internal/bench/sqlite.go` (SQLite WAL transaction benchmark with pure-Go fallback), integrating both into the disk suite in `internal/bench/disk.go`.
- Update `cli.go` and `main.go` with `--tag`, `ptop history tag`, and `ptop history diff`.
- Update `internal/ui` to support inline tag editing, selecting baseline runs, and rendering the new `scrHistDiff` side-by-side comparison screen.

**Tech Stack:** Go 1.23, Bubble Tea, Lipgloss, JSONL, Linux standard syscalls (`fdatasync`, `O_DIRECT`).

---

## File Structure

```
ptop/
├── internal/
│   ├── bench/
│   │   ├── bench.go          - Config.Tag field
│   │   ├── disk.go           - Integrates metadata & sqlite into disk suite
│   │   ├── meta.go           - [NEW] Small files metadata benchmark (create, stat, delete)
│   │   ├── meta_test.go      - [NEW] Tests for metadata benchmark
│   │   ├── sqlite.go         - [NEW] SQLite ACID transaction benchmark + pure-Go WAL fallback
│   │   └── sqlite_test.go    - [NEW] Tests for sqlite/WAL transaction benchmark
│   ├── history/
│   │   ├── history.go        - Tag field in Record/Session, SetTag, Save with tag
│   │   ├── history_test.go   - Unit tests for Tag, SetTag, DiffSessions
│   │   └── diff.go           - [NEW] DiffSessions, DiffResult, DiffItem computation
│   └── ui/
│       ├── model.go          - scrHistDiff screen, tag editing state, diffBase selection
│       ├── view.go           - viewHistDiff, tag display in history lists, inline tag input
│       └── render_test.go    - Render tests for diff view and tags
├── cli.go                    - --tag flag, ptop history tag, ptop history diff
├── cli_test.go               - Tests for CLI diff and tag subcommands
└── main.go                   - Updated usage text
```

---

### Task 1: Data Model - Tagging & Diff Logic in `internal/history`

**Files:**
- Modify: `internal/history/history.go`
- Create: `internal/history/diff.go`
- Test: `internal/history/history_test.go`

- [ ] **Step 1: Write the failing tests for Tag, SetTag, and DiffSessions in `history_test.go`**

```go
func TestRecordTagAndSetTag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PTOP_HISTORY", filepath.Join(dir, "history.jsonl"))

	res := bench.Result{
		Kind: bench.Disk,
		Metrics: []bench.Metric{
			{Name: "Sequential write", Display: "500 MB/s", Value: 500, Unit: "MB/s"},
		},
	}
	sessID := "20260831T200000-abcdef"
	if err := Save(sessID, "testhost", "normal", "initial-tag", res, nil); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	recs, err := Load()
	if err != nil || len(recs) != 1 {
		t.Fatalf("Load failed: %v, len=%d", err, len(recs))
	}
	if recs[0].Tag != "initial-tag" {
		t.Errorf("expected tag 'initial-tag', got %q", recs[0].Tag)
	}

	sessions := Sessions(recs)
	if len(sessions) != 1 || sessions[0].Tag != "initial-tag" {
		t.Errorf("expected session tag 'initial-tag', got %q", sessions[0].Tag)
	}

	if err := SetTag(sessID, "updated-tag"); err != nil {
		t.Fatalf("SetTag failed: %v", err)
	}

	recs2, err := Load()
	if err != nil || len(recs2) != 1 {
		t.Fatalf("Load after SetTag failed: %v", err)
	}
	if recs2[0].Tag != "updated-tag" {
		t.Errorf("expected updated tag 'updated-tag', got %q", recs2[0].Tag)
	}
}

func TestDiffSessions(t *testing.T) {
	s1 := Session{
		ID:   "s1",
		Host: "testhost",
		Time: time.Now().Add(-1 * time.Hour),
		Tag:  "base",
		Records: []Record{
			{
				Kind: "Disk",
				Metrics: []Metric{
					{Name: "Sequential write", Display: "500 MB/s", Value: 500, Unit: "MB/s", LowerBetter: false},
					{Name: "Commit latency", Display: "4.0 ms", Value: 4.0, Unit: "ms", LowerBetter: true},
				},
			},
		},
	}
	s2 := Session{
		ID:   "s2",
		Host: "testhost",
		Time: time.Now(),
		Tag:  "upgrade",
		Records: []Record{
			{
				Kind: "Disk",
				Metrics: []Metric{
					{Name: "Sequential write", Display: "1000 MB/s", Value: 1000, Unit: "MB/s", LowerBetter: false},
					{Name: "Commit latency", Display: "2.0 ms", Value: 2.0, Unit: "ms", LowerBetter: true},
				},
			},
		},
	}

	diff := DiffSessions(s1, s2)
	if len(diff.Items) != 2 {
		t.Fatalf("expected 2 diff items, got %d", len(diff.Items))
	}
	// Sequential write: 500 -> 1000 is +100%
	if diff.Items[0].Delta.Pct != 100 {
		t.Errorf("expected +100%% for seq write, got %f", diff.Items[0].Delta.Pct)
	}
	// Commit latency: 4.0 -> 2.0 (lower is better) is +50% better
	if diff.Items[1].Delta.Pct != 50 {
		t.Errorf("expected +50%% for commit latency, got %f", diff.Items[1].Delta.Pct)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/history -run TestRecordTagAndSetTag`
Expected: Compile error due to missing `Tag` field and `SetTag` function.

- [ ] **Step 3: Implement `Tag`, `SetTag`, and `DiffSessions`**

In `internal/history/history.go`:
- Add `Tag string json:"tag,omitempty"` to `Record`.
- Add `Tag string` to `Session`.
- Update `Save(session, host, depth, tag string, r bench.Result, runErr error) error`.
- Populate `s.Tag = r.Tag` in `Sessions(recs)`.
- Add `SetTag(sessionID, tag string) error` using atomic file rewrite with temp file.

In `internal/history/diff.go`:
- Implement `DiffItem`, `DiffResult`, and `DiffSessions(base, target Session) DiffResult`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/history -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/history/
git commit -m "feat(history): add tag support, SetTag, and DiffSessions"
```

---

### Task 2: Small Files & Metadata Benchmark (`A3`)

**Files:**
- Create: `internal/bench/meta.go`
- Create: `internal/bench/meta_test.go`
- Modify: `internal/bench/disk.go`

- [ ] **Step 1: Write the failing test in `internal/bench/meta_test.go`**

```go
package bench

import (
	"context"
	"os"
	"testing"
)

func TestDiskMetadata(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Depth: Quick, Path: dir}
	events := make(chan Event, 64)

	done := make(chan struct{})
	go func() {
		for range events {
		}
		close(done)
	}()

	metrics, err := diskMetadata(context.Background(), cfg, dir, events)
	<-done
	if err != nil {
		t.Fatalf("diskMetadata failed: %v", err)
	}
	if len(metrics) != 3 {
		t.Fatalf("expected 3 metadata metrics, got %d", len(metrics))
	}
	for _, m := range metrics {
		if m.Value <= 0 {
			t.Errorf("metric %s has non-positive value: %f", m.Name, m.Value)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bench -run TestDiskMetadata`
Expected: FAIL (`diskMetadata` undefined)

- [ ] **Step 3: Implement `diskMetadata` in `internal/bench/meta.go`**

- `diskMetadata(ctx context.Context, cfg Config, dir string, out chan<- Event) ([]Metric, error)`:
  - Creates `.ptop-meta-<pid>` in `dir`.
  - Determines file count based on `cfg.Depth`:
    - `Quick`: 2,500
    - `Normal`: 10,000
    - `Deep`: 25,000
  - Phase 1 (Create): Creates empty or 512B files in loop, emits progress, measures `files/s`.
  - Phase 2 (Stat): Runs `os.Stat` and read first byte on all files, measures `ops/s`.
  - Phase 3 (Delete): Runs `os.Remove` on all files, measures `deletions/s`.
  - Cleans up directory with `defer os.RemoveAll(...)`.
  - Builds `Metric` structs:
    - `Small file creation` (`files/s`, gauge 500..50000)
    - `Small file metadata (stat)` (`ops/s`, gauge 2000..200000)
    - `Small file deletion` (`files/s`, gauge 500..50000)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/bench -run TestDiskMetadata -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/bench/meta.go internal/bench/meta_test.go
git commit -m "feat(bench): add small files and metadata disk benchmark"
```

---

### Task 3: SQLite ACID Transaction Benchmark (`A4`)

**Files:**
- Create: `internal/bench/sqlite.go`
- Create: `internal/bench/sqlite_test.go`
- Modify: `internal/bench/disk.go`

- [ ] **Step 1: Write the failing test in `internal/bench/sqlite_test.go`**

```go
package bench

import (
	"context"
	"testing"
)

func TestDiskSQLite(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Depth: Quick, Path: dir}
	events := make(chan Event, 64)

	done := make(chan struct{})
	go func() {
		for range events {
		}
		close(done)
	}()

	metrics, err := diskSQLite(context.Background(), cfg, dir, events)
	<-done
	if err != nil {
		t.Fatalf("diskSQLite failed: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 sqlite metric, got %d", len(metrics))
	}
	m := metrics[0]
	if m.Name != "Database transactions (SQLite ACID)" {
		t.Errorf("unexpected metric name: %s", m.Name)
	}
	if m.Value <= 0 {
		t.Errorf("expected positive value, got %f", m.Value)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bench -run TestDiskSQLite`
Expected: FAIL (`diskSQLite` undefined)

- [ ] **Step 3: Implement `diskSQLite` in `internal/bench/sqlite.go`**

- If `have("sqlite3")`:
  - Creates `.ptop-sqlite-<pid>.db`.
  - Runs sqlite3 with `PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;`.
  - Creates test table with index, executes indexed inserts inside transactions for `secs / 2` (min 3s, max 10s).
- Fallback (pure-Go WAL transaction tester):
  - Creates `.ptop-wal-<pid>.db` and `.ptop-wal-<pid>.wal`.
  - Implements timed loop: appends 4 KiB frame to WAL file followed by `file.Sync()` (`fdatasync`).
  - Measures `txns/s`.
- Metrics produced:
  - `Database transactions (SQLite ACID)` (`txn/s`, gauge 50..10000, Note: explains database write capacity).
- Integrate `diskMetadata` and `diskSQLite` in `diskFio` and `diskDD` in `internal/bench/disk.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/bench -run TestDiskSQLite -v`
Expected: PASS

- [ ] **Step 5: Run full `internal/bench` tests**

Run: `go test ./internal/bench -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/bench/
git commit -m "feat(bench): add SQLite ACID transaction benchmark and integrate with disk suite"
```

---

### Task 4: CLI Flags & Subcommands (`--tag`, `history tag`, `history diff`)

**Files:**
- Modify: `cli.go`
- Modify: `main.go`
- Test: `cli_test.go`

- [ ] **Step 1: Write failing CLI unit tests in `cli_test.go`**

```go
func TestCLIHistoryTagAndDiff(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PTOP_HISTORY", filepath.Join(dir, "history.jsonl"))

	// Create two fake history records
	res1 := bench.Result{
		Kind: bench.Disk,
		Metrics: []bench.Metric{
			{Name: "Sequential write", Display: "400 MB/s", Value: 400, Unit: "MB/s"},
		},
	}
	res2 := bench.Result{
		Kind: bench.Disk,
		Metrics: []bench.Metric{
			{Name: "Sequential write", Display: "800 MB/s", Value: 800, Unit: "MB/s"},
		},
	}
	_ = history.Save("sess1", "host1", "normal", "initial", res1, nil)
	_ = history.Save("sess2", "host1", "normal", "second", res2, nil)

	// Test tag command
	rc := runHistory([]string{"tag", "1", "updated-tag"})
	if rc != 0 {
		t.Fatalf("expected rc 0 for history tag, got %d", rc)
	}

	// Test diff command
	rcDiff := runHistory([]string{"diff", "1", "2"})
	if rcDiff != 0 {
		t.Fatalf("expected rc 0 for history diff, got %d", rcDiff)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestCLIHistoryTagAndDiff`
Expected: FAIL

- [ ] **Step 3: Update `cli.go` and `main.go`**

- In `cli.go`:
  - Parse `--tag` flag in `runCLI`. Pass `tag` to `runOne` and `history.Save`.
  - In `runHistory`:
    - Add `tag` subcommand: `ptop history tag <#|session-id> "<new-tag>"`.
    - Add `diff` subcommand: `ptop history diff <#1|id1> <#2|id2>` which uses `history.DiffSessions` and prints a structured diff table with columns `Area`, `Metric`, `Run A`, `Run B`, `Change`.
    - In history session listing, add `tag` column if any session has a tag.
- In `main.go`:
  - Update usage string with `--tag`, `ptop history tag`, and `ptop history diff`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestCLIHistoryTagAndDiff -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cli.go cli_test.go main.go
git commit -m "feat(cli): add --tag flag, history tag, and history diff subcommands"
```

---

### Task 5: TUI Enhancements (Tag Editing, Baseline Selection, Diff Screen `scrHistDiff`)

**Files:**
- Modify: `internal/ui/model.go`
- Modify: `internal/ui/view.go`
- Modify: `internal/ui/render_test.go`

- [ ] **Step 1: Write the failing TUI render tests in `render_test.go`**

```go
func TestViewHistDiff(t *testing.T) {
	m := New()
	m.w, m.h = 100, 30
	s1 := history.Session{
		ID:   "s1",
		Host: "server-a",
		Time: time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC),
		Tag:  "Before SSD",
		Records: []history.Record{
			{
				Kind: "Disk",
				Metrics: []history.Metric{
					{Name: "Sequential write", Display: "450 MB/s", Value: 450, Unit: "MB/s"},
				},
			},
		},
	}
	s2 := history.Session{
		ID:   "s2",
		Host: "server-a",
		Time: time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC),
		Tag:  "After NVMe",
		Records: []history.Record{
			{
				Kind: "Disk",
				Metrics: []history.Metric{
					{Name: "Sequential write", Display: "1800 MB/s", Value: 1800, Unit: "MB/s"},
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
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui -run TestViewHistDiff`
Expected: FAIL (`scrHistDiff`, `diffBase` undefined)

- [ ] **Step 3: Implement TUI Tag Editing and Diff Screen**

In `internal/ui/model.go`:
- Add `scrHistDiff screen = iota`.
- Add `diffBase *history.Session`, `diffTarget *history.Session`.
- Add `tagInput textinput.Model`, `editingTag bool`, `tagSessionID string`.
- Update `updateHistory`:
  - Handle key <kbd>t</kbd> to start inline tag editing.
  - Handle key <kbd>Space</kbd> or <kbd>c</kbd> to set/toggle `diffBase`.
  - When `diffBase != nil`, pressing <kbd>c</kbd> or <kbd>Enter</kbd> on a different session sets `diffTarget` and transitions to `scrHistDiff`.
- Add `updateHistDiff`:
  - Handle <kbd>Esc</kbd> (returns to `scrHistory`).
  - Handle scrolling with <kbd>Up</kbd>, <kbd>Down</kbd>, <kbd>PgUp</kbd>, <kbd>PgDn</kbd>.

In `internal/ui/view.go`:
- In `viewHistory`:
  - Display tags next to timestamp.
  - If a session is selected as `diffBase`, render `[✓ Base]` badge.
  - If `editingTag` is true, render the inline `tagInput`.
- Add `viewHistDiff`:
  - Renders header with Base vs Target (date, host, tag).
  - Renders comparison table grouped by area.
  - Renders footer with help keys.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/
git commit -m "feat(ui): add inline tag editing, diff base selection, and scrHistDiff screen"
```

---

### Task 6: End-to-End Verification, Documentation & Build

**Files:**
- Modify: `README.md`
- Modify: `man/ptop.1`

- [ ] **Step 1: Update README.md and man page**

- Update `README.md` to document:
  - New disk metrics (small files metadata and SQLite ACID).
  - `--tag` flag and `ptop history tag`.
  - `ptop history diff` and TUI diff shortcuts (<kbd>Space</kbd>/<kbd>c</kbd> to mark base, <kbd>c</kbd>/<kbd>Enter</kbd> to diff, <kbd>t</kbd> to tag).
- Update `man/ptop.1` with the new flags and commands.

- [ ] **Step 2: Run all tests across the repository**

Run: `go test ./... -v`
Expected: All packages PASS.

- [ ] **Step 3: Build standalone binary and test manual execution**

Run:
```bash
go build -o bin/ptop .
./bin/ptop run disk --depth quick --tag "Test run"
./bin/ptop history
./bin/ptop history diff 1 1
```
Expected: Clean output and successful execution.

- [ ] **Step 4: Commit**

```bash
git add README.md man/ptop.1
git commit -m "docs: update README and man page with metadata, sqlite, tag, and diff features"
```
