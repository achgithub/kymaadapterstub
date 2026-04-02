FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod ./

RUN go mod download || true

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o soap-adapter .

# Pin to specific Alpine version to avoid busybox/zlib vulnerabilities
FROM alpine:3.21

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /app/soap-adapter .

EXPOSE 8080

ENTRYPOINT ["./soap-adapter"]
