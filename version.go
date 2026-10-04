package main

import (
	"runtime"
	"strings"
)

// Build information injected at link time, e.g.
//
//	go build -ldflags "-X main.version=v1.2.3 -X main.commit=abc123 -X main.date=2026-10-04" .
var (
	version = "dev"
	commit  = ""
	date    = ""
)

// versionString renders the build version of the running binary, falling back
// to "dev" when the linker did not inject metadata.
func versionString() string {
	v := version
	if v == "" {
		v = "dev"
	}

	parts := []string{"minigox", v, runtime.Version(), runtime.GOOS + "/" + runtime.GOARCH}
	if commit != "" {
		parts = append(parts, "commit "+commit)
	}
	if date != "" {
		parts = append(parts, "built "+date)
	}
	return strings.Join(parts, " ")
}