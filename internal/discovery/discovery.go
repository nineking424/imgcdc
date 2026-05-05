package discovery

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"imgcdc/internal/catalog"
	"imgcdc/internal/tailer"
)

type Config struct {
	LogDir            string
	Pattern           *regexp.Regexp
	Keyword           string
	Separator         string
	Grace             time.Duration
	DiscoveryInterval time.Duration
	TailInterval      time.Duration
	Now               func() time.Time
}

type Discoverer struct {
	cfg Config
	db  *catalog.DB
	out chan<- catalog.Record
}

func New(cfg Config, db *catalog.DB, out chan<- catalog.Record) *Discoverer {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Discoverer{cfg: cfg, db: db, out: out}
}

type activeTailer struct {
	cancel context.CancelFunc
}

func (d *Discoverer) Run(ctx context.Context) error {
	active := map[string]*activeTailer{}
	var wg sync.WaitGroup
	defer func() {
		for _, a := range active {
			a.cancel()
		}
		wg.Wait()
	}()

	tick := time.NewTicker(d.cfg.DiscoveryInterval)
	defer tick.Stop()

	for {
		d.reconcile(ctx, active, &wg)
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

func (d *Discoverer) reconcile(ctx context.Context, active map[string]*activeTailer, wg *sync.WaitGroup) {
	desired, err := d.scan()
	if err != nil {
		slog.Warn("discovery scan failed", "err", err)
		return
	}
	desiredSet := make(map[string]struct{}, len(desired))
	for _, p := range desired {
		desiredSet[p] = struct{}{}
	}

	for _, p := range desired {
		if _, ok := active[p]; ok {
			continue
		}
		tctx, cancel := context.WithCancel(ctx)
		active[p] = &activeTailer{cancel: cancel}
		wg.Add(1)
		go d.runTailer(tctx, p, wg)
	}

	for p, a := range active {
		if _, keep := desiredSet[p]; keep {
			continue
		}
		slog.Info("retiring tailer", "file", p)
		a.cancel()
		delete(active, p)
	}
}

func (d *Discoverer) runTailer(ctx context.Context, path string, wg *sync.WaitGroup) {
	defer wg.Done()
	t := tailer.New(tailer.Config{
		Path:      path,
		Keyword:   d.cfg.Keyword,
		Separator: d.cfg.Separator,
		Interval:  d.cfg.TailInterval,
	}, d.db, d.out)
	if err := t.Run(ctx); err != nil {
		slog.Warn("tailer exited with error", "file", path, "err", err)
	}
}

func (d *Discoverer) scan() ([]string, error) {
	entries, err := os.ReadDir(d.cfg.LogDir)
	if err != nil {
		return nil, err
	}
	now := d.cfg.Now()
	var out []string
	for _, e := range entries {
		if e.IsDir() || !d.cfg.Pattern.MatchString(e.Name()) {
			continue
		}
		day, ok := parseDateFromName(e.Name())
		if !ok {
			continue
		}
		if !inWindow(day, now, d.cfg.Grace) {
			continue
		}
		out = append(out, filepath.Join(d.cfg.LogDir, e.Name()))
	}
	return out, nil
}

func parseDateFromName(name string) (time.Time, bool) {
	base := name
	if i := strings.LastIndex(base, "."); i >= 0 {
		base = base[:i]
	}
	if len(base) < 10 {
		return time.Time{}, false
	}
	last := base[len(base)-10:]
	if last[4] != '_' || last[7] != '_' {
		return time.Time{}, false
	}
	y, err1 := strconv.Atoi(last[0:4])
	m, err2 := strconv.Atoi(last[5:7])
	dd, err3 := strconv.Atoi(last[8:10])
	if err1 != nil || err2 != nil || err3 != nil {
		return time.Time{}, false
	}
	return time.Date(y, time.Month(m), dd, 0, 0, 0, 0, time.Local), true
}

// inWindow returns true if a file dated `fileDay` is still in the active tail
// window at time `now`, given `grace` past midnight on the day-after.
func inWindow(fileDay, now time.Time, grace time.Duration) bool {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterday := today.AddDate(0, 0, -1)
	switch {
	case fileDay.Equal(today):
		return true
	case fileDay.Equal(yesterday) && now.Sub(today) < grace:
		return true
	default:
		return false
	}
}
