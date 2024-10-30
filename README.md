### minio-webhook

Based on https://github.com/brandond/minio-webhook/ but with:
* support filtering out not interesting events with `MINIO_WEBHOOK_SKIP_BROWSING=true` environment variable.
* support to index MinIO content to Microsoft SQL Server (like it is supported with PostreSQL and MySQL).
* run ClamAV scan for new files.

ClamAV policy:
```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "s3:GetBucketLocation",
                "s3:GetObject"
            ],
            "Resource": [
                "arn:aws:s3:::*"
            ]
        },
        {
            "Effect": "Allow",
            "Action": [
                "s3:PutObjectTagging"
            ],
            "Resource": [
                "arn:aws:s3:::*"
            ],
            "Condition": {
                "ForAnyValue:StringEquals": {
                    "s3:RequestObjectTag/ClamAV": [
                        "clean",
                        "infected"
                    ]
                }
            }
        }
    ]
}
```
