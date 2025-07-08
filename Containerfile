# 1. Build stage
FROM golang:1.24.2-alpine AS build
WORKDIR /app

# Install necessary build tools
RUN apk add --no-cache gcc musl-dev

# Copy go.mod and go.sum to leverage Docker cache
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application code
COPY . .

# Build the application (CGO enabled for sqlite)
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o mop-backend-api .

# 2. Run stage
FROM alpine:latest
WORKDIR /app

RUN apk add --no-cache sqlite-libs

# Add a non-root user (UID 10001 is just an example)
RUN adduser -D -u 10001 appuser
USER appuser

# Copy binary; .env and db will come from the host
COPY --from=build /app/mop-backend-api /app/
COPY --from=build /app/static /app/static

# Expose HTTP port
EXPOSE 8080

# Entrypoint (do NOT copy data.db, let it be mounted)
CMD ["/app/mop-backend-api"]
