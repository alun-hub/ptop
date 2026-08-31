package bench

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// have reports whether an executable is on PATH.
func have(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// HasTool is the exported form of have, for the UI's preflight screen.
func HasTool(name string) bool { return have(name) }

// streamCmd runs cmd, forwarding every stdout+stderr line to out as a LogLine,
// and returns the full combined output once the command exits.
func streamCmd(ctx context.Context, out chan<- Event, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	var b strings.Builder
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			b.WriteString(line)
			b.WriteByte('\n')
			out <- LogLine{Text: line}
		}
		close(done)
	}()

	err := cmd.Start()
	if err != nil {
		pw.Close()
		return "", err
	}
	waitErr := cmd.Wait()
	pw.Close()
	<-done
	return b.String(), waitErr
}

// timeProgress emits Progress events on a ticker, easing toward 0.95 over the
// expected duration so the bar keeps moving even when the tool is silent.
// Call the returned stop func when the real work finishes.
func timeProgress(out chan<- Event, expected time.Duration, label string) (stop func()) {
	stopCh := make(chan struct{})
	go func() {
		start := time.Now()
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-t.C:
				frac := float64(time.Since(start)) / float64(expected)
				if frac > 0.95 {
					frac = 0.95
				}
				out <- Progress{Frac: frac, Label: label}
			}
		}
	}()
	return func() { close(stopCh) }
}

// dropCaches best-effort clears the page cache so a read test is not served
// from RAM. Only works as root.
func dropCaches(cfg Config) {
	if !cfg.IsRoot {
		return
	}
	_ = exec.Command("sync").Run()
	_ = os.WriteFile("/proc/sys/vm/drop_caches", []byte("3\n"), 0o644)
}

func firstLineContaining(s, sub string) string {
	for _, ln := range strings.Split(s, "\n") {
		if strings.Contains(ln, sub) {
			return strings.TrimSpace(ln)
		}
	}
	return ""
}

// ctxSleep sleeps d unless ctx is cancelled first.
func ctxSleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
