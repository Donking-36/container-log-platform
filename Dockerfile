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
