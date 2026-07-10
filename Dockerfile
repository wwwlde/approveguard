FROM golang:1.24 AS builder

ARG VERSION=dev

WORKDIR /build/
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o ./ApproveGuard --ldflags "-w -s -extldflags '-static' -X main.version=${VERSION}" .

FROM alpine:3.21 AS alpine

RUN apk add -U --no-cache ca-certificates

FROM scratch AS app

LABEL org.opencontainers.image.source=https://github.com/wwwlde/ApproveGuard
LABEL org.opencontainers.image.description="ApproveGuard container image"
LABEL org.opencontainers.image.licenses=GPL-3.0-only

WORKDIR /app
COPY --from=builder /build/ApproveGuard .
COPY --from=alpine /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 8080
ENTRYPOINT ["/app/ApproveGuard"]
