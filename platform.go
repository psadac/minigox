package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Platform is a target platform for cross-compilation.
type Platform struct {
	OS   string `json:"GOOS"`
	Arch string `json:"GOARCH"`
}

// String returns the platform in "OS/Arch" form.
func (p Platform) String() string {
	return p.OS + "/" + p.Arch
}

// listPlatforms returns the platforms supported by the Go toolchain.
func (c *Config) listPlatforms() ([]Platform, error) {
	out, err := exec.Command(c.goCmd, "tool", "dist", "list", "-json").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed running go tool dist: %w", err)
	}

	var platforms []Platform
	err = json.Unmarshal(out, &platforms)
	if err != nil {
		return nil, fmt.Errorf("failed parsing platform list: %w", err)
	}

	return platforms, nil
}

// filterPlatforms selects platforms matching any include pattern and not
// matching any exclude pattern. An empty include matches all platforms.
func (c *Config) filterPlatforms(platforms []Platform) []Platform {
	includes := strings.Fields(c.platformInclude)
	excludes := strings.Fields(c.platformExclude)

	var filtered []Platform
	for _, p := range platforms {
		if len(includes) > 0 && !matchAny(p, includes) {
			continue
		}

		if matchAny(p, excludes) {
			continue
		}

		filtered = append(filtered, p)
	}

	return filtered
}

// matchAny checks if the platform `p` matches any pattern in the `patterns` slice.
// Returns true if a match is found, false otherwise. Patterns use filepath.Match syntax.
func matchAny(p Platform, patterns []string) bool {
	for _, pat := range patterns {
		ok, err := filepath.Match(pat, p.String())
		if err == nil && ok {
			return true
		}
	}

	return false
}

// validatePatterns reports malformed include/exclude patterns instead of
// letting them silently match nothing.
func validatePatterns(include, exclude string) error {
	for _, set := range []struct {
		flag string
		raw  string
	}{
		{"include", include},
		{"exclude", exclude},
	} {
		for pat := range strings.FieldsSeq(set.raw) {
			if _, err := filepath.Match(pat, "os/arch"); err != nil {
				return fmt.Errorf("invalid -%s pattern %q: %w", set.flag, pat, err)
			}
		}
	}

	return nil
}
