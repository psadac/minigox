package main

import (
	"slices"
	"strings"
	"testing"
)

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

func TestSafeEnv(t *testing.T) {
	c := &Config{
		goVars:  EnvVars{"GOCACHE": "testcache", "GOMODCACHE": "testmodcache"},
		homeDir: "/home/test",
	}
	env := c.safeEnv(Platform{OS: "linux", Arch: "arm64"})

	checkEnv := func(key, val string) {
		t.Helper()
		found := slices.Contains(env, key+"="+val)
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
