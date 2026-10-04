package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Result summarizes the outcome of a cross-compilation run.
type Result struct {
	Platforms int
	OK        int
	Skipped   int
	Failed    int
	// Unstarted counts platforms that were never attempted because the run was
	// cancelled first.
	Unstarted int
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
// Cancelling ctx kills the compiler and leaves no output behind.
func (c *Config) buildPlatform(ctx context.Context, p Platform) error {
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

	cmd := exec.CommandContext(ctx, c.goCmd, "build", "-o", tmpPath, ".")
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

// runState accumulates the outcome of a run. Its fields are guarded by mu, which
// also serialises reporting so concurrent builds cannot interleave output.
type runState struct {
	mu       sync.Mutex
	ok       int
	skipped  int
	failures []PlatformError
}

// record reports the outcome of building one platform.
func (s *runState) record(ctx context.Context, err error, p Platform) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case err == nil:
		s.ok++
		fmt.Printf("  ok    - %-18s\n", p)
	case errors.Is(err, errCGORequired):
		s.skipped++
		fmt.Printf("  skip  - %-18s - requires cgo\n", p)
	case ctx.Err() != nil:
		// The build was killed by cancellation, not by a genuine failure.
	default:
		s.failures = append(s.failures, PlatformError{Platform: p, Err: err})
		fmt.Fprintf(os.Stderr, "  error - %-18s - %v\n", p, err)
	}
}

// buildAll cross-compiles for all matching platforms in parallel. It always
// returns a Result, even when some platforms fail; failures are additionally
// reported as a *BuildError.
//
// Cancelling ctx stops the run: in-flight builds are killed, no further
// platforms are started, and a wrapped context error is returned. Builds
// killed by cancellation are not counted as failures.
func (c *Config) buildAll(ctx context.Context) (*Result, error) {
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
		state runState
		jobs  = make(chan Platform)
		wg    sync.WaitGroup
	)

	for range c.maxWorkers {
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case p, ok := <-jobs:
					if !ok {
						return
					}
					state.record(ctx, c.buildPlatform(ctx, p), p)
				}
			}
		})
	}

	var started int
feed:
	for _, p := range platforms {
		select {
		case <-ctx.Done():
			break feed
		case jobs <- p:
			started++
		}
	}
	close(jobs)
	wg.Wait()

	res := &Result{
		Platforms: len(platforms),
		OK:        state.ok,
		Skipped:   state.skipped,
		Failed:    len(state.failures),
		Unstarted: len(platforms) - started,
	}

	fmt.Printf(`
Results:

  ok      : %d
  skipped : %d
  errors  : %d

`, res.OK, res.Skipped, res.Failed)

	if err := ctx.Err(); err != nil {
		if res.Unstarted > 0 {
			fmt.Printf("  stopped : %d platform(s) not started\n", res.Unstarted)
		}
		return res, fmt.Errorf("build interrupted: %w", err)
	}

	if len(state.failures) > 0 {
		return res, &BuildError{Platforms: state.failures}
	}

	return res, nil
}
