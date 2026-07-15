ARG GO_IMAGE=golang:1.25-alpine
ARG ALPINE_IMAGE=alpine:3.22

# The defaults use public images, while CI or local builds can pass mirror images
# without changing the Dockerfile when outbound registry access is slow/restricted.
FROM ${GO_IMAGE} AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/admin ./cmd/admin

FROM ${ALPINE_IMAGE} AS runtime

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && \
    adduser -S -D -H -G app app

USER app
EXPOSE 8080

FROM runtime AS admin
COPY --from=builder /out/admin /usr/local/bin/admin
ENTRYPOINT ["/usr/local/bin/admin"]

FROM runtime AS server
COPY --from=builder /out/server /usr/local/bin/server

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/health/ready >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/server"]
