package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// EnvVars represents a collection of environment variables as key-value pairs.
type EnvVars map[string]string

func (ev *EnvVars) StringArray() []string {
	sa := make([]string, 0, len(*ev))

	for k, v := range *ev {
		sa = append(sa, k+"="+v)
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

// Config holds the configuration for a cross-compilation run.
type Config struct {
	goCmd           string
	goVars          EnvVars
	homeDir         string
	srcDir          string
	outDir          string
	appName         string
	platformInclude string
	platformExclude string
	platforms       []Platform
	maxWorkers      int
	nbErrors        atomic.Uint32
}

// NewConfig builds a Config, validating directories and selecting platforms
// from include/exclude patterns.
func NewConfig(srcDir, outDir, platformInclude, platformExclude string) (*Config, error) {
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

	absSrcDir, err := filepath.Abs(srcDir)
	if err != nil {
		return nil, err
	}

	_, err = os.Stat(absSrcDir)
	if err != nil {
		return nil, fmt.Errorf("source directory %q: %w", absSrcDir, err)
	}

	absOutDir, err := filepath.Abs(outDir)
	if err != nil {
		return nil, err
	}

	c := &Config{
		goCmd:           goCmd,
		homeDir:         homeDir,
		srcDir:          absSrcDir,
		outDir:          absOutDir,
		appName:         filepath.Base(absSrcDir),
		platformInclude: platformInclude,
		platformExclude: platformExclude,
		maxWorkers:      max(2, runtime.NumCPU()-1),
		nbErrors:        atomic.Uint32{},
	}

	err = validatePatterns(platformInclude, platformExclude)
	if err != nil {
		return nil, err
	}

	err = c.getGoVars("GOCACHE", "GOVERSION")
	if err != nil {
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

func (c *Config) String() string {
	return fmt.Sprintf(`
  Source dir : %s
  Output dir : %s
  Platforms  : %d
  Workers    : %d
  Go vars    : %v
`, c.srcDir, c.outDir, len(c.platforms), c.maxWorkers, c.goVars.StringArray())
}

// buildAll cross-compiles for all configured platforms in parallel.
func (c *Config) buildAll() error {
	err := os.MkdirAll(c.outDir, 0o755)
	if err != nil {
		return fmt.Errorf("cannot create output directory: %w", err)
	}

	ps, err := c.listPlatforms()
	if err != nil {
		return err
	}

	c.platforms = c.filterPlatforms(ps)

	fmt.Printf(`
Configuration:
%s
Starting cross-compilation:

`, c.String())

	workersCh := make(chan struct{}, c.maxWorkers)
	var wg sync.WaitGroup

	for _, p := range c.platforms {
		wg.Add(1)
		go func(p Platform) {
			defer wg.Done()
			workersCh <- struct{}{}
			defer func() { <-workersCh }()

			err := c.buildPlatform(p)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  error - %-18s - %v\n", p.String(), err)
				c.nbErrors.Add(1)
			} else {
				fmt.Printf("  ok    - %-18s\n", p.String())
			}
		}(p)
	}

	wg.Wait()

	n := int(c.nbErrors.Load())
	fmt.Printf(`
Results:

  ok     : %d
  errors : %d

`, len(c.platforms)-n, n)

	if n > 0 {
		return fmt.Errorf("build finished with %d errors", n)
	}

	return nil
}

// getGoVars retrieves Go environment variables in JSON format, parses them, and updates the Config with relevant values.
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
		c.goVars[k] = ev[k]
	}

	return nil
}

func goEnvCache(goCmd string) (string, error) {
	out, err := exec.Command(goCmd, "env", "GOCACHE").Output()
	if err != nil {
		return "", fmt.Errorf("failed running go env GOCACHE: %w", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// safeEnv returns a minimal environment for the build command,
// passing through only essential variables and explicitly setting
// Go cross-compilation variables. This prevents injection attacks
// via environment variables such as GOFLAGS, GOPATH, GOPROXY, or
// GONOSUMCHECK.
func (c *Config) safeEnv(p Platform) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"CGO_ENABLED=0",
		"GOOS=" + p.OS,
		"GOARCH=" + p.Arch,
		"HOME=" + c.homeDir,
	}

	env = append(env, c.goVars.StringArray()...)

	return env
}

// buildPlatform compiles the app for p and writes the binary to the output dir.
func (c *Config) buildPlatform(p Platform) error {
	ext := ""
	if p.OS == "windows" {
		ext = ".exe"
	}

	outputPath := filepath.Join(c.outDir, fmt.Sprintf("%s-%s-%s%s", c.appName, p.OS, p.Arch, ext))

	cmd := exec.Command(c.goCmd, "build", "-o", outputPath, ".")
	cmd.Dir = c.srcDir
	cmd.Env = c.safeEnv(p)

	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimRight(string(out), "\n")
		if msg == "" {
			return err
		}
		return fmt.Errorf("%s: %w", msg, err)
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

	c, err := NewConfig(srcDir, *outputDir, *platformInclude, *platformExclude)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	err = c.buildAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
