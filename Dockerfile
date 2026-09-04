FROM golang:1.27-alpine AS build

ARG BINARY

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY gen ./gen
COPY internal ./internal
COPY nodes/web ./nodes/web

RUN case "$BINARY" in orbit-agent|orbit-core|orbit-web) ;; *) exit 2 ;; esac \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/orbit "./cmd/$BINARY"

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata

ENV TZ=Asia/Shanghai

WORKDIR /app

COPY --from=build /out/orbit /usr/local/bin/orbit

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/orbit"]
