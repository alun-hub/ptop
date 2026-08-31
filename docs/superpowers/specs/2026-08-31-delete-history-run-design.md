# Design Spec: Delete History Run with 'r' Key

## Overview
Add the ability to delete individual historical benchmark runs using the `r` key in `ptop`. The feature allows users to remove old, inaccurate, or unwanted test runs from both the session list (`Recent runs`) and the detailed session view, with an inline confirmation prompt in the footer to prevent accidental data loss.

## Requirements & Scope
1. **Interactive TUI Support:**
   - In `scrHistory` (Session list / "Recent runs"): Pressing `r` prompts for deletion of the highlighted session.
   - In `scrHistoryView` (Detailed run view): Pressing `r` prompts for deletion of the currently viewed session.
   - Confirmation is displayed inline in the footer:
     `Delete run YYYY-MM-DD HH:MM:SS?  y confirm  ·  n/esc cancel`
   - Pressing `y` / `Y` confirms deletion: deletes all records for that session from storage, reloads history in memory, clears confirmation state, and updates the view/cursor (or navigates back to `scrHistory` if inside `scrHistoryView`).
   - Pressing `n`, `N`, `esc`, or any other key cancels deletion without modifying records.
   - Updated footer help text to indicate `r delete` availability in both screens.
2. **Storage Layer (`internal/history`):**
   - Add `DeleteSession(sessionID string) error` to remove all records matching the given session ID from `Path()`.
   - Ensure atomic write (write to temporary file and rename) to prevent file corruption.
3. **CLI Command (`cli.go`):**
   - Support `ptop history rm <#|session-id>` or `ptop history delete <#|session-id>` to allow deleting runs from the command line as well.
4. **Testing:**
   - Unit tests in `internal/history/history_test.go` covering `DeleteSession` (single session, multiple sessions, nonexistent session, atomic file replacement).
   - UI tests in `internal/ui/render_test.go` covering keypress flows (`r` -> `y` and `r` -> `n`).

## Architecture & Data Flow

### 1. Storage: `internal/history/history.go`
- `DeleteSession(sessionID string) error`:
  - Calls `Load()` to get all records.
  - Filters records: keeps those where `Record.Session != sessionID`.
  - If no records were removed, return `nil`.
  - Writes the filtered records to a temporary file in the same directory as `Path()`.
  - Renames the temporary file to `Path()`.

### 2. State & Handlers: `internal/ui/model.go`
- Model struct field:
  `confirmDel string // session ID pending deletion, or "" if none`
- `updateHistory`:
  - If `m.confirmDel != ""`:
    - `y`, `Y`:
      - `_ = history.DeleteSession(m.confirmDel)`
      - `m.hist, _ = history.Load()`
      - `m.confirmDel = ""`
      - Adjust `m.hcur` if out of bounds (`sessions := history.Sessions(m.hist); if m.hcur >= len(sessions) { m.hcur = len(sessions) - 1 }`).
    - `n`, `N`, `esc` (or any other key):
      - `m.confirmDel = ""`
  - If `m.confirmDel == ""`:
    - `r`:
      - `if m.hcur < len(sessions) { m.confirmDel = sessions[m.hcur].ID }`
    - Standard keys (`up`, `down`, `enter`, `esc`, `q`) behave normally.
- `updateHistoryView`:
  - If `m.confirmDel != ""`:
    - `y`, `Y`:
      - `_ = history.DeleteSession(m.confirmDel)`
      - `m.hist, _ = history.Load()`
      - `m.confirmDel = ""`
      - `m.hview = nil`
      - `m.scr = scrHistory`
    - `n`, `N`, `esc` (or any other key):
      - `m.confirmDel = ""`
  - If `m.confirmDel == ""`:
    - `r`:
      - `if m.hview != nil { m.confirmDel = m.hview.ID }`
    - Standard keys behave normally.

### 3. Rendering & Footer: `internal/ui/view.go`
- `footer()`:
  - Check if `m.confirmDel != ""`:
    - Find the session timestamp from `m.hist` matching `m.confirmDel`.
    - Format: `Delete run 2006-01-02 15:04:05?   y confirm   ·   n/esc cancel` in warning/accent style.
  - Otherwise, standard footers:
    - `scrHistory`: `↑/↓ select   ⏎ open   r delete   esc back   q quit`
    - `scrHistoryView`: `↑/↓ scroll   r delete   esc back to list   q quit`

### 4. CLI: `cli.go`
- In `runHistory(args []string)`:
  - If `args[0] == "rm"` or `args[0] == "delete"`:
    - Parse target session index or ID prefix from `args[1]`.
    - Call `history.DeleteSession(target.ID)`.
    - Print confirmation message: `Deleted run <ID> (<Time>)`.

## Verification & Testing Plan
1. `go test ./internal/history`: verify `DeleteSession` and round-trip behaviors.
2. `go test ./internal/ui`: verify screen rendering in both normal and deletion confirmation states, and keypress transitions.
3. `go test ./...`: ensure all tests across the package pass cleanly.
4. CLI verification: verify `ptop history rm` syntax and output.
