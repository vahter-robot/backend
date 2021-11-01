FROM golang:1.17-alpine AS build
WORKDIR /app
COPY / /app
ENV CGO_ENABLED=0
RUN go test ./...
RUN go build -o servicebin cmd/main.go

FROM alpine:latest
WORKDIR /app
COPY --from=build /app/servicebin /app
