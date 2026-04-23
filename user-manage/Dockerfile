# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS build
RUN apk add --no-cache git ca-certificates gcc linux-headers musl-dev
COPY . /src
WORKDIR /src/user-manage
RUN --mount=type=cache,sharing=locked,id=gomod,target=/go/pkg/mod/cache \
    --mount=type=cache,sharing=locked,id=goroot,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -a \
        -ldflags "-linkmode external -extldflags '-static' -s -w" \
        -o user-manage ./cmd

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /src/user-manage/user-manage /usr/bin/user-manage
ENTRYPOINT ["/usr/bin/user-manage"]
