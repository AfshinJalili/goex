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
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/order-ingest ./services/order-ingest/cmd/order-ingest

# Runtime stage
FROM alpine:latest
RUN apk add --no-cache ca-certificates curl
RUN adduser -D -H -u 10001 appuser
USER appuser
WORKDIR /app
COPY --from=build /out/order-ingest /app/order-ingest
COPY services/order-ingest/config.yaml /app/config.yaml
EXPOSE 8083 9093
ENTRYPOINT ["/app/order-ingest"]
