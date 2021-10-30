FROM golang:1.17-alpine AS build
ARG GH_CI_TOKEN=$GH_CI_TOKEN
WORKDIR /app
COPY / /app
ENV GOPRIVATE="github.com/vahter-robot/*"
RUN apk add --no-cache git
RUN git config --global url."https://vahter-robot:$GH_CI_TOKEN@github.com/".insteadOf "https://github.com/"
RUN go build -o servicebin cmd/main.go

FROM alpine:latest
WORKDIR /app
COPY --from=build /app/servicebin /app
