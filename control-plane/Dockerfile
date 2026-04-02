FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.* ./

# Download dependencies declared in go.mod
RUN go mod download

# Copy source code
COPY . .

# Upgrade vulnerable dependencies and ensure go.sum is up to date
# (must run after COPY to avoid go.sum being overwritten)
RUN go get golang.org/x/oauth2@latest golang.org/x/net@latest golang.org/x/crypto@latest && go mod tidy

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o control-plane .

# Final stage - pin to specific Alpine version to avoid busybox/zlib vulnerabilities
FROM alpine:3.21

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/control-plane .

# Copy UI files
COPY ui ./ui

EXPOSE 8080

ENTRYPOINT ["./control-plane"]
