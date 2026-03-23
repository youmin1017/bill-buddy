# Build stage
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o bill-buddy .

# Run stage
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/bill-buddy .
RUN apk --no-cache add ca-certificates tzdata
VOLUME ["/app/data"]
CMD ["./bill-buddy"]
