# Build stage
FROM golang:1.25.3-alpine AS builder

# Install git for fetching dependencies
RUN apk add --no-cache git

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with all optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -extldflags '-static'" \
    -a -tags netgo \
    -o linkserver .

# Final stage - using scratch (smallest possible)
FROM scratch

# Copy the binary
COPY --from=builder /build/linkserver /linkserver

# Copy templates
COPY --from=builder /build/templates /templates

# Copy SSL certificates for HTTPS requests
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

EXPOSE 8080

ENTRYPOINT ["/linkserver"]
CMD ["-c", "/config/config.yaml", "-a", "0.0.0.0", "-p", "8080"]
