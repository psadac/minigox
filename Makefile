BIN   := minigox
OUT   := bin

.PHONY: all build test vet clean release

all: build

build:
	go build -o $(BIN) .

test:
	go test -v -count=1 ./...

vet:
	go vet ./...

clean:
	rm -rf $(BIN) $(OUT)

release:
	go run . -out $(OUT) -include "linux/amd64 darwin/amd64 darwin/arm64 windows/amd64"
