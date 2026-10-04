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

Platform patterns use `filepath.Match` syntax (e.g. `linux/amd64`, `windows/*`,
`*/*`, `*/arm*`). A malformed pattern is reported as an error rather than
silently matching nothing.

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

minigox is inspired by [gox](https://github.com/mitchellh/gox).

## License

Apache 2.0. See [LICENSE](LICENSE).
