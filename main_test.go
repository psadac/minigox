package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPlatformString(t *testing.T) {
	p := Platform{OS: "linux", Arch: "amd64"}
	if got := p.String(); got != "linux/amd64" {
		t.Errorf("String() = %q, want %q", got, "linux/amd64")
	}
}

func TestMatchAny(t *testing.T) {
	tests := []struct {
		patterns []string
		p        Platform
		want     bool
	}{
		{[]string{"linux/amd64"}, Platform{"linux", "amd64"}, true},
		{[]string{"linux/*"}, Platform{"linux", "arm64"}, true},
		{[]string{"*/arm64"}, Platform{"linux", "arm64"}, true},
		{[]string{"*/*"}, Platform{"freebsd", "riscv64"}, true},
		{[]string{"windows/amd64"}, Platform{"linux", "amd64"}, false},
		{[]string{"linux/arm"}, Platform{"linux", "arm64"}, false},
		{[]string{"linux/amd64", "darwin/*"}, Platform{"darwin", "arm64"}, true},
		{nil, Platform{"linux", "amd64"}, false},
	}
	for _, tt := range tests {
		got := matchAny(tt.p, tt.patterns)
		if got != tt.want {
			t.Errorf("matchAny(%v, %v) = %v, want %v", tt.patterns, tt.p, got, tt.want)
		}
	}
}

func TestFilterPlatforms(t *testing.T) {
	platforms := []Platform{
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"darwin", "amd64"},
		{"darwin", "arm64"},
		{"windows", "amd64"},
		{"freebsd", "amd64"},
	}

	tests := []struct {
		name    string
		include string
		exclude string
		want    []Platform
	}{
		{
			name:    "all platforms",
			include: "",
			exclude: "",
			want:    platforms,
		},
		{
			name:    "include linux only",
			include: "linux/*",
			exclude: "",
			want:    []Platform{{"linux", "amd64"}, {"linux", "arm64"}},
		},
		{
			name:    "exclude arm",
			include: "",
			exclude: "*/arm64",
			want:    []Platform{{"linux", "amd64"}, {"darwin", "amd64"}, {"windows", "amd64"}, {"freebsd", "amd64"}},
		},
		{
			name:    "include and exclude",
			include: "linux/* darwin/*",
			exclude: "*/arm64",
			want:    []Platform{{"linux", "amd64"}, {"darwin", "amd64"}},
		},
		{
			name:    "no match",
			include: "nonexistent/*",
			exclude: "",
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{
				platformInclude: tt.include,
				platformExclude: tt.exclude,
			}
			got := c.filterPlatforms(platforms)
			if len(got) != len(tt.want) {
				t.Fatalf("filterPlatforms() = %v (%d), want %v (%d)", got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("filterPlatforms() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

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

func TestSafeEnv(t *testing.T) {
	c := &Config{
		goVars:  EnvVars{"GOCACHE": "testcache", "GOMODCACHE": "testmodcache"},
		homeDir: "/home/test",
	}
	env := c.safeEnv(Platform{OS: "linux", Arch: "arm64"})

	checkEnv := func(key, val string) {
		t.Helper()
		found := false
		for _, e := range env {
			if e == key+"="+val {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("safeEnv() missing %s=%s", key, val)
		}
	}

	checkEnv("GOOS", "linux")
	checkEnv("GOARCH", "arm64")
	checkEnv("CGO_ENABLED", "0")
	checkEnv("HOME", "/home/test")
	checkEnv("GOCACHE", "testcache")
	checkEnv("GOMODCACHE", "testmodcache")
	checkEnv("GOTOOLCHAIN", "local")

	for _, key := range []string{"GOFLAGS", "GOPATH", "GOPROXY", "GONOSUMCHECK", "GOEXPERIMENT"} {
		for _, e := range env {
			if strings.HasPrefix(e, key+"=") {
				t.Errorf("safeEnv() should not contain %s, got %q", key, e)
			}
		}
	}

	// The toolchain must not be able to switch toolchains via the environment.
	var toolchain string
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "GOTOOLCHAIN="); ok {
			toolchain = v
		}
	}
	if toolchain != "local" {
		t.Errorf("GOTOOLCHAIN = %q, want %q", toolchain, "local")
	}
}

func TestSafeEnv_IgnoresEmptyGoVars(t *testing.T) {
	c := &Config{
		goVars:  EnvVars{},
		homeDir: "/home/test",
	}
	for _, e := range c.safeEnv(Platform{OS: "linux", Arch: "amd64"}) {
		if strings.HasSuffix(e, "=") {
			t.Errorf("safeEnv() contains empty value %q", e)
		}
	}
}

func TestGetGoVars_SkipsEmptyKeys(t *testing.T) {
	c := testConfig(t, "testdata/cmd/notest", t.TempDir())
	c.goVars = nil

	if err := c.getGoVars("GOCACHE", "GOMODCACHE"); err != nil {
		t.Fatal(err)
	}

	if c.goVars["GOCACHE"] == "" {
		t.Error("expected a non-empty GOCACHE")
	}
	for k, v := range c.goVars {
		if v == "" {
			t.Errorf("goVars[%q] is empty, want the key to be omitted", k)
		}
	}
	if _, ok := c.goVars["GOVERSION"]; ok {
		t.Error("goVars should not contain GOVERSION")
	}
}

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

func TestValidatePatterns(t *testing.T) {
	tests := []struct {
		name    string
		include string
		exclude string
		wantErr bool
	}{
		{name: "valid patterns", include: "linux/amd64 windows/*", exclude: "*/arm"},
		{name: "empty patterns", include: "", exclude: ""},
		{name: "unterminated class in include", include: "linux/[amd64", wantErr: true},
		{name: "unterminated class in exclude", exclude: "[", wantErr: true},
		{name: "valid alongside invalid", include: "linux/amd64 bad[", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePatterns(tt.include, tt.exclude)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePatterns(%q, %q) error = %v, wantErr %v", tt.include, tt.exclude, err, tt.wantErr)
			}
		})
	}
}

func TestNewConfig_InvalidPattern(t *testing.T) {
	_, err := NewConfig(Options{SrcDir: "testdata/cmd/notest", OutDir: t.TempDir(), Include: "linux/[amd64"})
	if err == nil {
		t.Fatal("expected error for malformed include pattern")
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

	for _, p := range c.filterPlatforms(c.platformsFromToolchain(t)) {
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

func TestEnvVars_StringArrayIsSorted(t *testing.T) {
	ev := EnvVars{"GOMODCACHE": "/mod", "GOCACHE": "/cache", "GOFLAGS": ""}

	want := []string{"GOCACHE=/cache", "GOFLAGS=", "GOMODCACHE=/mod"}
	for range 20 {
		got := ev.StringArray()
		if !slices.Equal(got, want) {
			t.Fatalf("StringArray() = %v, want %v", got, want)
		}
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
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("output dir = %v, want exactly one binary", names)
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

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("output dir = %d entries, want empty after cgo skip", len(entries))
	}
}
