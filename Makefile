BINARY  := blast
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test fuzz vet clean

build:
	go build -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/blast

test:
	go test -count=1 ./...

# The rule-language parser must survive arbitrary input.
fuzz:
	go test ./internal/rules/expr -run '^$$' -fuzz FuzzCompile -fuzztime 30s

vet:
	go vet ./...

clean:
	rm -rf bin dist
