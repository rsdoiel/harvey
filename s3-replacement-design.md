# Harvey S3 Client Replacement (MinIO → aws-sdk-go-v2) — Design

**Status (2026-06-18):** Design settled. See
[s3-replacement-plan.md](s3-replacement-plan.md) for the implementation plan.

---

## Background

Harvey's `remote_s3.go` implements `RemoteReader` for `s3://` URIs using
`github.com/minio/minio-go/v7`. The MinIO Go client library has moved to a
closed-source license, making it incompatible with Harvey's AGPL-3.0
codebase and the open-source-first philosophy of the Laboratory workspace.

The three operations Harvey uses are:

| Harvey method | MinIO call | Description |
|---|---|---|
| `Stat` | `client.StatObject` | Get metadata for one S3 object |
| `Get` | `client.GetObject` | Download one S3 object to `io.Writer` |
| `List` | `client.ListObjects` | Iterate objects with a prefix |

These are a small, stable subset of the S3 API surface.

---

## Replacement: aws-sdk-go-v2

### Why aws-sdk-go-v2

The AWS SDK for Go v2 (`github.com/aws/aws-sdk-go-v2`) is:

- **Apache-2.0 licensed** — compatible with AGPL-3.0.
- **S3-compatible** — works with AWS S3, MinIO server, Cloudflare R2, and
  any other service that implements the S3 protocol, via the `BaseEndpoint`
  option on the client.
- **Actively maintained** — AWS's first-party SDK; unlikely to be
  abandoned or re-licensed.
- **Modular** — only the modules actually needed are imported, minimizing
  the dependency surface.

### Module imports

Replace:
```
github.com/minio/minio-go/v7        (removed)
github.com/minio/minio-go/v7/pkg/credentials  (removed)
```

Add:
```
github.com/aws/aws-sdk-go-v2/aws
github.com/aws/aws-sdk-go-v2/config
github.com/aws/aws-sdk-go-v2/service/s3
```

---

## API Mapping

### Client construction

Current (MinIO):
```go
client, err := minio.New(endpoint, &minio.Options{
    Creds:        credentials.NewStaticV4(accessKey, secretKey, ""),
    Secure:       strings.HasPrefix(endpoint, "https://"),
    BucketLookup: minio.BucketLookupPath,
})
```

Replacement (aws-sdk-go-v2):
```go
cfg, err := config.LoadDefaultConfig(ctx,
    config.WithRegion(region),
    config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
        accessKey, secretKey, "",
    )),
)
client := s3.NewFromConfig(cfg, func(o *s3.Options) {
    o.BaseEndpoint = aws.String(endpoint)
    o.UsePathStyle = true  // required for non-AWS endpoints
})
```

`region` defaults to `"us-east-1"` when not specified in the URI or config.
Non-AWS S3-compatible services (MinIO, R2) accept any region string.

### StatObject → HeadObject

Current:
```go
info, err := r.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
// info.Size, info.LastModified, info.ContentType
```

Replacement:
```go
resp, err := r.client.HeadObject(ctx, &s3.HeadObjectInput{
    Bucket: aws.String(bucket),
    Key:    aws.String(key),
})
// *resp.ContentLength, *resp.LastModified, *resp.ContentType
```

### GetObject

Current:
```go
obj, err := r.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
_, err = io.Copy(dst, obj)
obj.Close()
```

Replacement:
```go
resp, err := r.client.GetObject(ctx, &s3.GetObjectInput{
    Bucket: aws.String(bucket),
    Key:    aws.String(key),
})
_, err = io.Copy(dst, resp.Body)
resp.Body.Close()
```

### ListObjects → ListObjectsV2

Current (uses `ListObjects` which maps to the older S3 list API):
```go
for obj := range r.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
    Prefix:    prefix,
    Recursive: true,
}) {
    if obj.Err != nil { return obj.Err }
    // obj.Key, obj.Size, obj.LastModified
}
```

Replacement (uses `ListObjectsV2` paginator):
```go
paginator := s3.NewListObjectsV2Paginator(r.client, &s3.ListObjectsV2Input{
    Bucket: aws.String(bucket),
    Prefix: aws.String(prefix),
})
for paginator.HasMorePages() {
    page, err := paginator.NextPage(ctx)
    if err != nil { return err }
    for _, obj := range page.Contents {
        // *obj.Key, *obj.Size, *obj.LastModified
    }
}
```

`ListObjectsV2` is the current S3 best practice; `ListObjects` (v1) is
deprecated by AWS. Both are supported by all S3-compatible services.

---

## Configuration Changes

### harvey.yaml

No change to the user-visible configuration schema. The S3 remote
configuration fields (`endpoint`, `bucket`, `access_key`, `secret_key`,
`region`) remain the same.

### Credential resolution

The AWS SDK v2 credential chain reads from (in order):
1. Static credentials if `access_key` and `secret_key` are set in harvey.yaml.
2. `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` environment variables.
3. `~/.aws/credentials` shared credentials file.
4. IAM role credentials (EC2 instance roles, ECS task roles, etc.).

This is a superset of MinIO client's credential resolution. Users relying on
environment variables continue to work unchanged.

### Region

A new optional `region` field in the S3 remote config. Defaults to
`"us-east-1"` when absent. Non-AWS services ignore the value.

---

## Error Handling

AWS SDK v2 returns typed errors. The two most important for Harvey's use:

| Situation | AWS SDK error | Action |
|---|---|---|
| Object not found | `*types.NoSuchKey` | Return `ErrNotFound` (Harvey's sentinel) |
| Bucket not found | `*types.NoSuchBucket` | Return descriptive error with bucket name |
| Credentials failed | `smithy.APIError` with code `"InvalidAccessKeyId"` | Return error with hint to check credentials |

Harvey's `RemoteReader` interface returns plain `error`; the AWS error types
are unwrapped internally in `remote_s3.go` and mapped to Harvey's error
vocabulary.

---

## Testing

The existing `remote_test.go` uses a mock HTTP server to simulate S3
responses. The test infrastructure does not change — it will be updated to
serve responses compatible with the AWS SDK's request format (V4 signed
requests to an HTTP server). If the mock server is too complex to update,
an integration test flag (`--integration`) can gate tests that require a real
S3-compatible endpoint (e.g., a local MinIO server started for testing).

---

## Out of Scope

- **S3 write operations** — Harvey's `RemoteReader` is read-only. No PUT,
  DELETE, or multipart upload support is added.
- **Pre-signed URLs** — not used by the current implementation.
- **S3 event notifications** — not applicable.
- **Replacing other MinIO references** — Harvey has no other MinIO imports.
  `remote_s3.go` is the entire blast radius.
