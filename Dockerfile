# Build stage
FROM golang:1.24-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o ippool .

# Runtime stage
FROM alpine:3.19
RUN apk add --no-cache ca-certificates sqlite-libs tzdata
WORKDIR /app
COPY --from=builder /app/ippool .
RUN mkdir -p /data
VOLUME ["/data"]
EXPOSE 8080
ENV PORT=0.0.0.0:8080
ENV DB_PATH=/data/ippool.db
ENTRYPOINT ["./ippool"]
