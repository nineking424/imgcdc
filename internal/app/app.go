package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"imgcdc/internal/catalog"
	"imgcdc/internal/discovery"
	"imgcdc/internal/writer"
)

const ChannelBuffer = 256

func Run(ctx context.Context, dbPath string, dcfg discovery.Config, shutdownTimeout time.Duration) error {
	db, err := catalog.Open(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open catalog: %w", err)
	}
	defer db.Close()

	ch := make(chan catalog.Record, ChannelBuffer)
	g, gctx := errgroup.WithContext(ctx)

	d := discovery.New(dcfg, db, ch)
	g.Go(func() error {
		defer close(ch)
		return d.Run(gctx)
	})

	w := writer.New(db, ch)
	g.Go(func() error {
		return w.Run(gctx)
	})

	waitDone := make(chan error, 1)
	go func() { waitDone <- g.Wait() }()

	select {
	case err := <-waitDone:
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	case <-ctx.Done():
		select {
		case err := <-waitDone:
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		case <-time.After(shutdownTimeout):
			return errors.New("shutdown timeout exceeded")
		}
	}
}
