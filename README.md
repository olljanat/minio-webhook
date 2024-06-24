### minio-webhook

Based on https://github.com/brandond/minio-webhook/ but with:
* support filtering out not interesting events with `MINIO_WEBHOOK_SKIP_BROWSING=true` environment variable.
* support to index MinIO content to Microsoft SQL Server (like it is supported with PostreSQL and MySQL).
* run ClamAV scan for new files.

