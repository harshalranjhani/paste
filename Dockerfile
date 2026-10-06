# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/pastebin ./cmd/pastebin

FROM alpine:3.22
RUN apk add --no-cache ca-certificates wget \
	&& addgroup -S paste \
	&& adduser -S -G paste -H -s /sbin/nologin paste \
	&& mkdir -p /data \
	&& chown paste:paste /data

COPY --from=build /out/pastebin /usr/local/bin/pastebin

USER paste
WORKDIR /data
EXPOSE 8080
VOLUME ["/data"]

ENV LISTEN_ADDR=0.0.0.0:8080 \
	DATA_DIR=/data \
	DATABASE_PATH=/data/paste.sqlite \
	BASE_URL=http://localhost:8080

ENTRYPOINT ["/usr/local/bin/pastebin"]
