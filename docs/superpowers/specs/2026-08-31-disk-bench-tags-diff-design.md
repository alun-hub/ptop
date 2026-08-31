# Design Specification: Small File Benchmarks, SQLite ACID Test, Tagging, and Diff Mode

## Overview
This specification details four key enhancements for `ptop`:
1. **A3 (Small File & Metadata Benchmark):** Measures file creation, `stat()`, and deletion throughput.
2. **A4 (SQLite / ACID Database Transaction Benchmark):** Measures ACID-durable database transactions per second (using `sqlite3` CLI if present, or an internal pure-Go WAL synchronization engine).
3. **C2 (Run Tagging & Annotations):** Supports tagging runs (via `--tag` on `ptop run`, `ptop history tag`, and inline in the TUI).
4. **C1 (Interactive Run Diff Mode):** Side-by-side run comparison with per-metric percentage deltas in both CLI (`ptop history diff`) and TUI (`scrHistDiff`).

---

## 1. Data Model & History (`internal/history`)

### 1.1 Data Structures
Extend `Record` and `Session` with `Tag`:
```go
type Record struct {
    Time    time.Time `json:"time"`
    Session string    `json:"session"`
    Host    string    `json:"host"`
    Kind    string    `json:"kind"`
    Depth   string    `json:"depth"`
    Tool    string    `json:"tool,omitempty"`
    Tag     string    `json:"tag,omitempty"`
    Summary string    `json:"summary,omitempty"`
    Failed  bool      `json:"failed,omitempty"`
    Error   string    `json:"error,omitempty"`
    Metrics []Metric  `json:"metrics"`
}

type Session struct {
    ID      string
    Time    time.Time
    Host    string
    Depth   string
    Tag     string
    Records []Record
}
```

### 1.2 History Operations
- **`history.Save(session, host, depth, tag string, r bench.Result, runErr error) error`**: Persists `Tag`.
- **`history.SetTag(sessionID, tag string) error`**: Loads all records from `history.jsonl`, updates the `Tag` on matching records for `sessionID`, and writes back atomically via a temp file.
- **`history.DiffSessions(base, target Session) DiffResult`**:
  ```go
  type DiffItem struct {
      Kind        string
      Name        string
      BaseDisplay string
      BaseValue   float64
      TargDisplay string
      TargValue   float64
      Delta       Delta
      Verdict     string
  }

  type DiffResult struct {
      Base   Session
      Target Session
      Items  []DiffItem
  }
  ```
  Matches metrics across both sessions by canonical `Key(m.Name)`. Computes signed percentage delta considering `LowerBetter`.

---

## 2. Benchmark Engine (`internal/bench`)

### 2.1 Configuration
Extend `Config` with `Tag string`.

### 2.2 Small Files / Metadata Test (`A3`)
Executed as part of the Disk test suite in `internal/bench/disk.go` / `internal/bench/meta.go`:
- Creates a dedicated scratch directory `.ptop-meta-<pid>` inside `cfg.Path`.
- Scale by `Depth`:
  - `Quick`: 2,500 files
  - `Normal`: 10,000 files
  - `Deep`: 25,000 files
- **Phases:**
  1. **Create phase:** Creates files with 512-byte payload, measures `files/s`.
  2. **Stat/Read phase:** Calls `os.Stat` and reads 1 byte from each file, measures `ops/s`.
  3. **Delete phase:** Removes all files, measures `deletions/s`.
  4. Cleans up scratch directory.
- **Metrics produced:**
  - `Small file creation`: formatted as `X files/s` with gauge (scale 500/s to 50,000/s), verdict `● good / ok / low`.
  - `Small file metadata (stat)`: formatted as `X ops/s` with gauge (scale 2,000/s to 200,000/s).
  - `Small file deletion`: formatted as `X files/s` with gauge (scale 500/s to 50,000/s).

### 2.3 SQLite / Database ACID Transaction Benchmark (`A4`)
Executed as part of the Disk suite in `internal/bench/disk.go` / `internal/bench/sqlite.go`:
- **Execution Strategy:**
  - If `have("sqlite3")` is true:
    - Creates a scratch database `.ptop-sqlite-<pid>.db`.
    - Configures `PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;`.
    - Creates table `CREATE TABLE bench (id INTEGER PRIMARY KEY, k TEXT, v BLOB); CREATE INDEX idx_k ON bench(k);`.
    - Runs a timed loop of `BEGIN IMMEDIATE; INSERT ...; COMMIT;` for `Depth.Seconds() / 2` (minimum 3s, max 10s).
    - Cleans up `.ptop-sqlite-*` files.
  - **Native Pure-Go Fallback:**
    - Creates `.ptop-wal-<pid>.db` and `.ptop-wal-<pid>.wal`.
    - Simulates SQLite WAL commits by appending 4096-byte frames followed by `fdatasync()` per transaction commit.
    - Measures synchronous commit throughput (`txn/s`).
- **Metric produced:**
  - `Database transactions (SQLite ACID)`: formatted as `X txn/s`, verdict (`>= 2000 txn/s`: VGood, `>= 400 txn/s`: VOkay, else VPoor), gauge (scale 50 txn/s to 10,000 txn/s).
  - Explanatory note describing impact on SQL database write performance (PostgreSQL, MySQL, SQLite).

---

## 3. CLI Subcommands & Flags (`cli.go` & `main.go`)

### 3.1 New Flags on `ptop run`
- `--tag "<text>"`: Sets the annotation tag for this benchmark session.

### 3.2 New Subcommands on `ptop history`
- **`ptop history tag <#|session-id> "<tag-text>"`**: Updates or clears the tag for a past run.
- **`ptop history diff <#1|id1> <#2|id2>`**: Computes and prints a formatted diff table between two runs.
- **`ptop history` listing**: Updated to display a `tag` column when tags are present.

---

## 4. TUI Enhancements (`internal/ui`)

### 4.1 Tag Editing in TUI
- In `scrHistory` (session list) and `scrHistoryView` (single run view), pressing <kbd>t</kbd> activates an inline textinput model to edit the session's tag.
- Pressing <kbd>Enter</kbd> saves via `history.SetTag`. <kbd>Esc</kbd> cancels.

### 4.2 Diff Mode in TUI (`scrHistDiff`)
- In `scrHistory`:
  - Pressing <kbd>Space</kbd> or <kbd>c</kbd> toggles the selected run as the `diffBase` (marked with `[✓ Base]`).
  - Navigating to another run and pressing <kbd>c</kbd> or <kbd>Enter</kbd> opens `scrHistDiff`.
- **`scrHistDiff` UI:**
  - Header showing: `Diff: Run <Base-Time> ("<Base-Tag>")  vs  Run <Target-Time> ("<Target-Tag>")`
  - Grouped by Kind (Disk, CPU, Memory, Network, GPU).
  - Columns: `Metric`, `Base Value`, `Target Value`, `Delta %`, `Verdict`.
  - Scrollable with <kbd>↑</kbd>/<kbd>↓</kbd>, <kbd>PgUp</kbd>/<kbd>PgDn</kbd>.
  - <kbd>Esc</kbd> returns to `scrHistory`.

---

## 5. Testing & Verification
- Unit tests in `internal/bench` for small files and sqlite transaction runner.
- Unit tests in `internal/history` for `SetTag`, `DiffSessions`, and tag persistence.
- Unit tests in `cli_test.go` for `ptop history diff` and `ptop history tag`.
- TUI render tests in `internal/ui/render_test.go` verifying `scrHistDiff` and tag rendering.
