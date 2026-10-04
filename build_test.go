package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// testConfig returns a Config wired up for building srcDir into outDir without
// going through NewConfig validation.
func testConfig(t *testing.T, srcDir, outDir string) *Config {
	t.Helper()
	c := &Config{
		goCmd:   "go",
		homeDir: "/home/test",
		srcDir:  srcDir,
		outDir:  outDir,
		appName: filepath.Base(srcDir),
	}
	if err := c.getGoVars("GOCACHE"); err != nil {
		t.Fatal(err)
	}
	return c
}

// mustReadDir returns the names of the entries in dir.
func mustReadDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}

// platformsFromToolchain returns the platforms matching the config's filters,
// so tests can assert on the binaries a run should have produced.
func (c *Config) platformsFromToolchain(t *testing.T) []Platform {
	t.Helper()
	all, err := c.listPlatforms()
	if err != nil {
		t.Fatal(err)
	}
	return c.filterPlatforms(all)
}

func TestBuildError_Error(t *testing.T) {
	one := &BuildError{Platforms: []PlatformError{{Platform: Platform{OS: "linux", Arch: "amd64"}}}}
	if got := one.Error(); got != "build failed for 1 platform: linux/amd64" {
		t.Errorf("Error() = %q", got)
	}

	many := &BuildError{Platforms: []PlatformError{
		{Platform: Platform{OS: "linux", Arch: "amd64"}},
		{Platform: Platform{OS: "darwin", Arch: "arm64"}},
	}}
	if got := many.Error(); got != "build failed for 2 platforms" {
		t.Errorf("Error() = %q", got)
	}
}

func TestRequiresCGO(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{
			name: "external linking",
			msg:  "warning: something\nandroid/amd64 requires external (cgo) linking, but cgo is not enabled: exit status 1",
			want: true,
		},
		{
			name: "default PIE binary",
			msg:  "default PIE binary requires external (cgo) linking, but cgo is not enabled: exit status 1",
			want: true,
		},
		{
			name: "requires cgo",
			msg:  "ios/arm64 requires cgo",
			want: true,
		},
		{
			name: "compile error",
			msg:  "./main.go:10:2: undefined: nope",
			want: false,
		},
		{
			name: "empty",
			msg:  "",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requiresCGO(tt.msg); got != tt.want {
				t.Errorf("requiresCGO(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestCheckOutputPath_Missing(t *testing.T) {
	if err := checkOutputPath(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatalf("checkOutputPath on missing path = %v, want nil", err)
	}
}

func TestCheckOutputPath_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkOutputPath(path); err != nil {
		t.Fatalf("checkOutputPath on regular file = %v, want nil", err)
	}
}

func TestCheckOutputPath_Directory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkOutputPath(path); err == nil {
		t.Fatal("expected error for directory at output path")
	}
}

func TestCheckOutputPath_Symlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevation on Windows")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := checkOutputPath(link); err == nil {
		t.Fatal("expected error for symlink at output path")
	}

	// The link target must be left untouched.
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "x" {
		t.Fatalf("symlink target modified: %q", data)
	}
}

func TestBuildPlatform_AllCases(t *testing.T) {
	cases := []struct {
		name   string // relative to testdata/
		isMain bool
	}{
		{name: "cmd/notest", isMain: true},
		{name: "cmd/withtest", isMain: true},
		{name: "lib/notest", isMain: false},
		{name: "lib/withtest", isMain: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srcDir := filepath.Join("testdata", tc.name)
			outDir := t.TempDir()
			appName := filepath.Base(tc.name)

			c := testConfig(t, srcDir, outDir)

			err := c.buildPlatform(t.Context(), Platform{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatalf("buildPlatform: %v", err)
			}

			binPath := filepath.Join(outDir, appName+"-linux-amd64")
			if _, err := os.Stat(binPath); os.IsNotExist(err) {
				t.Fatalf("expected output at %s", binPath)
			}
		})
	}
}

func TestBuildPlatform_Windows_AllCases(t *testing.T) {
	for _, name := range []string{"cmd/notest", "cmd/withtest"} {
		t.Run(name, func(t *testing.T) {
			srcDir := filepath.Join("testdata", name)
			outDir := t.TempDir()
			appName := filepath.Base(name)

			c := testConfig(t, srcDir, outDir)

			err := c.buildPlatform(t.Context(), Platform{OS: "windows", Arch: "amd64"})
			if err != nil {
				t.Fatalf("buildPlatform: %v", err)
			}

			binPath := filepath.Join(outDir, appName+"-windows-amd64.exe")
			if _, err := os.Stat(binPath); os.IsNotExist(err) {
				t.Fatalf("expected binary at %s", binPath)
			}
		})
	}
}

func TestBuildPlatform_CGORequiredIsNotAnError(t *testing.T) {
	srcDir := filepath.Join("testdata", "cmd", "notest")
	outDir := t.TempDir()

	c := testConfig(t, srcDir, outDir)

	err := c.buildPlatform(t.Context(), Platform{OS: "ios", Arch: "arm64"})
	if err == nil {
		t.Skip("toolchain can build ios/arm64 without cgo")
	}
	if !errors.Is(err, errCGORequired) {
		t.Fatalf("buildPlatform(ios/arm64) = %v, want errCGORequired", err)
	}

	entries := mustReadDir(t, outDir)
	if len(entries) != 0 {
		t.Errorf("output dir = %d entries, want empty after cgo skip", len(entries))
	}
}

func TestBuildPlatform_RespectsCancelledContext(t *testing.T) {
	srcDir := filepath.Join("testdata", "cmd", "notest")
	outDir := t.TempDir()

	c := testConfig(t, srcDir, outDir)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := c.buildPlatform(ctx, Platform{OS: "linux", Arch: "amd64"})
	if err == nil {
		t.Fatal("expected an error for a cancelled build")
	}

	entries := mustReadDir(t, outDir)
	if len(entries) != 0 {
		t.Errorf("output dir = %v, want empty: a cancelled build must not write a binary", entries)
	}
}

func TestBuildPlatform_NoLeftoverTempFiles(t *testing.T) {
	srcDir := filepath.Join("testdata", "cmd", "notest")
	outDir := t.TempDir()

	c := testConfig(t, srcDir, outDir)
	if err := c.buildPlatform(t.Context(), Platform{OS: "linux", Arch: "amd64"}); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary build artifact left behind: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("output dir = %v, want exactly one binary", mustReadDir(t, outDir))
	}
}

func TestBuildPlatform_RefusesSymlinkOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevation on Windows")
	}

	srcDir := filepath.Join("testdata", "cmd", "notest")
	outDir := t.TempDir()
	appName := filepath.Base(srcDir)

	victim := filepath.Join(outDir, "victim")
	if err := os.WriteFile(victim, []byte("do not touch"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(outDir, appName+"-linux-amd64")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}

	c := testConfig(t, srcDir, outDir)
	err := c.buildPlatform(t.Context(), Platform{OS: "linux", Arch: "amd64"})
	if err == nil {
		t.Fatal("expected error when output path is a symlink")
	}

	data, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "do not touch" {
		t.Fatalf("symlink target was overwritten: %q", data)
	}
}

func TestBuildAll_IncludesSuccess(t *testing.T) {
	srcDir := "testdata/cmd/withtest"
	outDir := t.TempDir()

	c, err := NewConfig(Options{SrcDir: srcDir, OutDir: outDir, Include: "linux/amd64 windows/amd64"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := c.buildAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if res.Platforms != 2 || res.OK != 2 || res.Skipped != 0 || res.Failed != 0 {
		t.Errorf("result = %+v, want 2 platforms all ok", res)
	}

	for _, p := range c.platformsFromToolchain(t) {
		ext := ""
		if p.OS == "windows" {
			ext = ".exe"
		}
		binPath := filepath.Join(outDir, "withtest"+"-"+p.OS+"-"+p.Arch+ext)
		if _, err := os.Stat(binPath); os.IsNotExist(err) {
			t.Fatalf("expected binary at %s", binPath)
		}
	}
}

func TestBuildAll_ReportsFailures(t *testing.T) {
	outDir := t.TempDir()

	// A source directory that cannot compile gives a genuine per-platform
	// failure, which must be reported as a *BuildError and counted.
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, "main.go"), []byte("package main\nfunc main() { nope() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := NewConfig(Options{SrcDir: broken, OutDir: outDir, Include: "linux/amd64 darwin/amd64", Workers: 2})
	if err != nil {
		t.Fatal(err)
	}

	res, buildErr := c.buildAll(t.Context())
	if buildErr == nil {
		t.Fatal("expected an error for a source directory that does not compile")
	}

	var be *BuildError
	if !errors.As(buildErr, &be) {
		t.Fatalf("error = %T (%v), want *BuildError", buildErr, buildErr)
	}
	if len(be.Platforms) != 2 {
		t.Errorf("BuildError.Platforms = %d, want 2", len(be.Platforms))
	}
	for _, pe := range be.Platforms {
		if pe.Err == nil {
			t.Errorf("PlatformError for %s has a nil Err", pe.Platform)
		}
	}

	if res.Failed != 2 || res.OK != 0 {
		t.Errorf("result = %+v, want 2 failures", res)
	}
}

func TestBuildAll_CGOPlatformsAreSkipped(t *testing.T) {
	c, err := NewConfig(Options{SrcDir: "testdata/cmd/notest", OutDir: t.TempDir(), Include: "linux/amd64 ios/arm64", Workers: 2})
	if err != nil {
		t.Fatal(err)
	}

	res, buildErr := c.buildAll(t.Context())
	if buildErr != nil {
		t.Fatalf("buildAll() = %v, want nil (ios should be skipped, not fatal)", buildErr)
	}
	if res.Skipped != 1 {
		t.Errorf("result.Skipped = %d, want 1", res.Skipped)
	}
	if res.OK != 1 || res.Failed != 0 {
		t.Errorf("result = %+v, want 1 ok and no failures", res)
	}
}

func TestBuildAll_CancelledBeforeStart(t *testing.T) {
	c, err := NewConfig(Options{SrcDir: "testdata/cmd/notest", OutDir: t.TempDir(), Include: "*/*", Workers: 4})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	res, buildErr := c.buildAll(ctx)
	if !errors.Is(buildErr, context.Canceled) {
		t.Fatalf("buildAll() error = %v, want context.Canceled", buildErr)
	}

	var be *BuildError
	if errors.As(buildErr, &be) {
		t.Error("cancellation must not be reported as a BuildError")
	}
	if res.Failed != 0 {
		t.Errorf("result.Failed = %d, want 0: killed builds are not failures", res.Failed)
	}
	if res.Platforms == 0 || res.OK+res.Skipped+res.Failed+res.Unstarted != res.Platforms {
		t.Errorf("result = %+v, do not want the platform counts to add up", res)
	}
}

func TestBuildAll_CancelledMidRun(t *testing.T) {
	outDir := t.TempDir()

	c, err := NewConfig(Options{SrcDir: "testdata/cmd/notest", OutDir: outDir, Include: "*/*", Workers: 2})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())

	// Cancel once the first build has landed, so the run is interrupted
	// mid-flight rather than up front.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			entries, err := os.ReadDir(outDir)
			if err == nil && len(entries) > 0 {
				cancel()
				return
			}
			if ctx.Err() != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	start := time.Now()
	res, buildErr := c.buildAll(ctx)
	<-done

	if !errors.Is(buildErr, context.Canceled) {
		t.Fatalf("buildAll() error = %v, want context.Canceled", buildErr)
	}
	if res.Failed != 0 {
		t.Errorf("result.Failed = %d, want 0: killed builds are not failures", res.Failed)
	}

	// A cancelled run must still leave no partial binaries or temp files.
	for _, name := range mustReadDir(t, outDir) {
		if strings.HasSuffix(name, ".tmp") {
			t.Errorf("temporary build artifact left behind after cancel: %s", name)
		}
	}

	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("buildAll took %s after cancellation, expected a prompt return", elapsed)
	}
}
