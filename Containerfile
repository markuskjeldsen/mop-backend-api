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

# Build the application
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o mop-backend-api .

# 2. Run stage
FROM alpine:latest
WORKDIR /app

# Install sqlite3 if your binary needs the CLI (optional)
RUN apk add --no-cache sqlite-libs

# Copy compiled binary and other necessary files
COPY --from=build /app/mop-backend-api /app/
COPY .env /app/
COPY data.db /app/

# Expose port (default Gin port, change if needed)
EXPOSE 8080

# Set environment (optional)
ENV GIN_MODE=release

# Command to run your application
CMD ["./mop-backend-api"]
