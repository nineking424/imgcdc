package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"imgcdc/internal/app"
	"imgcdc/internal/discovery"
)

const (
	defaultPattern   = `^etl_defectimg_work\d+_\d{4}_\d{2}_\d{2}\.log$`
	defaultKeyword   = "DEFECTIMG.PARSE.OK"
	defaultSeparator = " - "
)

func main() {
	var (
		logDir            = flag.String("log-dir", "", "directory containing ETL log files (required)")
		dbPath            = flag.String("db", "", "SQLite catalog path (required)")
		filePattern       = flag.String("file-pattern", defaultPattern, "regex matching log file names")
		keyword           = flag.String("keyword", defaultKeyword, "match keyword on each log line")
		pathSeparator     = flag.String("path-separator", defaultSeparator, "separator between temp and real path")
		discoveryInterval = flag.Duration("discovery-interval", time.Second, "log directory poll interval")
		tailInterval      = flag.Duration("tail-interval", time.Second, "per-file tail poll interval")
		grace             = flag.Duration("grace", 90*time.Minute, "yesterday-file grace window")
		shutdownTimeout   = flag.Duration("shutdown-timeout", 5*time.Second, "graceful shutdown timeout")
		logLevel          = flag.String("log-level", "info", "debug|info|warn|error")
	)
	flag.Parse()

	if *logDir == "" || *dbPath == "" {
		fmt.Fprintln(os.Stderr, "imgcdc: --log-dir and --db are required")
		flag.Usage()
		os.Exit(2)
	}
	if err := configureLogging(*logLevel); err != nil {
		fmt.Fprintf(os.Stderr, "imgcdc: %v\n", err)
		os.Exit(2)
	}

	pattern, err := regexp.Compile(*filePattern)
	if err != nil {
		slog.Error("invalid --file-pattern", "err", err)
		os.Exit(2)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := app.Run(rootCtx, *dbPath, discovery.Config{
		LogDir:            *logDir,
		Pattern:           pattern,
		Keyword:           *keyword,
		Separator:         *pathSeparator,
		Grace:             *grace,
		DiscoveryInterval: *discoveryInterval,
		TailInterval:      *tailInterval,
	}, *shutdownTimeout); err != nil {
		slog.Error("imgcdc exited", "err", err)
		os.Exit(1)
	}
}

func configureLogging(level string) error {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "info":
		lv = slog.LevelInfo
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		return fmt.Errorf("invalid --log-level %q", level)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv})))
	return nil
}
