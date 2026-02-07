# Build stage
FROM golang:1.24-alpine AS build

ARG GOPROXY=direct
ENV GOPROXY=${GOPROXY}
ENV GOTOOLCHAIN=local

RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/ledger ./services/ledger/cmd/ledger

# Runtime stage
FROM alpine:latest
ARG GRPC_HEALTH_PROBE_VERSION=v0.4.26
RUN apk add --no-cache ca-certificates curl wget
RUN wget -qO /usr/local/bin/grpc_health_probe \
  https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-amd64
RUN chmod +x /usr/local/bin/grpc_health_probe
RUN adduser -D -H -u 10001 appuser
USER appuser
WORKDIR /app
COPY --from=build /out/ledger /app/ledger
EXPOSE 8080 9091
ENTRYPOINT ["/app/ledger"]
