FROM --platform=$BUILDPLATFORM golang:1.22-bookworm AS builder

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    GOARM=${TARGETVARIANT#v} \
    go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o bank-connector ./cmd/connector

# Runtime: debian slim (glibc, ca-certs, tzdata — compatible with Raspberry Pi OS)
FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates tzdata && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app
LABEL org.opencontainers.image.licenses="AGPL-3.0-only"
COPY --from=builder /app/bank-connector .
COPY LICENSE LICENSING.md THIRD_PARTY_NOTICES.md ./
COPY THIRD_PARTY_LICENSES/ ./THIRD_PARTY_LICENSES/
COPY .third_party-source/ ./.third_party-source/

VOLUME ["/app/config", "/app/data"]
HEALTHCHECK --interval=5m --timeout=30s --start-period=30s --retries=2 \
    CMD ["./bank-connector", "doctor", "--config", "/app/config/config.yaml"]
ENTRYPOINT ["./bank-connector"]
CMD ["sync", "--daemon", "--config", "/app/config/config.yaml"]
