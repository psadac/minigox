BIN   := minigox
OUT   := bin

# Version metadata injected into the binary. Overridable on the command line,
# e.g. `make build VERSION=v1.2.3`.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG     := main
LDFLAGS := -s -w \
	-X '$(PKG).version=$(VERSION)' \
	-X '$(PKG).commit=$(COMMIT)' \
	-X '$(PKG).date=$(DATE)'

.PHONY: all build test vet clean version

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .

version:
	@go run -ldflags "$(LDFLAGS)" . -version

test:
	go test -v -count=1 ./...

vet:
	go vet ./...

clean:
	rm -rf $(BIN) $(OUT)
