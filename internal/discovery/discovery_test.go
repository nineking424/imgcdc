package discovery

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"imgcdc/internal/catalog"
)

func TestDiscovery_SpawnsTailerForMatchingFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "etl_defectimg_work01_2026_05_06.log")
	if err := os.WriteFile(logPath, nil, 0644); err != nil {
		t.Fatal(err)
	}

	db, _ := catalog.Open(context.Background(), filepath.Join(dir, "c.db"))
	defer db.Close()

	out := make(chan catalog.Record, 8)
	cfg := Config{
		LogDir:            dir,
		Pattern:           regexp.MustCompile(`^etl_defectimg_work\d+_\d{4}_\d{2}_\d{2}\.log$`),
		Keyword:           "DEFECTIMG.PARSE.OK",
		Separator:         " - ",
		Grace:             90 * time.Minute,
		DiscoveryInterval: 20 * time.Millisecond,
		TailInterval:      10 * time.Millisecond,
		Now:               func() time.Time { return time.Date(2026, 5, 6, 12, 0, 0, 0, time.Local) },
	}

	d := New(cfg, db, out)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("DEFECTIMG.PARSE.OK : /tmp/x - /real/y\n")
	f.Close()

	select {
	case rec := <-out:
		if rec.Path != "/real/y" {
			t.Errorf("Path = %q", rec.Path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no record from discovered file")
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run: %v", err)
	}
}
