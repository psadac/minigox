package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Options describes a cross-compilation run as requested by the user.
type Options struct {
	SrcDir  string
	OutDir  string
	Include string
	Exclude string
	// Workers caps the number of concurrent builds. Zero means auto-detect.
	Workers int
}

// Config holds the validated configuration for a cross-compilation run. It is
// immutable once returned by NewConfig; per-run state is reported by Result.
type Config struct {
	goCmd           string
	goVars          EnvVars
	homeDir         string
	srcDir          string
	outDir          string
	appName         string
	platformInclude string
	platformExclude string
	maxWorkers      int
}

// NewConfig validates opts and returns the configuration for a
// cross-compilation run.
func NewConfig(opts Options) (*Config, error) {
	if opts.Workers < 0 {
		return nil, fmt.Errorf("-workers must be positive, got %d", opts.Workers)
	}

	if err := validatePatterns(opts.Include, opts.Exclude); err != nil {
		return nil, err
	}

	goCmd, err := exec.LookPath("go")
	if err != nil {
		return nil, fmt.Errorf("go binary not found in PATH: %w", err)
	}
	goCmd, err = filepath.Abs(goCmd)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve go binary path: %w", err)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	absSrcDir, err := filepath.Abs(opts.SrcDir)
	if err != nil {
		return nil, err
	}

	_, err = os.Stat(absSrcDir)
	if err != nil {
		return nil, fmt.Errorf("source directory %q: %w", absSrcDir, err)
	}

	absOutDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return nil, err
	}

	maxWorkers := opts.Workers
	if maxWorkers == 0 {
		maxWorkers = max(2, runtime.NumCPU()-1)
	}

	c := &Config{
		goCmd:           goCmd,
		homeDir:         homeDir,
		srcDir:          absSrcDir,
		outDir:          absOutDir,
		appName:         filepath.Base(absSrcDir),
		platformInclude: opts.Include,
		platformExclude: opts.Exclude,
		maxWorkers:      maxWorkers,
	}

	if err := c.getGoVars("GOCACHE", "GOMODCACHE"); err != nil {
		return nil, fmt.Errorf("failed to determine go env variables: %w", err)
	}

	return c, nil
}

// summary describes the effective configuration for a run.
func (c *Config) summary(platforms []Platform) string {
	return fmt.Sprintf(`
  Source dir : %s
  Output dir : %s
  Platforms  : %d
  Workers    : %d
  Go vars    : %v
`, c.srcDir, c.outDir, len(platforms), c.maxWorkers, c.goVars.StringArray())
}
