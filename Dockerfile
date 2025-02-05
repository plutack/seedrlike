FROM golang:1.23.5 as build
WORKDIR /app
COPY . .
RUN go build -o ./seedrlike ./cmd/
FROM scratch
COPY --from=build ./seedrlike ./seedrlike
EXPOSE 3000
CMD ["./seedrlike"]
