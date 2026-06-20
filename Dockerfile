# Build stage
FROM golang:1.26.2 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Pre-copy/cache Go modules
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

# Copy source code
COPY cmd/ cmd/
COPY internal/ internal/

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build \
    -a -ldflags '-extldflags "-static"' \
    -o kivert cmd/manager/main.go

# Runtime stage
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/kivert .
USER 65532:65532

ENTRYPOINT ["/kivert"]
