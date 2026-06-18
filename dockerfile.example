FROM golang:1.25.5-alpine AS builder

# bypassing goproxy since google is returning 403 (GOPROXY=direct)
RUN apk add --no-cache git

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Build command
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o seedrlike ./cmd/

# Final stage
FROM ghcr.io/distroless/static

USER 0:0

EXPOSE 3000

COPY --from=builder /app/seedrlike /seedrlike

CMD ["/seedrlike"]