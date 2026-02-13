.PHONY: all build clean test coverage lint fmt vet run

# 项目名称
PROJECT := warden

# 版本信息
VERSION := 0.1.0
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")

# Go 环境变量
GO := go
GOPATH := $(shell $(GO) env GOPATH)
GOBIN := $(GOPATH)/bin

# 主要目标
all: fmt vet test build

# 构建二进制文件
build:
	@mkdir -p bin
	$(GO) build -v -o bin/$(PROJECT) \
		-ldflags "-X 'main.Version=$(VERSION)' \
				  -X 'main.BuildTime=$(BUILD_TIME)' \
				  -X 'main.GitHash=$(GIT_HASH)' \
				  -X 'main.GitBranch=$(GIT_BRANCH)'" \
		./cmd/$(PROJECT)

# 清理操作
clean:
	@rm -f bin/$(PROJECT)
	@rmdir bin 2>/dev/null || true
	@$(GO) clean -v ./...

# 运行项目
run: build
	./bin/$(PROJECT)

# 运行测试
test:
	$(GO) test -v ./internal/gateway

# 运行全部测试
test-all:
	$(GO) test -v ./...

# 测试覆盖率
coverage:
	@mkdir -p coverage
	$(GO) test -v ./internal/gateway -coverprofile=coverage/gateway.out
	$(GO) tool cover -func=coverage/gateway.out

# 查看 HTML 格式的测试覆盖率
coverage-html: coverage
	$(GO) tool cover -html=coverage/gateway.out

# 代码格式化
fmt:
	$(GO) fmt ./...

# 代码审查
lint:
	@which golangci-lint >/dev/null || $(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	golangci-lint run ./...

# 静态检查
vet:
	$(GO) vet -v ./...

# 安装依赖
deps:
	$(GO) mod download
	$(GO) mod tidy

# 生成文档
doc:
	$(GO) doc ./internal/gateway > docs/internal-gateway.txt
	$(GO) doc ./config > docs/config.txt

# 检查 Go 模块
verify:
	$(GO) mod verify

# 运行 benchmarks
bench:
	$(GO) test -v ./internal/gateway -bench=. -benchmem

# Docker 相关目标
docker-build:
	docker build -t $(PROJECT):$(VERSION) .

docker-run:
	docker run -p 8080:8080 --env-file .env $(PROJECT):$(VERSION)

# 帮助信息
help:
	@echo "Available targets:"
	@echo "  build        - 构建项目"
	@echo "  clean        - 清理构建产物"
	@echo "  run          - 运行项目"
	@echo "  test         - 运行内部网关测试"
	@echo "  test-all     - 运行全部测试"
	@echo "  coverage     - 测试覆盖率"
	@echo "  coverage-html- HTML格式测试覆盖率"
	@echo "  fmt          - 代码格式化"
	@echo "  lint         - 代码审查"
	@echo "  vet          - 静态检查"
	@echo "  deps         - 下载依赖"
	@echo "  doc          - 生成文档"
	@echo "  verify       - 检查模块"
	@echo "  bench        - 运行基准测试"
	@echo "  docker-build - 构建 Docker 镜像"
	@echo "  docker-run   - 运行 Docker 容器"
	@echo "  help         - 显示帮助信息"
