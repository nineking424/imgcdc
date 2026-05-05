package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"imgcdc/internal/app"
	"imgcdc/internal/discovery"
)

func TestEndToEnd_HappyPath(t *testing.T) {
	base := t.TempDir()
	logDir := filepath.Join(base, "log")
	if err := os.Mkdir(logDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(base, "c.db")

	today := time.Now().Format("2006_01_02")
	logFile := filepath.Join(logDir, fmt.Sprintf("etl_defectimg_work01_%s.log", today))
	if err := os.WriteFile(logFile, nil, 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- app.Run(ctx, dbPath, discovery.Config{
			LogDir:            logDir,
			Pattern:           regexp.MustCompile(`^etl_defectimg_work\d+_\d{4}_\d{2}_\d{2}\.log$`),
			Keyword:           "DEFECTIMG.PARSE.OK",
			Separator:         " - ",
			Grace:             90 * time.Minute,
			DiscoveryInterval: 50 * time.Millisecond,
			TailInterval:      20 * time.Millisecond,
		}, 2*time.Second)
	}()

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		fmt.Fprintf(f, "2026-05-06 14:23:0%d.000 INFO [load] DEFECTIMG.PARSE.OK : /tmp/x - /real/p%d\n", i, i)
	}
	f.Close()

	waitForRows(t, dbPath, 5, 5*time.Second)

	cancel()
	if err := <-done; err != nil {
		t.Errorf("app.Run: %v", err)
	}
}

func waitForRows(t *testing.T, dbPath string, want int, timeout time.Duration) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM file_events").Scan(&n); err == nil && n >= want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	var n int
	db.QueryRow("SELECT COUNT(*) FROM file_events").Scan(&n)
	t.Fatalf("after %s, got %d rows, want %d", timeout, n, want)
}
