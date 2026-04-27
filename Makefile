MAKEFLAGS += --jobs all
GO := CGO_ENABLED=0 go
BUN := bun
BINARY_NAME := warden
WINDOWS_SETUP_NAME := ${BINARY_NAME}-setup
LOCAL_GOEXE := $(shell go env GOEXE)
LOCAL_BINARY := bin/${BINARY_NAME}${LOCAL_GOEXE}
VERSION ?= $(shell git describe --tags --always)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
DIST_DIR ?= dist
RELEASE_PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64
WINDOWS_PACKAGE_FORMAT ?= setup
GO_LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"
GO_LDFLAGS_WINDOWS_GUI := -ldflags "-H=windowsgui -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"
UNAME_S := $(shell uname -s)

.PHONY: default test web build package run install clean

default: test build


test: web
	${GO} vet ./...
	${GO} test ./...

web:
	cd web/admin && ${BUN} install --frozen-lockfile && ${BUN} run build

build: web
	mkdir -p bin
	${GO} build ${GO_LDFLAGS} -o ${LOCAL_BINARY} ./cmd/warden

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
		if [ "$$goos" = "windows" ]; then \
			CGO_ENABLED=0 GOOS="$$goos" GOARCH="$$goarch" go build ${GO_LDFLAGS} -o "$$stage_dir/${BINARY_NAME}$$ext" ./cmd/warden; \
			if [ "${WINDOWS_PACKAGE_FORMAT}" = "setup" ]; then \
				CGO_ENABLED=0 GOOS="$$goos" GOARCH="$$goarch" go build ${GO_LDFLAGS_WINDOWS_GUI} -o "$$stage_dir/${WINDOWS_SETUP_NAME}$$ext" ./cmd/warden-setup; \
				${GO} run ./tools/windows-installer-packager -bootstrap "$$stage_dir/${WINDOWS_SETUP_NAME}$$ext" -runtime "$$stage_dir/${BINARY_NAME}$$ext" -output "${DIST_DIR}/$${asset_name}_setup.exe"; \
			elif [ "${WINDOWS_PACKAGE_FORMAT}" = "zip" ]; then \
				if command -v zip >/dev/null 2>&1; then \
					( cd "$$stage_dir" && zip -q "../$$asset_name.zip" "${BINARY_NAME}$$ext" ); \
				elif command -v python3 >/dev/null 2>&1; then \
					python3 -m zipfile -c "${DIST_DIR}/$$asset_name.zip" "$$stage_dir/${BINARY_NAME}$$ext"; \
				else \
					echo "zip or python3 is required to package windows artifacts"; \
					exit 1; \
				fi; \
			else \
				echo "unsupported WINDOWS_PACKAGE_FORMAT=${WINDOWS_PACKAGE_FORMAT}"; \
				exit 1; \
			fi; \
		else \
			CGO_ENABLED=0 GOOS="$$goos" GOARCH="$$goarch" go build ${GO_LDFLAGS} -o "$$stage_dir/${BINARY_NAME}$$ext" ./cmd/warden; \
			tar -C "$$stage_dir" -czf "${DIST_DIR}/$$asset_name.tar.gz" "${BINARY_NAME}$$ext"; \
		fi; \
		rm -rf "$$stage_dir"; \
	done

run: build
	${LOCAL_BINARY}

install: build
	@if [ "${UNAME_S}" = "Linux" ] && [ "$$(id -u)" -ne 0 ]; then \
		sudo ${LOCAL_BINARY} -i -y; \
	else \
		${LOCAL_BINARY} -i -y; \
	fi

clean:
	rm -rf bin dist
