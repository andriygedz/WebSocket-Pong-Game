# -----------------------------
# Stage 1: Build the Go Application
# -----------------------------
FROM golang:1.23-alpine AS builder

# Install git (required for fetching Go modules)
RUN apk update && apk add --no-cache git

# Set the working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum to leverage Docker cache
COPY go.mod go.sum ./

# Download Go modules
RUN go mod download

# Copy the entire project to the container
COPY . .

# Build the Go application
# -o websocket-game: output binary named "websocket-game"
# -trimpath: remove file system paths from the binary for security
# -ldflags="-s -w": strip debugging information to reduce binary size
RUN go build -o websocket-game -trimpath -ldflags="-s -w" .

# -----------------------------
# Stage 2: Create the Runtime Image
# -----------------------------
FROM alpine:latest

# Install necessary certificates for HTTPS (if required)
RUN apk update && apk add --no-cache ca-certificates

# Create a non-root user for running the application
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Set the working directory
WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/websocket-game .

# Copy the public directory from the builder stage
COPY --from=builder /app/public ./public

# Change ownership to the non-root user
RUN chown -R appuser:appgroup /app

# Switch to the non-root user
USER appuser

# Expose port 8080 (default port from your Go server)
EXPOSE 8080

# Command to run the executable
CMD ["./websocket-game"]