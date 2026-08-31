package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ptop/internal/bench"
	"ptop/internal/history"
)

func TestCLIHistoryRM(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess := history.NewSession()
	r := bench.Result{Kind: bench.CPU, Tool: "test", Metrics: []bench.Metric{{Name: "X", Value: 1}}}
	_ = history.Save(sess, "host", "normal", "", r, nil)

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
	_ = history.Save(sess, "host", "normal", "", r, nil)

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
	_ = history.Save(sess, "host", "normal", "", r, nil)

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

func captureStdout(f func()) string {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		_ = r.Close()
		outC <- buf.String()
	}()
	f()
	_ = w.Close()
	os.Stdout = old
	return <-outC
}

func TestCLIRunWithTag(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	rc := runCLI([]string{"cpu", "--depth", "quick", "--tag", "my-test-tag"})
	if rc != 0 {
		t.Fatalf("expected exit code 0, got %d", rc)
	}

	recs, err := history.Load()
	if err != nil || len(recs) == 0 {
		t.Fatalf("expected history records saved, got err=%v len=%d", err, len(recs))
	}
	if recs[0].Tag != "my-test-tag" {
		t.Errorf("expected tag 'my-test-tag', got %q", recs[0].Tag)
	}
}

func TestCLIHistoryTag(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess := history.NewSession()
	r := bench.Result{Kind: bench.CPU, Tool: "test", Metrics: []bench.Metric{{Name: "X", Value: 1}}}
	_ = history.Save(sess, "host", "normal", "initial", r, nil)

	// Set tag by index
	var out string
	out = captureStdout(func() {
		rc := runHistory([]string{"tag", "1", "updated-tag"})
		if rc != 0 {
			t.Fatalf("expected exit code 0 for tag, got %d", rc)
		}
	})
	if !strings.Contains(out, "Updated tag for run") || !strings.Contains(out, "updated-tag") {
		t.Errorf("unexpected output: %s", out)
	}

	recs, _ := history.Load()
	if len(recs) != 1 || recs[0].Tag != "updated-tag" {
		t.Errorf("expected tag 'updated-tag', got %q", recs[0].Tag)
	}

	// Update tag by session ID prefix
	prefix := sess[:8]
	out = captureStdout(func() {
		rc := runHistory([]string{"tag", prefix, "new-prefix-tag"})
		if rc != 0 {
			t.Fatalf("expected exit code 0 for tag by prefix, got %d", rc)
		}
	})
	if !strings.Contains(out, "Updated tag for run") {
		t.Errorf("unexpected output: %s", out)
	}

	recs, _ = history.Load()
	if len(recs) != 1 || recs[0].Tag != "new-prefix-tag" {
		t.Errorf("expected tag 'new-prefix-tag', got %q", recs[0].Tag)
	}

	// Clear tag
	out = captureStdout(func() {
		rc := runHistory([]string{"tag", "1"})
		if rc != 0 {
			t.Fatalf("expected exit code 0 for clearing tag, got %d", rc)
		}
	})
	if !strings.Contains(out, `""`) {
		t.Errorf("expected output to show cleared tag, got: %s", out)
	}

	recs, _ = history.Load()
	if len(recs) != 1 || recs[0].Tag != "" {
		t.Errorf("expected empty tag after clearing, got %q", recs[0].Tag)
	}

	// Errors
	if rc := runHistory([]string{"tag"}); rc != 2 {
		t.Fatalf("expected exit code 2 on missing tag args, got %d", rc)
	}
	if rc := runHistory([]string{"tag", "99", "fail"}); rc != 2 {
		t.Fatalf("expected exit code 2 on nonexistent run, got %d", rc)
	}
}

func TestCLIHistoryDiff(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess1 := history.NewSession()
	res1 := bench.Result{
		Kind: bench.Disk,
		Metrics: []bench.Metric{
			{Name: "Sequential write", Display: "400 MB/s", Value: 400, Unit: "MB/s", Verdict: bench.VOkay},
		},
	}
	_ = history.Save(sess1, "host1", "normal", "base-tag", res1, nil)

	sess2 := history.NewSession()
	res2 := bench.Result{
		Kind: bench.Disk,
		Metrics: []bench.Metric{
			{Name: "Sequential write", Display: "800 MB/s", Value: 800, Unit: "MB/s", Verdict: bench.VGood},
		},
	}
	_ = history.Save(sess2, "host1", "normal", "targ-tag", res2, nil)

	var out string
	out = captureStdout(func() {
		rc := runHistory([]string{"diff", "2", "1"})
		if rc != 0 {
			t.Fatalf("expected exit code 0 for diff, got %d", rc)
		}
	})

	if !strings.Contains(out, "Diff: Run #2") || !strings.Contains(out, "vs Run #1") {
		t.Errorf("expected header with Run #2 vs Run #1, got:\n%s", out)
	}
	if !strings.Contains(out, "Area") || !strings.Contains(out, "Metric") || !strings.Contains(out, "Change") {
		t.Errorf("expected table header columns, got:\n%s", out)
	}
	if !strings.Contains(out, "Sequential write") || !strings.Contains(out, "400 MB/s") || !strings.Contains(out, "800 MB/s") {
		t.Errorf("expected metric row with values, got:\n%s", out)
	}
	if !strings.Contains(out, "+100.0%") || !strings.Contains(out, "[good]") {
		t.Errorf("expected +100.0%% and [good] in change column, got:\n%s", out)
	}

	// Reverse diff (newer vs older)
	out = captureStdout(func() {
		rc := runHistory([]string{"diff", "1", "2"})
		if rc != 0 {
			t.Fatalf("expected exit code 0 for reverse diff, got %d", rc)
		}
	})
	if !strings.Contains(out, "-50.0%") {
		t.Errorf("expected -50.0%% in reverse diff, got:\n%s", out)
	}

	// Diff by ID prefix
	out = captureStdout(func() {
		rc := runHistory([]string{"diff", sess1[:18], sess2[:18]})
		if rc != 0 {
			t.Fatalf("expected exit code 0 for diff by prefix, got %d", rc)
		}
	})
	if !strings.Contains(out, "Sequential write") || !strings.Contains(out, "+100.0%") {
		t.Errorf("expected diff by prefix to work, got:\n%s", out)
	}

	// Errors
	if rc := runHistory([]string{"diff"}); rc != 2 {
		t.Fatalf("expected exit code 2 on missing diff args, got %d", rc)
	}
	if rc := runHistory([]string{"diff", "1"}); rc != 2 {
		t.Fatalf("expected exit code 2 on missing second diff arg, got %d", rc)
	}
	if rc := runHistory([]string{"diff", "1", "99"}); rc != 2 {
		t.Fatalf("expected exit code 2 on nonexistent second run, got %d", rc)
	}
}

func TestCLIHistoryListViewWithAndWithoutTags(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	t.Setenv("PTOP_HISTORY", p)

	sess1 := history.NewSession()
	r1 := bench.Result{Kind: bench.CPU, Tool: "test", Metrics: []bench.Metric{{Name: "X", Value: 1}}}
	_ = history.Save(sess1, "host1", "normal", "", r1, nil)

	// Without tags
	out := captureStdout(func() {
		rc := runHistory(nil)
		if rc != 0 {
			t.Fatalf("expected exit code 0, got %d", rc)
		}
	})
	if strings.Contains(out, "tag") {
		// column header should not have "tag" column
		lines := strings.Split(out, "\n")
		if len(lines) > 0 && strings.Contains(lines[0], "tag") {
			t.Errorf("expected no tag column header when no tags exist, got: %s", lines[0])
		}
	}
	if !strings.Contains(out, "ptop history diff") || !strings.Contains(out, "ptop history tag") {
		t.Errorf("expected footer to mention diff and tag commands, got:\n%s", out)
	}

	// With tags
	_ = history.SetTag(sess1, "tagged-run")
	out = captureStdout(func() {
		rc := runHistory(nil)
		if rc != 0 {
			t.Fatalf("expected exit code 0, got %d", rc)
		}
	})
	lines := strings.Split(out, "\n")
	if len(lines) == 0 || !strings.Contains(lines[0], "tag") {
		t.Errorf("expected tag column header when tags exist, got:\n%s", out)
	}
	if !strings.Contains(out, "tagged-run") {
		t.Errorf("expected tag content 'tagged-run' in output, got:\n%s", out)
	}
}
