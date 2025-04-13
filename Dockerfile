# Build stage
FROM golang:1.24.2-alpine AS builder

# Install necessary build tools
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application with necessary flags for a fully static binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -o ghcsd cmd/server/main.go

# Prepare the root directory structure that will be copied to scratch
RUN mkdir -p rootfs/etc/ssl/certs \
    rootfs/root/.config/ghcsd \
    && cp /etc/ssl/certs/ca-certificates.crt rootfs/etc/ssl/certs/ \
    && cp /etc/passwd rootfs/etc/passwd \
    && cp /etc/group rootfs/etc/group

# Final stage
FROM scratch

# Copy the prepared root filesystem
COPY --from=builder /app/rootfs /

# Copy the binary
COPY --from=builder /app/ghcsd /ghcsd

# Expose the port
EXPOSE 8080

# Set environment variables
ENV DEBUG=0

# Run the binary
ENTRYPOINT ["/ghcsd"]