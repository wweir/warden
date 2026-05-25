# ---- Stage 1: Go build ----
FROM golang:1.26-alpine AS builder

# Use China Go module proxy for faster downloads in mainland China
ENV GOPROXY=https://goproxy.cn,direct

ARG VERSION=dev
ARG BUILD_TIME=1970-01-01T00:00:00Z

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" \
    -o /warden ./cmd/warden

# ---- Stage 2: Minimal runtime ----
FROM alpine:3.21

# Use China APK mirror for faster downloads in mainland China
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H warden

COPY --from=builder /warden /usr/local/bin/warden
COPY deploy/warden.docker.yaml /etc/warden.docker.yaml

USER warden
EXPOSE 9832

ENTRYPOINT ["warden"]
CMD ["-c", "/etc/warden.docker.yaml"]
