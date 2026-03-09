FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /agentveil ./cmd/proxy

FROM alpine:3.20
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 1001 agentveil
COPY --from=builder /agentveil /usr/local/bin/agentveil
USER agentveil
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD wget -q --spider http://localhost:8080/health || exit 1
ENTRYPOINT ["agentveil"]
