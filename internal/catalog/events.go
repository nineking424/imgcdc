package catalog

import (
	"context"
	"fmt"
	"time"
)

type Record struct {
	Path      string
	EventTSNs int64
	LogFile   string
	Offset    int64
	Inode     uint64
}

const eventTypeUpsert = 1

func (d *DB) WriteRecord(ctx context.Context, r Record) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO file_events (event_type, path, event_ts_ns) VALUES (?, ?, ?)`,
		eventTypeUpsert, r.Path, r.EventTSNs); err != nil {
		return fmt.Errorf("insert file_events: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO tail_offsets (file, offset, inode, updated_ns) VALUES (?, ?, ?, ?)`,
		r.LogFile, r.Offset, r.Inode, time.Now().UnixNano()); err != nil {
		return fmt.Errorf("upsert tail_offsets: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
