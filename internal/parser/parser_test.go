package parser

import (
	"errors"
	"testing"
	"time"
)

var fixedNow = func() time.Time {
	return time.Unix(1714999381, 234_000_000)
}

func TestParse_HappyPath(t *testing.T) {
	line := "2026-05-06 14:23:01.234 INFO [load] DEFECTIMG.PARSE.OK : /tmp/x.info - /real/path/x.info"
	ev, err := Parse(line, "DEFECTIMG.PARSE.OK", " - ", fixedNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Path != "/real/path/x.info" {
		t.Errorf("Path = %q, want /real/path/x.info", ev.Path)
	}
	if ev.TSNs != fixedNow().UnixNano() {
		t.Errorf("TSNs = %d, want %d", ev.TSNs, fixedNow().UnixNano())
	}
	_ = errors.New
}

func TestParse_EdgeCases(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		wantErr  error
		wantPath string
	}{
		{"no keyword", "2026-05-06 INFO some other line", ErrNoMatch, ""},
		{"keyword but no separator", "DEFECTIMG.PARSE.OK only", ErrMalformed, ""},
		{"relative path", "DEFECTIMG.PARSE.OK : /tmp/x - relative/x.info", ErrMalformed, ""},
		{"trailing CR", "DEFECTIMG.PARSE.OK : /tmp/x - /real/x.info\r", nil, "/real/x.info"},
		{"multiple separators", "DEFECTIMG.PARSE.OK : /tmp/a - /tmp/b - /real/x.info", nil, "/real/x.info"},
		{"empty real path", "DEFECTIMG.PARSE.OK : /tmp/x - ", ErrMalformed, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := Parse(tc.line, "DEFECTIMG.PARSE.OK", " - ", fixedNow)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && ev.Path != tc.wantPath {
				t.Errorf("Path = %q, want %q", ev.Path, tc.wantPath)
			}
		})
	}
}
