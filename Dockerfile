FROM golang:1.25.5-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Build command
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o seedrlike ./cmd/

# Final stage
FROM gcr.io/distroless/static@sha256:cd64bec9cec257044ce3a8dd3620cf83b387920100332f2b041f19c4d2febf93

EXPOSE 3000

COPY --from=builder /app/seedrlike /seedrlike

CMD ["/seedrlike"]