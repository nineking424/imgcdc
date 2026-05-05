package writer

import (
	"context"
	"fmt"

	"imgcdc/internal/catalog"
)

type Writer struct {
	db *catalog.DB
	in <-chan catalog.Record
}

func New(db *catalog.DB, in <-chan catalog.Record) *Writer {
	return &Writer{db: db, in: in}
}

// Run drains the input channel until it is closed. The channel owner
// (discovery, in production) must close the channel for Run to return.
func (w *Writer) Run(ctx context.Context) error {
	for r := range w.in {
		if err := w.db.WriteRecord(ctx, r); err != nil {
			return fmt.Errorf("writer: %w", err)
		}
	}
	return nil
}
