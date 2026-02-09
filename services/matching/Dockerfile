FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /matching ./services/matching/cmd/matching

FROM alpine:latest
ARG GRPC_HEALTH_PROBE_VERSION=0.4.19
RUN apk --no-cache add ca-certificates curl \
  && curl -fsSL -o /usr/local/bin/grpc_health_probe \
     "https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-amd64" \
  && chmod +x /usr/local/bin/grpc_health_probe
COPY --from=builder /matching /matching
EXPOSE 9094 8080
CMD ["/matching"]
