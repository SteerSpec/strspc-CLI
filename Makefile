.PHONY: build test lint fmt install-hooks clean

build:
	go build -o strspc ./...

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
