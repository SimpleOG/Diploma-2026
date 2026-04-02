FROM golang:1.22-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

ENV GONOSUMDB=* GONOSUMCHECK=* GOFLAGS=-mod=mod

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -o /app/rooms-service ./cmd/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata wget

COPY --from=builder /app/rooms-service /rooms-service

EXPOSE 8082

ENTRYPOINT ["/rooms-service"]
