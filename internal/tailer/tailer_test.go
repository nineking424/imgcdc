package tailer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"imgcdc/internal/catalog"
)

func TestTailer_EmitsMatchedLines(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "etl_defectimg_work01_2026_05_06.log")
	if err := os.WriteFile(logPath, nil, 0644); err != nil {
		t.Fatal(err)
	}

	db, err := catalog.Open(context.Background(), filepath.Join(dir, "cat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	out := make(chan catalog.Record, 4)
	tlr := New(Config{
		Path:      logPath,
		Keyword:   "DEFECTIMG.PARSE.OK",
		Separator: " - ",
		Interval:  10 * time.Millisecond,
	}, db, out)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tlr.Run(ctx) }()

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	skipped := "2026-05-06 14:23:02.000 INFO [load] OTHER thing\n"
	matched := "2026-05-06 14:23:01.234 INFO [load] DEFECTIMG.PARSE.OK : /tmp/x - /real/x.info\n"
	if _, err := f.WriteString(skipped + matched); err != nil {
		t.Fatal(err)
	}
	f.Close()

	select {
	case rec := <-out:
		if rec.Path != "/real/x.info" {
			t.Errorf("Path = %q", rec.Path)
		}
		if rec.LogFile != logPath {
			t.Errorf("LogFile = %q", rec.LogFile)
		}
		if rec.Offset != int64(len(skipped)+len(matched)) {
			t.Errorf("Offset = %d, want %d", rec.Offset, len(skipped)+len(matched))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for record")
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run returned: %v", err)
	}
}
