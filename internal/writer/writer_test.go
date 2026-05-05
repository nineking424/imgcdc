package writer

import (
	"context"
	"path/filepath"
	"testing"

	"imgcdc/internal/catalog"
)

func TestWriter_DrainsAllRecords(t *testing.T) {
	db, err := catalog.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	in := make(chan catalog.Record, 3)
	in <- catalog.Record{Path: "/a", EventTSNs: 1, LogFile: "/log", Offset: 10, Inode: 1}
	in <- catalog.Record{Path: "/b", EventTSNs: 2, LogFile: "/log", Offset: 20, Inode: 1}
	in <- catalog.Record{Path: "/c", EventTSNs: 3, LogFile: "/log", Offset: 30, Inode: 1}
	close(in)

	w := New(db, in)
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	o, err := db.GetOffset(context.Background(), "/log")
	if err != nil {
		t.Fatalf("GetOffset: %v", err)
	}
	if o.Offset != 30 {
		t.Errorf("final offset = %d, want 30", o.Offset)
	}
}
