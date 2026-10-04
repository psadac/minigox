package main

import (
	"path/filepath"
	"testing"
)

func TestNewConfig(t *testing.T) {
	srcDir := "testdata/cmd/notest"
	outDir := filepath.Join(t.TempDir(), "bin")

	c, err := NewConfig(Options{SrcDir: srcDir, OutDir: outDir, Include: "linux/amd64"})
	if err != nil {
		t.Fatal(err)
	}

	absSrc, _ := filepath.Abs(srcDir)
	if c.srcDir != absSrc {
		t.Errorf("srcDir = %q, want %q", c.srcDir, absSrc)
	}
	if c.appName != "notest" {
		t.Errorf("appName = %q, want %q", c.appName, "notest")
	}
	if c.maxWorkers < 2 {
		t.Errorf("maxWorkers = %d, want at least 2 when auto-detected", c.maxWorkers)
	}
}

func TestNewConfig_Workers(t *testing.T) {
	srcDir := "testdata/cmd/notest"

	c, err := NewConfig(Options{SrcDir: srcDir, OutDir: t.TempDir(), Workers: 7})
	if err != nil {
		t.Fatal(err)
	}
	if c.maxWorkers != 7 {
		t.Errorf("maxWorkers = %d, want 7", c.maxWorkers)
	}
}

func TestNewConfig_NegativeWorkers(t *testing.T) {
	_, err := NewConfig(Options{SrcDir: "testdata/cmd/notest", OutDir: t.TempDir(), Workers: -1})
	if err == nil {
		t.Fatal("expected error for negative worker count")
	}
}

func TestNewConfig_InvalidSrcDir(t *testing.T) {
	_, err := NewConfig(Options{SrcDir: "/nonexistent/path", OutDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected error for invalid source directory")
	}
}

func TestNewConfig_InvalidPattern(t *testing.T) {
	_, err := NewConfig(Options{SrcDir: "testdata/cmd/notest", OutDir: t.TempDir(), Include: "linux/[amd64"})
	if err == nil {
		t.Fatal("expected error for malformed include pattern")
	}
}
