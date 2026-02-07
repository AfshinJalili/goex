FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /matching ./services/matching/cmd/matching

FROM alpine:latest
RUN apk --no-cache add ca-certificates curl
COPY --from=builder /matching /matching
EXPOSE 9094 8080
CMD ["/matching"]
