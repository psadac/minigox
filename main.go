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

// goCmd is set at init time to a resolved absolute path, preventing PATH
// hijacking between the platform-list and build steps.
var goCmd string

func init() {
	var err error
	goCmd, err = exec.LookPath("go")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: go binary not found in PATH: %v\n", err)
		os.Exit(2)
	}
	goCmd, err = filepath.Abs(goCmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot resolve go binary path: %v\n", err)
		os.Exit(2)
	}
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
	srcDir     string
	outDir     string
	appName    string
	platforms  []Platform
	maxWorkers int
	nbErrors   atomic.Uint32
}

// NewConfig builds a Config, validating directories and selecting platforms
// from include/exclude patterns.
func NewConfig(srcDir, outDir, platformInclude, platformExclude string) (*Config, error) {
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

	err = os.MkdirAll(absOutDir, 0o755)
	if err != nil {
		return nil, fmt.Errorf("cannot create output directory: %w", err)
	}

	platforms, err := listPlatforms()
	if err != nil {
		return nil, err
	}

	return &Config{
		srcDir:     absSrcDir,
		outDir:     absOutDir,
		appName:    filepath.Base(absSrcDir),
		maxWorkers: max(2, runtime.NumCPU()-1),
		platforms:  filterPlatforms(platforms, platformInclude, platformExclude),
	}, nil
}

// listPlatforms returns the platforms supported by the Go toolchain.
func listPlatforms() ([]Platform, error) {
	out, err := exec.Command(goCmd, "tool", "dist", "list", "-json").CombinedOutput()
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
func filterPlatforms(platforms []Platform, include, exclude string) []Platform {
	includes := strings.Fields(include)
	excludes := strings.Fields(exclude)

	var filtered []Platform
	for _, p := range platforms {
		if len(includes) > 0 && !matchAny(includes, p) {
			continue
		}
		if matchAny(excludes, p) {
			continue
		}
		filtered = append(filtered, p)
	}

	return filtered
}

func matchAny(patterns []string, p Platform) bool {
	for _, pat := range patterns {
		ok, err := filepath.Match(pat, p.String())
		if err == nil && ok {
			return true
		}
	}
	return false
}

// buildAll cross-compiles for all configured platforms in parallel.
func (c *Config) buildAll() error {
	fmt.Printf(`
Configuration:
  Source dir : %s
  Output dir : %s
  Platforms  : %d
  Workers    : %d

Starting cross-compilation:
`, c.srcDir, c.outDir, len(c.platforms), c.maxWorkers)

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

// safeEnv returns a minimal environment for the build command,
// passing through only essential variables and explicitly setting
// Go cross-compilation variables. This prevents injection attacks
// via environment variables such as GOFLAGS, GOPATH, GOPROXY, or
// GONOSUMCHECK.
func safeEnv(goos, goarch string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"CGO_ENABLED=0",
		"GOOS=" + goos,
		"GOARCH=" + goarch,
	}
	// HOME is needed for Go module cache; prefer the real home dir
	// rather than trusting a potentially manipulated value.
	if home, err := os.UserHomeDir(); err == nil {
		env = append(env, "HOME="+home)
	}
	return env
}

// buildPlatform compiles the app for p and writes the binary to the output dir.
func (c *Config) buildPlatform(p Platform) error {
	ext := ""
	if p.OS == "windows" {
		ext = ".exe"
	}

	outputPath := filepath.Join(c.outDir, fmt.Sprintf("%s-%s-%s%s", c.appName, p.OS, p.Arch, ext))

	cmd := exec.Command(goCmd, "build", "-o", outputPath, ".")
	cmd.Dir = c.srcDir
	cmd.Env = safeEnv(p.OS, p.Arch)

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
  The last positional argument is used as the source directory (defaults to ".").

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

	srcDir := "."
	if flag.NArg() > 0 {
		srcDir = flag.Arg(flag.NArg() - 1)
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
