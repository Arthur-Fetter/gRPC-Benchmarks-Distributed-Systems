# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /servidor ./carrinho/

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.19

WORKDIR /app
COPY --from=builder /servidor .

# gRPC
EXPOSE 50051
# HTTP/JSON
EXPOSE 8080

# Carrinho Service — expõe gRPC :50051 e HTTP :8080
CMD ["./servidor"]
