FROM golang:1.24-alpine

WORKDIR /app

ENV GOTOOLCHAIN=auto

# Install git, curl, and build tools
RUN apk add --no-cache git build-base curl

# Install Air for live reload
RUN go install github.com/air-verse/air@v1.61.7

# Copy go.mod and go.sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build production binary
RUN CGO_ENABLED=0 go build -o /app/server .

# Expose server port
EXPOSE 8080

# Run compiled binary
CMD ["/app/server"]
