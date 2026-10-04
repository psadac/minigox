package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
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

// Platform is a target platform for cross-compilation.
type Platform struct {
	OS   string `json:"GOOS"`
	Arch string `json:"GOARCH"`
}

// String returns the platform in "OS/Arch" form.
func (p Platform) String() string {
	return p.OS + "/" + p.Arch
}

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
		for _, pat := range strings.Fields(set.raw) {
			if _, err := filepath.Match(pat, "os/arch"); err != nil {
				return fmt.Errorf("invalid -%s pattern %q: %w", set.flag, pat, err)
			}
		}
	}

	return nil
}

// Result summarizes the outcome of a cross-compilation run.
type Result struct {
	Platforms int
	OK        int
	Skipped   int
	Failed    int
}

// PlatformError records why a single platform failed to build.
type PlatformError struct {
	Platform Platform
	Err      error
}

// BuildError reports the platforms that could not be built. The underlying
// errors have already been reported to stderr as they happened; this type lets
// callers inspect them programmatically.
type BuildError struct {
	Platforms []PlatformError
}

func (e *BuildError) Error() string {
	if len(e.Platforms) == 1 {
		return fmt.Sprintf("build failed for 1 platform: %s", e.Platforms[0].Platform)
	}

	return fmt.Sprintf("build failed for %d platforms", len(e.Platforms))
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

// buildAll cross-compiles for all matching platforms in parallel. It always
// returns a Result, even when some platforms fail; failures are additionally
// reported as a *BuildError.
func (c *Config) buildAll() (*Result, error) {
	if err := os.MkdirAll(c.outDir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create output directory: %w", err)
	}

	all, err := c.listPlatforms()
	if err != nil {
		return nil, err
	}

	platforms := c.filterPlatforms(all)

	fmt.Printf(`
Configuration:
%s
Starting cross-compilation:

`, c.summary(platforms))

	var (
		mu       sync.Mutex
		failures []PlatformError
		nOK      int
		nSkip    int
	)

	jobs := make(chan Platform)

	var wg sync.WaitGroup
	for range c.maxWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				err := c.buildPlatform(p)

				mu.Lock()
				switch {
				case err == nil:
					nOK++
					fmt.Printf("  ok    - %-18s\n", p)
				case errors.Is(err, errCGORequired):
					nSkip++
					fmt.Printf("  skip  - %-18s - requires cgo\n", p)
				default:
					failures = append(failures, PlatformError{Platform: p, Err: err})
					fmt.Fprintf(os.Stderr, "  error - %-18s - %v\n", p, err)
				}
				mu.Unlock()
			}
		}()
	}

	for _, p := range platforms {
		jobs <- p
	}
	close(jobs)
	wg.Wait()

	res := &Result{
		Platforms: len(platforms),
		OK:        nOK,
		Skipped:   nSkip,
		Failed:    len(failures),
	}

	fmt.Printf(`
Results:

  ok      : %d
  skipped : %d
  errors  : %d

`, res.OK, res.Skipped, res.Failed)

	if len(failures) > 0 {
		return res, &BuildError{Platforms: failures}
	}

	return res, nil
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

// errCGORequired marks a platform the Go toolchain cannot build with
// CGO_ENABLED=0, such as android/* and ios/*. These are reported as skipped
// rather than as build failures, since they are not fixable by the caller.
var errCGORequired = errors.New("platform requires cgo")

// requiresCGO reports whether a failed build was caused by the target platform
// needing cgo rather than by a genuine compilation error.
func requiresCGO(msg string) bool {
	return strings.Contains(msg, "requires external (cgo) linking") ||
		strings.Contains(msg, "requires cgo") ||
		strings.Contains(msg, "cgo is not enabled")
}

// checkOutputPath rejects an output path that already exists but is not a
// regular file, so a symlink, directory or device node in the output directory
// is never silently written through or replaced.
func checkOutputPath(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot inspect output path %q: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("output path %q exists and is not a regular file (mode %s)", path, fi.Mode())
	}

	return nil
}

// buildPlatform compiles the app for p and writes the binary to the output dir.
func (c *Config) buildPlatform(p Platform) error {
	ext := ""
	if p.OS == "windows" {
		ext = ".exe"
	}

	outputPath := filepath.Join(c.outDir, fmt.Sprintf("%s-%s-%s%s", c.appName, p.OS, p.Arch, ext))

	if err := checkOutputPath(outputPath); err != nil {
		return err
	}

	// Build into a temporary file in the output directory and rename it into
	// place, so an interrupted build cannot leave a truncated binary behind.
	tmp, err := os.CreateTemp(c.outDir, c.appName+"-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	cmd := exec.Command(c.goCmd, "build", "-o", tmpPath, ".")
	cmd.Dir = c.srcDir
	cmd.Env = c.safeEnv(p)

	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimRight(string(out), "\n")
		if requiresCGO(msg) {
			return errCGORequired
		}
		if msg == "" {
			return err
		}
		return fmt.Errorf("%s: %w", msg, err)
	}

	if err := os.Rename(tmpPath, outputPath); err != nil {
		return fmt.Errorf("cannot write %q: %w", outputPath, err)
	}

	return nil
}

const usageText = `Usage: %s [options] [path]

  %s cross-compiles Go applications in parallel for all operating systems and architectures.
  The optional positional argument is the source directory (defaults to ".").

Flags:
`

func main() {
	outputDir := flag.String("out", "bin", "Output directory for binaries")
	platformInclude := flag.String("include", "*/*", "Space-separated platform patterns to include (e.g. 'linux/amd64 windows/*')")
	platformExclude := flag.String("exclude", "", "Space-separated platform patterns to exclude (e.g. 'openbsd/* */arm')")
	workers := flag.Int("workers", 0, "Maximum number of concurrent builds (default: NumCPU-1, minimum 2)")

	flag.Usage = func() {
		name := filepath.Base(os.Args[0])
		fmt.Fprintf(os.Stderr, usageText, name, name)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n"+
			"  %s ./cmd/app\n"+
			"  %s -include \"linux/amd64 freebsd/* windows/*\"\n"+
			"  %s -exclude \"android/* */arm*\"\n", name, name, name)
	}

	flag.Parse()

	if flag.NArg() > 1 {
		fmt.Fprintf(os.Stderr, "error: expected at most one source directory, got %d\n\n", flag.NArg())
		flag.Usage()
		os.Exit(2)
	}

	srcDir := "."
	if flag.NArg() == 1 {
		srcDir = flag.Arg(0)
	}

	c, err := NewConfig(Options{
		SrcDir:  srcDir,
		OutDir:  *outputDir,
		Include: *platformInclude,
		Exclude: *platformExclude,
		Workers: *workers,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if _, err := c.buildAll(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
