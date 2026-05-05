package catalog

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(context.Background(), p)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestGetOffset_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := db.GetOffset(context.Background(), "/no/such/file")
	if !errors.Is(err, ErrNoOffset) {
		t.Errorf("err = %v, want ErrNoOffset", err)
	}
}

func TestGetOffset_RoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	_, err := db.sql.ExecContext(ctx,
		`INSERT INTO tail_offsets (file, offset, inode, updated_ns) VALUES (?, ?, ?, ?)`,
		"/var/log/etl/a.log", int64(4096), uint64(123), time.Now().UnixNano())
	if err != nil {
		t.Fatal(err)
	}
	o, err := db.GetOffset(ctx, "/var/log/etl/a.log")
	if err != nil {
		t.Fatalf("GetOffset: %v", err)
	}
	if o.Offset != 4096 || o.Inode != 123 {
		t.Errorf("got %+v", o)
	}
}

func TestDeleteOffset(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	_, err := db.sql.ExecContext(ctx,
		`INSERT INTO tail_offsets (file, offset, inode, updated_ns) VALUES (?, ?, ?, ?)`,
		"/x", int64(0), uint64(1), int64(0))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteOffset(ctx, "/x"); err != nil {
		t.Fatalf("DeleteOffset: %v", err)
	}
	_, err = db.GetOffset(ctx, "/x")
	if !errors.Is(err, ErrNoOffset) {
		t.Errorf("after delete err = %v, want ErrNoOffset", err)
	}
}
