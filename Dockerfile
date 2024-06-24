# First stage: Build static binary
FROM golang:1.22-alpine as builder
RUN apk add -U --no-cache ca-certificates
WORKDIR /go/src/minio-webhook
COPY . .
RUN CGO_ENABLED=0 go build -o minio-webhook main.go

# Second stage: setup the runtime container
FROM clamav/clamav:1.2
COPY /docker-entrypoint-unprivileged.sh /init-unprivileged
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go/src/minio-webhook/minio-webhook .
ENTRYPOINT ["/init-unprivileged"]
