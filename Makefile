BINARY  := blast
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test fuzz vet vuln lint audit clean

build:
	go build -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/blast

test:
	go test -count=1 ./...

# The rule-language parser must survive arbitrary input.
fuzz:
	go test ./internal/rules/expr -run '^$$' -fuzz FuzzCompile -fuzztime 30s

vet:
	go vet ./...

# Known vulnerabilities in blast's dependencies and in the Go it is built with.
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

lint: vet
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
	go run github.com/securego/gosec/v2/cmd/gosec@v2.30.0 -quiet ./...

# Everything the security notes in docs/security.md rely on.
audit: test vuln lint

clean:
	rm -rf bin dist
