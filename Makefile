SHELL := /bin/bash

VERSION ?= 0.4.0
BINARY := conductor
PKG := ./cmd/conductor
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all print-version fmt fmt-check test vet race test-installer build dist checksums package verify-package app release clean

all: build

print-version:
	@printf '%s\n' "$(VERSION)"

fmt:
	gofmt -w $$(find . -name '*.go' -type f)

fmt-check:
	@test -z "$$(gofmt -l .)" || { echo "Run make fmt on:"; gofmt -l .; exit 1; }

test:
	go test ./...

vet:
	go vet ./...

race:
	go test -race ./...

test-installer:
	sh scripts/test-semver.sh
	sh scripts/test-install-version.sh
	@if [ "$$(uname -s)" = "Darwin" ]; then sh scripts/test-runtime-json.sh; fi

build:
	mkdir -p build
	go build -trimpath -ldflags "$(LDFLAGS)" -o build/$(BINARY) $(PKG)

dist:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64 $(PKG)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-amd64 $(PKG)

checksums: dist
	cd dist && shasum -a 256 $(BINARY)-darwin-arm64 $(BINARY)-darwin-amd64 > CHECKSUMS.txt

package: checksums
	VERSION=$(VERSION) ./scripts/package.sh

verify-package: package
	cd dist && shasum -a 256 -c CHECKSUMS.txt
	cd release && shasum -a 256 -c conductor-v$(VERSION).zip.sha256
	@set -eu; \
	archive="release/conductor-v$(VERSION).zip"; \
	prefix="conductor-v$(VERSION)"; \
	for path in \
		"$$prefix/scripts/install.sh" \
		"$$prefix/dist/conductor-darwin-arm64" \
		"$$prefix/dist/conductor-darwin-amd64" \
		"$$prefix/dist/CHECKSUMS.txt"; do \
		unzip -Z1 "$$archive" | grep -Fqx "$$path" || { \
			echo "Missing $$path from $$archive" >&2; \
			exit 1; \
		}; \
	done

app:
	VERSION=$(VERSION) ./scripts/build-macos-app.sh

release: fmt-check test vet race test-installer verify-package

clean:
	rm -rf build dist release
