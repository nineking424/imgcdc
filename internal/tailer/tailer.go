package tailer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"imgcdc/internal/catalog"
	"imgcdc/internal/inode"
	"imgcdc/internal/parser"
)

type Config struct {
	Path      string
	Keyword   string
	Separator string
	Interval  time.Duration
}

type Tailer struct {
	cfg Config
	db  *catalog.DB
	out chan<- catalog.Record
	now func() time.Time
}

func New(cfg Config, db *catalog.DB, out chan<- catalog.Record) *Tailer {
	return &Tailer{cfg: cfg, db: db, out: out, now: time.Now}
}

func (t *Tailer) Run(ctx context.Context) error {
	var (
		f            *os.File
		reader       *bufio.Reader
		currentInode uint64
		offset       int64
		partial      []byte
	)

	open := func() error {
		nf, err := os.Open(t.cfg.Path)
		if err != nil {
			return fmt.Errorf("open %s: %w", t.cfg.Path, err)
		}
		info, err := nf.Stat()
		if err != nil {
			nf.Close()
			return fmt.Errorf("stat: %w", err)
		}
		ci := inode.Of(info)

		var start int64
		saved, gerr := t.db.GetOffset(ctx, t.cfg.Path)
		if gerr == nil && saved.Inode == ci {
			start = saved.Offset
		} else if gerr != nil && !errors.Is(gerr, catalog.ErrNoOffset) {
			nf.Close()
			return fmt.Errorf("get offset: %w", gerr)
		}
		if _, err := nf.Seek(start, io.SeekStart); err != nil {
			nf.Close()
			return fmt.Errorf("seek: %w", err)
		}

		if f != nil {
			f.Close()
		}
		f = nf
		reader = bufio.NewReader(f)
		currentInode = ci
		offset = start
		partial = nil
		return nil
	}

	if err := open(); err != nil {
		return err
	}
	defer func() {
		if f != nil {
			f.Close()
		}
	}()

	rotated := func() bool {
		info, err := os.Stat(t.cfg.Path)
		if err != nil {
			return false
		}
		return inode.Of(info) != currentInode
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		chunk, rerr := reader.ReadBytes('\n')
		if errors.Is(rerr, io.EOF) {
			partial = append(partial, chunk...)

			if rotated() {
				slog.Info("inode rotation detected", "file", t.cfg.Path)
				if err := open(); err != nil {
					return err
				}
				continue
			}

			select {
			case <-ctx.Done():
				return nil
			case <-time.After(t.cfg.Interval):
			}
			continue
		}
		if rerr != nil {
			return fmt.Errorf("read: %w", rerr)
		}

		complete := append(partial, chunk...)
		partial = nil
		offset += int64(len(complete))

		ev, perr := parser.Parse(string(complete), t.cfg.Keyword, t.cfg.Separator, t.now)
		if errors.Is(perr, parser.ErrNoMatch) {
			continue
		}
		if perr != nil {
			slog.Warn("malformed line", "file", t.cfg.Path, "err", perr)
			continue
		}
		rec := catalog.Record{
			Path:      ev.Path,
			EventTSNs: ev.TSNs,
			LogFile:   t.cfg.Path,
			Offset:    offset,
			Inode:     currentInode,
		}
		select {
		case t.out <- rec:
		case <-ctx.Done():
			return nil
		}
	}
}
