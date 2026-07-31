# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26.5
ARG ALPINE_VERSION=3.23
ARG VERSION=dev

FROM golang:${GO_VERSION}-alpine AS builder

ARG VERSION
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

# 项目使用纯 Go SQLite 驱动，因此关闭 CGO 后仍可构建静态 Linux 二进制，
# Alpine 运行镜像无需额外安装 libc。API 版本通过 ldflags 注入，供启动日志
# 与发布验收追踪镜像来源。
RUN mkdir -p /out \
    && CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -buildvcs=false \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o /out/api \
        ./cmd/api \
    && CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -buildvcs=false \
        -ldflags="-s -w" \
        -o /out/log-producer \
        ./cmd/log-producer

FROM alpine:${ALPINE_VERSION} AS runtime

ARG APP_UID=1000
ARG APP_GID=1000

# UID/GID 可与宿主机绑定挂载目录对齐；运行阶段始终使用非 root 用户，
# 限制应用进程对容器文件系统的权限。
RUN addgroup -S -g "${APP_GID}" app \
    && adduser -S -D -H -u "${APP_UID}" -G app app \
    && mkdir -p /app/data /logs \
    && chown -R app:app /app /logs

WORKDIR /app
USER app

FROM runtime AS api

COPY --from=builder --chown=app:app /out/api /usr/local/bin/api

EXPOSE 8080 8081

ENTRYPOINT ["/usr/local/bin/api"]

FROM runtime AS log-producer

COPY --from=builder --chown=app:app /out/log-producer /usr/local/bin/log-producer

ENTRYPOINT ["/usr/local/bin/log-producer"]
