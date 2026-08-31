// Package history persists every ptop run to a local JSONL file so runs can be
// browsed later and compared against earlier ones.
package history

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"ptop/internal/bench"
)

// Record is one test's result from one run.
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

// Metric is the stored form of bench.Metric.
type Metric struct {
	Name        string  `json:"name"`
	Display     string  `json:"display"`
	Value       float64 `json:"value,omitempty"`
	Unit        string  `json:"unit,omitempty"`
	LowerBetter bool    `json:"lower_better,omitempty"`
	Verdict     string  `json:"verdict,omitempty"`
	Note        string  `json:"note,omitempty"`
}

// Path is the JSONL file. Override with PTOP_HISTORY.
func Path() string {
	if p := os.Getenv("PTOP_HISTORY"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "ptop", "history.jsonl")
}

// NewSession returns an id that groups the Records written by one invocation.
func NewSession() string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b[:])
}

// Save appends one test result to the history file.
func Save(session, host, depth, tag string, r bench.Result, runErr error) error {
	rec := Record{
		Time: time.Now().UTC(), Session: session, Host: host,
		Kind: r.Kind.String(), Depth: depth, Tool: r.Tool, Tag: tag, Summary: r.Summary,
	}
	if runErr != nil {
		rec.Failed = true
		rec.Error = runErr.Error()
	}
	for _, m := range r.Metrics {
		rec.Metrics = append(rec.Metrics, Metric{
			Name: m.Name, Display: m.Display, Value: m.Value, Unit: m.Unit,
			LowerBetter: m.LowerBetter, Verdict: m.Verdict.Label(), Note: m.Note,
		})
	}
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// Load reads every record, oldest first. Missing file is not an error.
func Load() ([]Record, error) {
	f, err := os.Open(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r Record
		if json.Unmarshal([]byte(line), &r) == nil {
			out = append(out, r)
		}
	}
	return out, sc.Err()
}

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

// SetTag updates or clears the tag on all records for the given session ID.
func SetTag(sessionID, tag string) error {
	recs, err := Load()
	if err != nil {
		return err
	}
	p := Path()
	var modified bool
	for i := range recs {
		if recs[i].Session == sessionID {
			if recs[i].Tag != tag {
				recs[i].Tag = tag
				modified = true
			}
		}
	}
	if !modified {
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

	for _, r := range recs {
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

// Session groups the records written together by one invocation.
type Session struct {
	ID      string
	Time    time.Time
	Host    string
	Depth   string
	Tag     string
	Records []Record
}

// Sessions groups records into sessions, newest first.
func Sessions(recs []Record) []Session {
	byID := map[string]*Session{}
	var order []string
	for _, r := range recs {
		s := byID[r.Session]
		if s == nil {
			s = &Session{ID: r.Session, Time: r.Time, Host: r.Host, Depth: r.Depth, Tag: r.Tag}
			byID[r.Session] = s
			order = append(order, r.Session)
		}
		if r.Time.Before(s.Time) {
			s.Time = r.Time
		}
		if r.Tag != "" {
			s.Tag = r.Tag
		}
		s.Records = append(s.Records, r)
	}
	out := make([]Session, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out
}

func (s Session) Kinds() []string {
	var ks []string
	for _, r := range s.Records {
		ks = append(ks, strings.ToLower(r.Kind))
	}
	return ks
}

var parenRe = regexp.MustCompile(`\s*\([^)]*\)\s*`)

// Key normalises a metric name for matching across runs (drops the variable
// parenthetical, e.g. "(to gateway 192.168.1.1)").
func Key(name string) string {
	return strings.TrimSpace(strings.ToLower(parenRe.ReplaceAllString(name, " ")))
}

// Baseline returns the most recent record for kind (and host, if non-empty)
// from a session other than exclude.
func Baseline(recs []Record, kind, host, exclude string) *Record {
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		if r.Failed || !strings.EqualFold(r.Kind, kind) || r.Session == exclude {
			continue
		}
		if host != "" && !strings.EqualFold(r.Host, host) {
			continue
		}
		rr := r
		return &rr
	}
	return nil
}

// Delta is a signed percentage change where positive always means "better".
type Delta struct {
	Valid    bool
	Pct      float64
	Baseline float64
	When     time.Time
}

// Compare looks up metric in base and returns how the current value compares.
func Compare(base *Record, name string, cur float64, lowerBetter bool) Delta {
	if base == nil || cur == 0 {
		return Delta{}
	}
	k := Key(name)
	for _, m := range base.Metrics {
		if Key(m.Name) != k || m.Value == 0 {
			continue
		}
		var pct float64
		if lowerBetter {
			pct = (m.Value - cur) / m.Value * 100
		} else {
			pct = (cur - m.Value) / m.Value * 100
		}
		return Delta{Valid: true, Pct: pct, Baseline: m.Value, When: base.Time}
	}
	return Delta{}
}

// Label renders a Delta, e.g. "+4% faster vs Aug 30" / "12% slower vs Aug 30".
func (d Delta) Label() string {
	if !d.Valid {
		return ""
	}
	when := d.When.Local().Format("Jan 2")
	switch {
	case d.Pct > 2:
		return fmt.Sprintf("+%s%% faster vs %s", pctStr(d.Pct), when)
	case d.Pct < -2:
		return fmt.Sprintf("%s%% slower vs %s", pctStr(d.Pct), when)
	default:
		return "~same vs " + when
	}
}

func pctStr(p float64) string {
	if p < 10 && p > -10 {
		return fmt.Sprintf("%.1f", p)
	}
	return fmt.Sprintf("%.0f", p)
}
