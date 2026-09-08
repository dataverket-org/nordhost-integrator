# Build on the native (host) platform and cross-compile - Go handles that
# natively via GOOS/GOARCH, avoiding QEMU emulation for the build itself
# (which is what --platform=$TARGETPLATFORM here would otherwise trigger).
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build_deps

RUN apk add --no-cache git

WORKDIR /workspace

COPY go.mod .
COPY go.sum .

RUN go mod download

FROM build_deps AS build

ARG TARGETOS
ARG TARGETARCH

COPY . .

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o webhook -ldflags '-w -extldflags "-static"' ./cmd/main

FROM alpine:3.22

# Stays root: the webhook binds to port 443 (--tls-cert-file/--tls-private-key-file),
# which requires CAP_NET_BIND_SERVICE to bind as non-root - not worth the
# extra securityContext complexity given this isn't under a runAsNonRoot
# policy anywhere it's deployed.
RUN apk add --no-cache ca-certificates

COPY --from=build /workspace/webhook /usr/local/bin/webhook

ENTRYPOINT ["webhook"]
