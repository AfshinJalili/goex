# Build stage
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/template ./services/template/cmd/template

# Runtime stage
FROM alpine:3.19
RUN adduser -D -H -u 10001 appuser
USER appuser
WORKDIR /app
COPY --from=build /out/template /app/template
EXPOSE 8080
ENTRYPOINT ["/app/template"]
