package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
)

// EnvVars represents a collection of environment variables as key-value pairs.
type EnvVars map[string]string

// StringArray returns the variables as KEY=VALUE strings, sorted by key so that
// the result is deterministic.
func (ev EnvVars) StringArray() []string {
	keys := make([]string, 0, len(ev))
	for k := range ev {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	sa := make([]string, 0, len(ev))
	for _, k := range keys {
		sa = append(sa, k+"="+ev[k])
	}

	return sa
}

// getGoVars retrieves Go environment variables in JSON format, parses them, and updates the Config with relevant values.
// Keys the toolchain reports as empty are skipped, so that an empty value is
// never passed through to the build environment as `KEY=`.
func (c *Config) getGoVars(keys ...string) error {
	out, err := exec.Command(c.goCmd, "env", "-json").Output()
	if err != nil {
		return fmt.Errorf("failed running go env: %w", err)
	}

	var ev EnvVars
	err = json.Unmarshal(out, &ev)
	if err != nil {
		return fmt.Errorf("failed parsing go env output: %w", err)
	}

	c.goVars = make(EnvVars)
	for _, k := range keys {
		if ev[k] != "" {
			c.goVars[k] = ev[k]
		}
	}

	return nil
}

// safeEnv returns a minimal environment for the build command, passing through
// only essential variables and explicitly setting Go cross-compilation
// variables.
//
// Variables that change build *behaviour* are deliberately dropped so they
// cannot be injected through the parent environment: GOFLAGS (arbitrary
// compiler and linker flags), GOEXPERIMENT, GOPROXY, GONOSUMCHECK and
// friends. Cache *locations* (GOCACHE, GOMODCACHE) are passed through because
// they only control where the toolchain reads already-fetched artifacts, and
// dropping them would break any project using a non-default cache.
//
// Note that dropping GOPROXY does not make a build offline: with it unset the
// toolchain falls back to its default proxy for modules missing from the cache.
func (c *Config) safeEnv(p Platform) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + c.homeDir,
		"CGO_ENABLED=0",
		"GOOS=" + p.OS,
		"GOARCH=" + p.Arch,
		// Pin the toolchain, so that GOTOOLCHAIN in the parent environment
		// cannot make the build switch to and download a different one.
		"GOTOOLCHAIN=local",
	}

	env = append(env, c.goVars.StringArray()...)

	return env
}
