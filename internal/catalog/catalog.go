package catalog

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type DB struct {
	sql *sql.DB
}

const schemaVersion = 1

const ddl = `
CREATE TABLE IF NOT EXISTS file_events (
    seq_id      INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type  INTEGER NOT NULL,
    path        TEXT    NOT NULL,
    event_ts_ns INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_file_events_path ON file_events(path);

CREATE TABLE IF NOT EXISTS tail_offsets (
    file       TEXT PRIMARY KEY,
    offset     INTEGER NOT NULL,
    inode      INTEGER NOT NULL,
    updated_ns INTEGER NOT NULL
);
`

var pragmas = []string{
	"PRAGMA synchronous = NORMAL",
	"PRAGMA wal_autocheckpoint = 10000",
	"PRAGMA foreign_keys = OFF",
	fmt.Sprintf("PRAGMA user_version = %d", schemaVersion),
}

func Open(ctx context.Context, path string) (*DB, error) {
	// Set journal_mode and busy_timeout via DSN so they apply at connection
	// open time. journal_mode=WAL needs to be set before any other connection
	// attaches in non-WAL mode; doing it inline here avoids a race where a
	// concurrent reader (e.g., a test polling the same file) prevents the
	// later `PRAGMA journal_mode=WAL` from acquiring the EXCLUSIVE lock and
	// hangs the call.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sdb.SetMaxOpenConns(1)

	for _, p := range pragmas {
		if _, err := sdb.ExecContext(ctx, p); err != nil {
			sdb.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}
	if _, err := sdb.ExecContext(ctx, ddl); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("ddl: %w", err)
	}
	return &DB{sql: sdb}, nil
}

func (d *DB) Close() error { return d.sql.Close() }
