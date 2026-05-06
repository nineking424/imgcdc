package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintVersion(t *testing.T) {
	version = "1.2.3"
	commit = "abcdef0"
	date = "2026-05-06T12:00:00Z"
	t.Cleanup(func() {
		version = "dev"
		commit = "none"
		date = "unknown"
	})

	var buf bytes.Buffer
	printVersion(&buf)

	got := buf.String()
	for _, want := range []string{"imgcdc", "1.2.3", "abcdef0", "2026-05-06T12:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Errorf("printVersion() output %q missing %q", got, want)
		}
	}
}
