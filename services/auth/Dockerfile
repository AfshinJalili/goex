FROM golang:1.24-alpine AS build

ARG GOPROXY=direct
ENV GOPROXY=${GOPROXY}
ENV GOTOOLCHAIN=local

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
WORKDIR /src/services/auth
RUN go build -o /app/auth ./cmd/auth

FROM alpine:latest
RUN apk add --no-cache ca-certificates curl
WORKDIR /app
COPY --from=build /app/auth /app/auth
EXPOSE 8080
ENTRYPOINT ["/app/auth"]
