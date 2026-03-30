FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install dependencies for CGO (none needed here, but keep for completeness)
RUN apk add --no-cache git ca-certificates tzdata

ENV GONOSUMDB=* GONOSUMCHECK=* GOFLAGS=-mod=mod

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -o /app/auth-service ./cmd/

# Final minimal image
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /app/auth-service /auth-service

EXPOSE 8081

ENTRYPOINT ["/auth-service"]
