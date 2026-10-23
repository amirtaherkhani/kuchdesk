# syntax=docker/dockerfile:1.7

FROM golang:1.26.2-alpine AS builder

WORKDIR /src/kuchdesk-agent

COPY kuchdesk-agent/go.mod kuchdesk-agent/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY kuchdesk-agent .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/kuchdesk-agent \
      ./cmd/kuchdesk-agent

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder --chown=65532:65532 /out/kuchdesk-agent /kuchdesk-agent

USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/kuchdesk-agent"]
