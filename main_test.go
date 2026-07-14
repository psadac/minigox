package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		got := matchAny(tt.patterns, tt.p)
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
			got := filterPlatforms(platforms, tt.include, tt.exclude)
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

func TestSafeEnv(t *testing.T) {
	env := safeEnv("linux", "arm64")

	checkEnv := func(key, val string) {
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

	for _, e := range env {
		if strings.HasPrefix(e, "GOFLAGS=") {
			t.Errorf("safeEnv() should not contain GOFLAGS, got %q", e)
		}
		if strings.HasPrefix(e, "GOPATH=") {
			t.Errorf("safeEnv() should not contain GOPATH, got %q", e)
		}
		if strings.HasPrefix(e, "GOPROXY=") {
			t.Errorf("safeEnv() should not contain GOPROXY, got %q", e)
		}
	}
}

func TestNewConfig(t *testing.T) {
	srcDir := "testdata/cmd/notest"
	outDir := filepath.Join(t.TempDir(), "bin")

	c, err := NewConfig(srcDir, outDir, "linux/amd64", "")
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
	if len(c.platforms) != 1 {
		t.Fatalf("expected 1 platform, got %d", len(c.platforms))
	}
	if c.platforms[0] != (Platform{"linux", "amd64"}) {
		t.Errorf("platform = %v, want linux/amd64", c.platforms[0])
	}
}

func TestNewConfig_InvalidSrcDir(t *testing.T) {
	_, err := NewConfig("/nonexistent/path", t.TempDir(), "*/*", "")
	if err == nil {
		t.Fatal("expected error for invalid source directory")
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

			c := &Config{
				srcDir:  srcDir,
				outDir:  outDir,
				appName: appName,
			}

			err := c.buildPlatform(Platform{OS: "linux", Arch: "amd64"})
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

			c := &Config{
				srcDir:  srcDir,
				outDir:  outDir,
				appName: appName,
			}

			err := c.buildPlatform(Platform{OS: "windows", Arch: "amd64"})
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

	c, err := NewConfig(srcDir, outDir, "linux/amd64 windows/amd64", "")
	if err != nil {
		t.Fatal(err)
	}

	err = c.buildAll()
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range c.platforms {
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
