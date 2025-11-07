FROM golang:1.25-alpine3.21 AS builder

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -ldflags="-s -w" -o rpcgate ./cmd/rpcgate

FROM alpine:3.22

WORKDIR /app
COPY --from=builder /app/rpcgate .

EXPOSE 8080

ENTRYPOINT ["./rpcgate"]
CMD ["--config", "/config.yaml"]