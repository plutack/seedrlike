FROM golang:1.25.5-alpine AS builder

# bypassing goproxy since google is returning 403 (GOPROXY=direct)
RUN apk add --no-cache git

# Create the folder and a placeholder file so Docker detects it
RUN mkdir -p /home/plutack/Downloads/seedrlike && \
    touch /home/plutack/Downloads/seedrlike/.keep

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Build command
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o seedrlike ./cmd/

# Final stage
FROM ghcr.io/distroless/static

EXPOSE 3000

# Copy the folder structure from builder
COPY --from=builder /home/plutack/Downloads/seedrlike /home/plutack/Downloads/seedrlike
COPY --from=builder /app/seedrlike /seedrlike

CMD ["/seedrlike"]