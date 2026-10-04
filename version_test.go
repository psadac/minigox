package main

import (
	"strings"
	"testing"
)

func TestVersionStringDefaults(t *testing.T) {
	origVersion, origCommit, origDate := version, commit, date
	t.Cleanup(func() { version, commit, date = origVersion, origCommit, origDate })

	version, commit, date = "", "", ""
	got := versionString()
	if !strings.HasPrefix(got, "minigox dev ") {
		t.Errorf("expected dev fallback, got %q", got)
	}
	if strings.Contains(got, "commit") || strings.Contains(got, "built") {
		t.Errorf("unexpected build metadata in %q", got)
	}
}

func TestVersionStringWithMetadata(t *testing.T) {
	origVersion, origCommit, origDate := version, commit, date
	t.Cleanup(func() { version, commit, date = origVersion, origCommit, origDate })

	version, commit, date = "v1.2.3", "abc1234", "2026-10-04T00:00:00Z"
	got := versionString()
	for _, want := range []string{"minigox", "v1.2.3", "commit abc1234", "built 2026-10-04T00:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}
}