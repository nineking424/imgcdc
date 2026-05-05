package tailer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"imgcdc/internal/catalog"
	"imgcdc/internal/inode"
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

func TestTailer_ResumesFromSavedOffset(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log")
	line := "X DEFECTIMG.PARSE.OK : /tmp/a - /real/a\n"
	seed := line + line
	if err := os.WriteFile(logPath, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}

	db, _ := catalog.Open(context.Background(), filepath.Join(dir, "c.db"))
	defer db.Close()

	info, _ := os.Stat(logPath)
	pre := catalog.Record{
		Path: "/real/a", EventTSNs: 1, LogFile: logPath,
		Offset: int64(len(line)), Inode: inode.Of(info),
	}
	if err := db.WriteRecord(context.Background(), pre); err != nil {
		t.Fatal(err)
	}

	out := make(chan catalog.Record, 4)
	tlr := New(Config{Path: logPath, Keyword: "DEFECTIMG.PARSE.OK", Separator: " - ", Interval: 10 * time.Millisecond}, db, out)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tlr.Run(ctx) }()

	select {
	case rec := <-out:
		if rec.Offset != int64(2*len(line)) {
			t.Errorf("Offset = %d, want %d", rec.Offset, 2*len(line))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no record")
	}

	select {
	case extra := <-out:
		t.Errorf("unexpected extra record: %+v", extra)
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	<-done
}

func TestTailer_DoesNotEmitPartialLine(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log")
	if err := os.WriteFile(logPath, nil, 0644); err != nil {
		t.Fatal(err)
	}

	db, _ := catalog.Open(context.Background(), filepath.Join(dir, "c.db"))
	defer db.Close()
	out := make(chan catalog.Record, 4)
	tlr := New(Config{Path: logPath, Keyword: "DEFECTIMG.PARSE.OK", Separator: " - ", Interval: 10 * time.Millisecond}, db, out)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tlr.Run(ctx) }()

	f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("X DEFECTIMG.PARSE.OK : /tmp/a - /real/a")
	f.Sync()

	select {
	case rec := <-out:
		t.Fatalf("partial line was emitted: %+v", rec)
	case <-time.After(200 * time.Millisecond):
	}

	f.WriteString("\nY DEFECTIMG.PARSE.OK : /tmp/b - /real/b\n")
	f.Close()

	var got []string
	for i := 0; i < 2; i++ {
		select {
		case rec := <-out:
			got = append(got, rec.Path)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout for completed lines")
		}
	}
	if got[0] != "/real/a" || got[1] != "/real/b" {
		t.Errorf("got %v", got)
	}

	cancel()
	<-done
}

func TestTailer_DetectsInodeRotation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log")
	line := "X DEFECTIMG.PARSE.OK : /tmp/x - /real/first\n"
	if err := os.WriteFile(logPath, []byte(line), 0644); err != nil {
		t.Fatal(err)
	}

	db, _ := catalog.Open(context.Background(), filepath.Join(dir, "c.db"))
	defer db.Close()

	out := make(chan catalog.Record, 4)
	tlr := New(Config{Path: logPath, Keyword: "DEFECTIMG.PARSE.OK", Separator: " - ", Interval: 10 * time.Millisecond}, db, out)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tlr.Run(ctx) }()

	select {
	case rec := <-out:
		if rec.Path != "/real/first" {
			t.Errorf("Path = %q", rec.Path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no first record")
	}

	newLine := "Y DEFECTIMG.PARSE.OK : /tmp/y - /real/second\n"
	tmp := logPath + ".new"
	if err := os.WriteFile(tmp, []byte(newLine), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, logPath); err != nil {
		t.Fatal(err)
	}

	select {
	case rec := <-out:
		if rec.Path != "/real/second" {
			t.Errorf("Path = %q, want /real/second", rec.Path)
		}
		if rec.Offset != int64(len(newLine)) {
			t.Errorf("Offset = %d, want %d", rec.Offset, len(newLine))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no second record after rotation")
	}

	cancel()
	<-done
}
