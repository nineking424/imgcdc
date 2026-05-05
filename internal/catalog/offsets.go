package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrNoOffset = errors.New("catalog: no saved offset for file")

type Offset struct {
	File   string
	Offset int64
	Inode  uint64
}

func (d *DB) GetOffset(ctx context.Context, file string) (Offset, error) {
	var o Offset
	err := d.sql.QueryRowContext(ctx,
		`SELECT file, offset, inode FROM tail_offsets WHERE file = ?`,
		file).Scan(&o.File, &o.Offset, &o.Inode)
	if errors.Is(err, sql.ErrNoRows) {
		return Offset{}, ErrNoOffset
	}
	if err != nil {
		return Offset{}, fmt.Errorf("query tail_offsets: %w", err)
	}
	return o, nil
}

func (d *DB) DeleteOffset(ctx context.Context, file string) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM tail_offsets WHERE file = ?`, file); err != nil {
		return fmt.Errorf("delete tail_offsets: %w", err)
	}
	return nil
}
