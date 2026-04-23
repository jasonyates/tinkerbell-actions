# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS build
RUN apk add --no-cache git ca-certificates gcc linux-headers musl-dev
COPY . /src
WORKDIR /src/serial-console
RUN --mount=type=cache,sharing=locked,id=gomod,target=/go/pkg/mod/cache \
    --mount=type=cache,sharing=locked,id=goroot,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -a \
        -ldflags "-linkmode external -extldflags '-static' -s -w" \
        -o serial-console ./cmd

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /src/serial-console/serial-console /usr/bin/serial-console
ENTRYPOINT ["/usr/bin/serial-console"]
