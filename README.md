### minio-webhook

minio-webhook listens for webhook events from MinIO server and logs the event to a log file or stdout.

Usage:
```
minio-webhook <logfile>
```

Environment variables:
* MINIO_WEBHOOK_AUTH_TOKEN: Authorization token to be used by minio server for sending events
* MINIO_WEBHOOK_PORT: Listening port (Default 8080)
* MINIO_WEBHOOK_FORMAT: `raw` to output the raw JSON log body, or any other value to print out plaintext log fields in a format similar to S3 access logs.
  Note that the plaintext format only recognizes access/audit logs, not system logs.

The minio-webhook can be setup as a Kubernetes service using the provided minio-webhook.yaml file.

To send access/audit logs from MinIO server, please see the instructions at https://min.io/docs/minio/macos/operations/monitoring/minio-logging.html
