.PHONY: build test vet check cover install clean release release-snapshot

BINARY := linux-time-machine
PKG    := ./cmd/linux-time-machine

build:
	go build -o bin/$(BINARY) $(PKG)

test:
	go test ./...

vet:
	go vet ./...

cover:
	go test -cover ./...

check: vet test build

install:
	go build -o $(HOME)/.local/bin/$(BINARY) $(PKG)

clean:
	rm -rf bin/ dist/

# Cut a release with goreleaser. Requires a tag (e.g. `git tag v0.2.0 && make release`).
release:
	goreleaser release --clean

# Build release artifacts locally without publishing — useful for verifying config.
release-snapshot:
	goreleaser release --snapshot --clean
