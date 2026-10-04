# minigox

A fast, parallel cross-compilation tool for Go applications.

minigox discovers every platform supported by your Go toolchain and compiles
your project for all of them in parallel, with configurable platform
filtering, safe build environments, and live progress reporting.

## Installation

```sh
go install github.com/psadac/minigox@latest
```

## Usage

```sh
minigox                    # build current directory for all platforms
minigox ./cmd/app          # build a specific directory
minigox -out ./dist        # specify output directory
minigox -include "linux/amd64 freebsd/* windows/*"
minigox -exclude "android/* */arm*"
```

### Flags

| Flag       | Default  | Description                                    |
|------------|----------|------------------------------------------------|
| `<path>`   | `.`      | Optional positional argument; directory of the Go code/package to compile |
| `-out`     | `bin`    | Output directory for binaries                  |
| `-include` | `*/*`    | Space-separated platform patterns to include   |
| `-exclude` | `""`     | Space-separated platform patterns to exclude   |
| `-workers` | auto    | Maximum number of concurrent builds           |

Platform patterns use `filepath.Match` syntax (e.g. `linux/amd64`, `windows/*`,
`*/*`, `*/arm*`). A malformed pattern is reported as an error rather than
silently matching nothing.

`-workers` defaults to `NumCPU-1`, with a floor of 2. Set it to `1` to build
serially.

## How it works

1. Executes `go tool dist list -json` to enumerate all supported platforms.
2. Applies include/exclude filters.
3. Cross-compiles every remaining platform in parallel using a worker pool.
4. Each build runs in a sanitised environment (`CGO_ENABLED=0`, explicit
   `GOOS`/`GOARCH`, no leaked variables), writing to a temporary file that is
   renamed into place so an interrupted build never leaves a truncated binary.

Platforms that cannot be built without cgo (`android/*`, `ios/*`) are reported
as `skip` rather than as errors, and do not affect the exit status. Genuine
compilation failures are reported as `error` and exit non-zero.

Binaries are named `<app>-<os>-<arch>`, where `<app>` is the base name of the
source directory. Two projects sharing a directory name will overwrite each
other if pointed at the same `-out` directory; use separate output directories
in that case.

### Build environment

Each build gets a sanitised environment. `PATH` and `HOME` are inherited,
`CGO_ENABLED=0` plus the target `GOOS`/`GOARCH` are set explicitly, and
`GOTOOLCHAIN=local` pins the toolchain. Variables that would change build
behaviour are dropped so they cannot be injected from the parent environment:

| Passed through         | Dropped                                     |
|------------------------|---------------------------------------------|
| `GOCACHE`, `GOMODCACHE` | `GOFLAGS`, `GOEXPERIMENT`, `GOPROXY`, `GOPATH` |

`GOCACHE` and `GOMODCACHE` only affect *where* the toolchain reads
already-fetched artifacts, so they are preserved to support projects using
non-default caches. Behaviour-changing variables are not.

Two consequences worth knowing:

- Builds are **not offline**. With `GOPROXY` unset the toolchain falls back to
  its default proxy for modules missing from the cache.
- If `go.mod` requires a newer Go than the one on `PATH`, the build fails with
  `go.mod requires go >= X (running go Y; GOTOOLCHAIN=local)` instead of
  silently downloading a different toolchain. Install a newer `go` on `PATH` to
  cross-compile such a project.

minigox is inspired by [gox](https://github.com/mitchellh/gox).

## License

Apache 2.0. See [LICENSE](LICENSE).
