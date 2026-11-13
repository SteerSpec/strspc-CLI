.PHONY: build test cover lint fmt install-hooks clean strspc-lint vulncheck depcheck

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

# cover runs the tests the way CI does and fails below the coverage CONV-005 requires.
COVER_MIN ?= 95
cover:
	go test -race -coverprofile=coverage.out ./...
	scripts/coverage-gate.sh coverage.out $(COVER_MIN)

lint:
	golangci-lint run

# vulncheck and depcheck enforce the dependency time frames in SECURITY.md;
# the Security workflow runs them. GOVULNCHECK_VERSION and DEP_MAX_AGE_DAYS
# are the single source of the version and the update time frame.
GOVULNCHECK_VERSION ?= v1.9.0
vulncheck:
	go version
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

DEP_MAX_AGE_DAYS ?= 30
depcheck:
	scripts/dep-age-gate.sh $(DEP_MAX_AGE_DAYS)

fmt:
	golangci-lint fmt

install-hooks:
	cp scripts/commit-msg .git/hooks/commit-msg
	chmod +x .git/hooks/commit-msg

strspc-lint: build
	./strspc lint rules/

clean:
	rm -f strspc coverage.out
	rm -rf dist/
