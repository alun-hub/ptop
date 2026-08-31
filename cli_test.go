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

func TestCLIHistoryDeleteByPrefix(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess := history.NewSession()
	r := bench.Result{Kind: bench.CPU, Tool: "test", Metrics: []bench.Metric{{Name: "X", Value: 1}}}
	_ = history.Save(sess, "host", "normal", r, nil)

	prefix := sess[:8]
	rc := runHistory([]string{"delete", prefix})
	if rc != 0 {
		t.Fatalf("expected exit code 0, got %d", rc)
	}

	recs, _ := history.Load()
	if len(recs) != 0 {
		t.Fatalf("expected 0 records after CLI delete, got %d", len(recs))
	}
}

func TestCLIHistoryRMErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess := history.NewSession()
	r := bench.Result{Kind: bench.CPU, Tool: "test", Metrics: []bench.Metric{{Name: "X", Value: 1}}}
	_ = history.Save(sess, "host", "normal", r, nil)

	// Missing arg
	if rc := runHistory([]string{"rm"}); rc != 2 {
		t.Fatalf("expected exit code 2 on missing arg, got %d", rc)
	}

	// Nonexistent run
	if rc := runHistory([]string{"rm", "99"}); rc != 2 {
		t.Fatalf("expected exit code 2 on nonexistent run, got %d", rc)
	}

	// Empty prefix
	if rc := runHistory([]string{"rm", ""}); rc != 2 {
		t.Fatalf("expected exit code 2 on empty arg, got %d", rc)
	}

	recs, _ := history.Load()
	if len(recs) != 1 {
		t.Fatalf("expected 1 record preserved after invalid rm, got %d", len(recs))
	}
}
