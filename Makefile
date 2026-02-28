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
	bin/warden

install: build
	sudo install -D -m755 bin/warden /usr/local/bin/warden
	sudo /usr/local/bin/warden -r

clean:
	rm -f bin/warden
