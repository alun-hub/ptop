package bench

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func sqliteDuration(d Depth) time.Duration {
	switch d {
	case Quick:
		return 3 * time.Second
	case Deep:
		return 10 * time.Second
	default:
		return 5 * time.Second
	}
}

func removeGlob(pattern string) {
	if files, _ := filepath.Glob(pattern); files != nil {
		for _, f := range files {
			_ = os.Remove(f)
		}
	}
}

func sqliteMetric(txnsPerSec float64) Metric {
	var verdict Verdict
	switch {
	case txnsPerSec >= 2000:
		verdict = VGood
	case txnsPerSec >= 400:
		verdict = VOkay
	case txnsPerSec > 0:
		verdict = VPoor
	default:
		verdict = VNeutral
	}
	return Metric{
		Name:    "Database transactions (SQLite ACID)",
		Display: fmt.Sprintf("%.0f txn/s", txnsPerSec),
		Verdict: verdict,
		Note:    "synchronous ACID commit rate in WAL mode - bottlenecks relational databases (PostgreSQL, MySQL, SQLite)",
		Bar:     normLog(clampLo(txnsPerSec, 50), 50, 10000),
		HasBar:  true,
		ScaleLo: "slow HDD",
		ScaleHi: "fast NVMe",
	}.cmp(txnsPerSec, "txn/s", false)
}

func diskSQLite(ctx context.Context, cfg Config, dir string, out chan<- Event) (Metric, error) {
	dur := sqliteDuration(cfg.Depth)
	if out != nil {
		out <- LogLine{Text: fmt.Sprintf("database benchmark: running SQLite ACID transactions (%v)...", dur)}
		out <- Progress{Frac: 0.96, Label: "database: transactions"}
	}

	var txnsPerSec float64
	var err error

	if have("sqlite3") {
		txnsPerSec, err = diskSQLiteCLI(ctx, dur, dir, out)
		if err != nil {
			if ctx.Err() != nil {
				return Metric{}, ctx.Err()
			}
			if out != nil {
				out <- LogLine{Text: fmt.Sprintf("sqlite3 failed (%v) - falling back to pure-Go WAL simulation", err)}
			}
			txnsPerSec, err = diskSQLitePureGo(ctx, dur, dir, out)
		}
	} else {
		if out != nil {
			out <- LogLine{Text: "sqlite3 not found - falling back to pure-Go WAL transaction simulation"}
		}
		txnsPerSec, err = diskSQLitePureGo(ctx, dur, dir, out)
	}

	if err != nil {
		return Metric{}, err
	}

	if out != nil {
		out <- LogLine{Text: fmt.Sprintf("database benchmark: %.0f txn/s", txnsPerSec)}
	}

	return sqliteMetric(txnsPerSec), nil
}

func diskSQLiteCLI(ctx context.Context, dur time.Duration, dir string, out chan<- Event) (float64, error) {
	dbPath := filepath.Join(dir, fmt.Sprintf(".ptop-sqlite-%d.db", os.Getpid()))
	defer removeGlob(filepath.Join(dir, fmt.Sprintf(".ptop-sqlite-%d*", os.Getpid())))

	initSQL := "PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; CREATE TABLE bench (id INTEGER PRIMARY KEY, k TEXT, v BLOB); CREATE INDEX idx_k ON bench(k);"
	cmdInit := exec.CommandContext(ctx, "sqlite3", dbPath, initSQL)
	if outB, err := cmdInit.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("sqlite3 init failed: %v (%s)", err, string(outB))
	}

	cmd := exec.CommandContext(ctx, "sqlite3", dbPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, err
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return 0, err
	}

	start := time.Now()
	deadline := start.Add(dur)
	txnCount := 0
	var sqlBuf strings.Builder

	for {
		if time.Now().After(deadline) {
			break
		}
		if err := ctx.Err(); err != nil {
			_ = stdin.Close()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return 0, err
		}
		sqlBuf.Reset()
		batchSize := 25
		for b := 0; b < batchSize; b++ {
			id := txnCount + b + 1
			fmt.Fprintf(&sqlBuf, "BEGIN IMMEDIATE; INSERT INTO bench (k, v) VALUES ('key_%d', X'0102030405060708090a0b0c0d0e0f10'); SELECT v FROM bench WHERE k = 'key_%d'; COMMIT;\n", id, id)
		}
		if _, err := stdin.Write([]byte(sqlBuf.String())); err != nil {
			break
		}
		txnCount += batchSize
	}
	_ = stdin.Close()
	_ = cmd.Wait()

	elapsed := time.Since(start)
	if elapsed <= 0 {
		elapsed = time.Microsecond
	}

	cntCmd := exec.CommandContext(ctx, "sqlite3", dbPath, "SELECT count(*) FROM bench;")
	if cntOut, err := cntCmd.Output(); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(cntOut))); err == nil && n > 0 {
			txnCount = n
		}
	}

	txnsPerSec := float64(txnCount) / elapsed.Seconds()
	return txnsPerSec, nil
}

func diskSQLitePureGo(ctx context.Context, dur time.Duration, dir string, out chan<- Event) (float64, error) {
	dbPath := filepath.Join(dir, fmt.Sprintf(".ptop-wal-%d.db", os.Getpid()))
	walPath := filepath.Join(dir, fmt.Sprintf(".ptop-wal-%d.wal", os.Getpid()))
	defer func() {
		_ = os.Remove(dbPath)
		_ = os.Remove(walPath)
		removeGlob(filepath.Join(dir, fmt.Sprintf(".ptop-wal-%d*", os.Getpid())))
	}()

	dbFile, err := os.OpenFile(dbPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	defer dbFile.Close()

	initPage := make([]byte, 4096)
	copy(initPage, "SQLite format 3\x00")
	if _, err := dbFile.Write(initPage); err != nil {
		return 0, err
	}
	if err := dbFile.Sync(); err != nil {
		return 0, err
	}

	walFile, err := os.OpenFile(walPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	defer walFile.Close()

	walHeader := make([]byte, 32)
	// SQLite WAL header magic: 0x377f0683
	walHeader[0], walHeader[1], walHeader[2], walHeader[3] = 0x37, 0x7f, 0x06, 0x83
	// page size 4096 (0x1000)
	walHeader[8], walHeader[9], walHeader[10], walHeader[11] = 0x00, 0x00, 0x10, 0x00
	if _, err := walFile.Write(walHeader); err != nil {
		return 0, err
	}
	if err := walFile.Sync(); err != nil {
		return 0, err
	}

	frame := make([]byte, 4096)
	// 32-byte WAL frame header
	frame[0], frame[1], frame[2], frame[3] = 0x00, 0x00, 0x00, 0x01 // page 1
	for i := 32; i < 4096; i++ {
		frame[i] = byte(i % 251)
	}

	start := time.Now()
	deadline := start.Add(dur)
	txnCount := 0

	for {
		if time.Now().After(deadline) {
			break
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if _, err := walFile.Write(frame); err != nil {
			return 0, err
		}
		if err := walFile.Sync(); err != nil {
			return 0, err
		}
		txnCount++
	}

	elapsed := time.Since(start)
	if elapsed <= 0 {
		elapsed = time.Microsecond
	}
	txnsPerSec := float64(txnCount) / elapsed.Seconds()
	return txnsPerSec, nil
}
