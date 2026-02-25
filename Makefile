MAKEFLAGS += --jobs all
GO := CGO_ENABLED=0 go

default: test build

test:
	${GO} vet ./...
	${GO} test ./...

.PHONY: web
web:
	cd web/admin && npm install && npm run build

.PHONY: build
build: web
	${GO} build -ldflags "\
		-X main.Version=$(shell git describe --tags --always) \
		-X main.BuildTime=$(shell date +%Y-%m-%d)" \
		-o bin/warden ./cmd/warden

run: build
	sudo pkill warden || true
	./bin/warden

clean:
	rm -f bin/warden
