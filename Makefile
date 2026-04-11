MAKEFLAGS += --jobs all
GO := CGO_ENABLED=0 go
NPM := npm
BINARY_NAME := warden
VERSION ?= $(shell git describe --tags --always)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
DIST_DIR ?= dist
RELEASE_PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64
GO_LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

default: test build

.PHONY: default test web build package run install clean
.NOTPARALLEL: default

test: web
	${GO} vet ./...
	${GO} test ./...

web:
	cd web/admin && ${NPM} ci && ${NPM} run build

build: web
	mkdir -p bin
	${GO} build ${GO_LDFLAGS} -o bin/${BINARY_NAME} ./cmd/warden

package: clean web
	mkdir -p ${DIST_DIR}
	@set -eu; \
	version="$(VERSION)"; \
	version="$${version#v}"; \
	for target in ${RELEASE_PLATFORMS}; do \
		goos="$${target%/*}"; \
		goarch="$${target#*/}"; \
		ext=""; \
		if [ "$$goos" = "windows" ]; then ext=".exe"; fi; \
		stage_dir="${DIST_DIR}/$${goos}_$${goarch}"; \
		asset_name="${BINARY_NAME}_$${version}_$${goos}_$${goarch}"; \
		mkdir -p "$$stage_dir"; \
		CGO_ENABLED=0 GOOS="$$goos" GOARCH="$$goarch" go build ${GO_LDFLAGS} -o "$$stage_dir/${BINARY_NAME}$$ext" ./cmd/warden; \
		tar -C "$$stage_dir" -czf "${DIST_DIR}/$$asset_name.tar.gz" "${BINARY_NAME}$$ext"; \
		rm -rf "$$stage_dir"; \
	done

run: build
	bin/${BINARY_NAME}

install: build
	sudo install -D -m755 bin/${BINARY_NAME} /usr/local/bin/${BINARY_NAME}
	sudo /usr/local/bin/warden -r

clean:
	rm -rf bin dist
