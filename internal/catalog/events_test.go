package catalog

import (
	"context"
	"testing"
	"time"
)

func TestWriteRecord_InsertsEventAndUpsertsOffset(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	rec := Record{
		Path:      "/real/x.info",
		EventTSNs: time.Date(2026, 5, 6, 14, 23, 1, 234_000_000, time.UTC).UnixNano(),
		LogFile:   "/var/log/etl/work01_2026_05_06.log",
		Offset:    4096,
		Inode:     999,
	}
	if err := db.WriteRecord(ctx, rec); err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}

	var (
		seqID, eventType int64
		path             string
		ts               int64
	)
	err := db.sql.QueryRowContext(ctx,
		`SELECT seq_id, event_type, path, event_ts_ns FROM file_events`).
		Scan(&seqID, &eventType, &path, &ts)
	if err != nil {
		t.Fatalf("select event: %v", err)
	}
	if eventType != 1 || path != rec.Path || ts != rec.EventTSNs {
		t.Errorf("event row mismatch: %d %q %d", eventType, path, ts)
	}

	o, err := db.GetOffset(ctx, rec.LogFile)
	if err != nil {
		t.Fatalf("GetOffset: %v", err)
	}
	if o.Offset != rec.Offset || o.Inode != rec.Inode {
		t.Errorf("offset row mismatch: %+v", o)
	}
}

func TestWriteRecord_OffsetReplacesPriorValue(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	base := Record{Path: "/p", EventTSNs: 1, LogFile: "/log", Offset: 100, Inode: 1}
	if err := db.WriteRecord(ctx, base); err != nil {
		t.Fatal(err)
	}
	base.Offset = 200
	base.EventTSNs = 2
	if err := db.WriteRecord(ctx, base); err != nil {
		t.Fatal(err)
	}
	o, _ := db.GetOffset(ctx, "/log")
	if o.Offset != 200 {
		t.Errorf("Offset = %d, want 200", o.Offset)
	}
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM file_events`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("event count = %d, want 2", count)
	}
}

func TestWriteRecord_RollsBackEventOnOffsetFailure(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Force the second statement to fail by dropping the tail_offsets table.
	// The INSERT into file_events will succeed, but the UPSERT into
	// tail_offsets will return an error, exercising the deferred Rollback.
	if _, err := db.sql.ExecContext(ctx, `DROP TABLE tail_offsets`); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	rec := Record{Path: "/p", EventTSNs: 1, LogFile: "/log", Offset: 1, Inode: 1}
	if err := db.WriteRecord(ctx, rec); err == nil {
		t.Fatal("WriteRecord: want error, got nil")
	}

	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM file_events`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("file_events count = %d, want 0 (rollback should have undone insert)", count)
	}
}
