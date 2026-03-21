.PHONY: build test lint fmt install-hooks clean

VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BRANCH  := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -s -w \
	-X main.Version=$(VERSION) \
	-X main.GitCommit=$(COMMIT) \
	-X main.BuildTime=$(DATE) \
	-X main.GitBranch=$(BRANCH)

build:
	go build -ldflags "$(LDFLAGS)" -o strspc ./src

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	golangci-lint fmt

install-hooks:
	cp scripts/commit-msg .git/hooks/commit-msg
	chmod +x .git/hooks/commit-msg

clean:
	rm -f strspc coverage.out
	rm -rf dist/
