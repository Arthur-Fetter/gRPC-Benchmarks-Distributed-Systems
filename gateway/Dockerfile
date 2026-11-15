# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=darwin go build -o /gateway ./gateway/

# Runtime stage
FROM alpine:3.19

WORKDIR /app
COPY --from=builder /gateway .

# Gateway Service — escuta apenas em localhost
EXPOSE 3000

CMD ["./gateway"]
