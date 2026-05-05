package parser

import (
	"errors"
	"path/filepath"
	"strings"
	"time"
)

type Event struct {
	Path string
	TSNs int64
}

var (
	ErrNoMatch   = errors.New("parser: line does not contain keyword")
	ErrMalformed = errors.New("parser: malformed matched line")
)

func Parse(line, keyword, separator string, now func() time.Time) (Event, error) {
	if !strings.Contains(line, keyword) {
		return Event{}, ErrNoMatch
	}
	parts := strings.Split(line, separator)
	if len(parts) < 2 {
		return Event{}, ErrMalformed
	}
	realPath := strings.TrimRight(strings.TrimSpace(parts[len(parts)-1]), "\r\n")
	if realPath == "" || !filepath.IsAbs(realPath) {
		return Event{}, ErrMalformed
	}
	return Event{Path: realPath, TSNs: now().UnixNano()}, nil
}
