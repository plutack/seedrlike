FROM golang:1.25.5-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Build command
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o seedrlike ./cmd/

# Final stage
FROM gcr.io/distroless/static

EXPOSE 3000

COPY --from=builder /app/seedrlike /seedrlike

CMD ["/seedrlike"]