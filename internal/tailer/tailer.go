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
	f, err := os.Open(t.cfg.Path)
	if err != nil {
		return fmt.Errorf("open %s: %w", t.cfg.Path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	currentInode := inode.Of(info)

	var startOffset int64
	saved, gerr := t.db.GetOffset(ctx, t.cfg.Path)
	if gerr == nil {
		if saved.Inode == currentInode {
			startOffset = saved.Offset
		} else {
			slog.Info("inode mismatch on startup; restarting from 0",
				"file", t.cfg.Path, "old_inode", saved.Inode, "new_inode", currentInode)
		}
	} else if !errors.Is(gerr, catalog.ErrNoOffset) {
		return fmt.Errorf("get offset: %w", gerr)
	}
	if _, err := f.Seek(startOffset, io.SeekStart); err != nil {
		return fmt.Errorf("seek: %w", err)
	}

	reader := bufio.NewReader(f)
	offset := startOffset

	var partial []byte
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		chunk, rerr := reader.ReadBytes('\n')
		if errors.Is(rerr, io.EOF) {
			partial = append(partial, chunk...)
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
