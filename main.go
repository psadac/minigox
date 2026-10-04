package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

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

	// Stop on Ctrl-C, killing in-flight builds instead of leaving them to run
	// to completion.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if _, err := c.buildAll(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		if errors.Is(err, context.Canceled) {
			// Conventional exit status for a process stopped by SIGINT.
			os.Exit(130)
		}
		os.Exit(1)
	}
}
