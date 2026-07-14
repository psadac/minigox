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
| `<path>`   | `.`      | Last positional argument; directory of the Go code/package to compile |
| `-out`     | `bin`    | Output directory for binaries                  |
| `-include` | `*/*`    | Space-separated platform patterns to include   |
| `-exclude` | `""`     | Space-separated platform patterns to exclude   |

Platform patterns use `filepath.Match` syntax (e.g. `linux/amd64`, `windows/*`,
`*/*`, `*/arm*`).

## How it works

1. Executes `go tool dist list -json` to enumerate all supported platforms.
2. Applies include/exclude filters.
3. Cross-compiles every remaining platform in parallel using a worker pool.
4. Each build runs in a sanitised environment (`CGO_ENABLED=0`, explicit
   `GOOS`/`GOARCH`, no leaked variables).

minigox is inspired by [gox](https://github.com/mitchellh/gox).

## License

Apache 2.0. See [LICENSE](LICENSE).
