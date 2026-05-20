# Harvey Remote Protocol Integration — Design

## Overview

Harvey's workspace is local by nature, but many workflows — RAG ingestion,
reading reference material, attaching documents — involve files that live on
remote storage. This design introduces a **protocol abstraction layer** that
allows Harvey to read from any URI-addressed storage backend in a consistent,
credential-safe way.

The layer is read-only. Harvey writes only to its local workspace.

**Initial target:** S3-compatible object storage (AWS S3, MinIO, Cloudflare R2).  
**Planned follow-on:** HTTP/HTTPS, SFTP/SCP.  
**Future:** Additional protocols (FTP, DAV, etc.) can be added by implementing
the `RemoteReader` interface.

## URI Scheme

All remote resources are addressed as URIs. Harvey detects a remote URI by
the presence of a `://` scheme prefix.

| Scheme | Backend | Notes |
|--------|---------|-------|
| `s3://bucket/key` | S3-compatible | AWS, MinIO, Cloudflare R2, etc. |
| `http://host/path` | HTTP | Public unauthenticated resources |
| `https://host/path` | HTTPS | TLS-secured public or authenticated |
| `sftp://user@host/path` | SFTP | SSH file transfer |
| `scp://user@host/path` | SCP | Treated as SFTP at protocol level |

A trailing `/` on a path signals directory/prefix semantics (list + walk).

## Protocol Abstraction Layer

A single narrow interface covers all backends:

```go
// RemoteReader provides read-only access to a remote storage backend.
type RemoteReader interface {
    // Stat returns metadata for a single object.
    Stat(ctx context.Context, uri string) (RemoteObjectInfo, error)
    // Get downloads a single object, writing content to dst.
    Get(ctx context.Context, uri string, dst io.Writer) error
    // List returns objects under a prefix URI (trailing slash).
    List(ctx context.Context, prefix string) ([]RemoteObjectInfo, error)
}

type RemoteObjectInfo struct {
    URI         string
    Size        int64
    ContentType string
    IsDir       bool
}
```

`NewRemoteReader(uri string) (RemoteReader, error)` inspects the scheme and
returns the appropriate backend, or an error if the scheme is unsupported or
required credentials are absent.

## S3-Compatible Backend

### Dependency

`github.com/minio/minio-go/v7` — works with AWS S3, MinIO, Cloudflare R2,
and any S3-compatible endpoint. No AWS SDK required.

### Credential Sources (environment only)

| Variable | Purpose |
|----------|---------|
| `AWS_ACCESS_KEY_ID` | Access key ID |
| `AWS_SECRET_ACCESS_KEY` | Secret access key |
| `AWS_SESSION_TOKEN` | Session token (optional, STS/assumed role) |
| `AWS_REGION` | Region (optional for MinIO/R2) |
| `AWS_ENDPOINT_URL` | Override endpoint for non-AWS services |

These are read once at client construction. They are never written to
`harvey.yaml`, never passed to LLMs, and never appear in tool results or
Fountain recordings.

### URI Parsing

```
s3://mybucket/docs/   → bucket=mybucket, key=docs/, isPrefix=true
s3://mybucket/file.md → bucket=mybucket, key=file.md, isPrefix=false
```

### File Type Filtering (RAG)

S3 prefix walks use the same `ragIngestableExts` map as local ingestion:
`.md`, `.txt`, `.go`, `.ts`, `.py`, `.yaml`, `.yml`, `.toml`, `.sql`, `.pdf`.

## HTTP/HTTPS Backend

### Dependency

`net/http` — Go standard library; no new module dependency.

### Credential Sources (environment only)

| Variable | Purpose |
|----------|---------|
| `HTTP_BEARER_TOKEN` | Bearer token for Authorization header |
| `HTTP_BASIC_USER` | Username for HTTP Basic auth |
| `HTTP_BASIC_PASSWORD` | Password for HTTP Basic auth |

Public resources require no credentials.

### Security Constraints

- HTTPS is always preferred. Harvey does **not** follow redirects from HTTPS
  to HTTP (downgrade protection).
- `List()` on an HTTP URI returns an error; there is no standard directory
  listing protocol over HTTP.

## SFTP/SCP Backend

### Dependency

`golang.org/x/crypto/ssh` — verify whether this is already in the module
graph before adding; it is a common transitive dependency.

### Credential Sources (environment only)

| Variable | Purpose |
|----------|---------|
| `SFTP_USER` | SSH username |
| `SFTP_PASSWORD` | Password (optional if key auth used) |
| `SFTP_KEY_PATH` | Path to private key file |
| `SFTP_HOST_KEY` | Expected host key fingerprint (SHA-256) |

SSH host key verification is **strict by default**. Harvey will not silently
accept an unknown host key. `SFTP_HOST_KEY` must be set, or the connection
fails with an actionable error message.

`scp://` URIs are treated identically to `sftp://` at the protocol level.

## Credential Safety

Credentials are read from environment variables at client construction and
are **never**:

- Written to `harvey.yaml` or any file
- Included in tool results returned to the LLM
- Logged to the Fountain session recording (only the URI is recorded)
- Passed to subprocesses via the environment

### `filterCommandEnvironment` Extensions

The following variables are stripped from any environment slice passed to
subprocesses or tool results:

```
AWS_ACCESS_KEY_ID       AWS_SECRET_ACCESS_KEY   AWS_SESSION_TOKEN
AWS_SECURITY_TOKEN      MINIO_ACCESS_KEY        MINIO_SECRET_KEY
SFTP_PASSWORD           SFTP_KEY_PATH           HTTP_BEARER_TOKEN
HTTP_BASIC_PASSWORD
```

## Integration Points

### `/rag ingest s3://bucket/prefix/`

`ragCollectFiles` detects the `s3://` scheme prefix and delegates to
`s3Reader.List()`. Each listed object is downloaded to `os.CreateTemp`,
processed through the existing ingest pipeline, and the temp file is removed
immediately after ingestion.

The existing large-file confirmation flow (> 100 KB) and per-file progress
reporting apply unchanged.

### `/read s3://bucket/key` (and other URI schemes)

The `/read` command detects a `://` prefix and calls
`NewRemoteReader(uri).Get(ctx, uri, buf)`. File content is returned to the
user and LLM context. The URI appears in output; credentials do not.

The same extension applies to `/attach` for including remote content as
conversation context.

### Tab Completion

`workspacePathCandidates` handles local paths only and is unchanged.

A separate `remotePathCandidates(scheme, partial string) []string` handles
URI completion:

- `s3://` prefix: list buckets (credentials present), then keys within a
  named bucket
- `sftp://`, `http://`, `https://`: no automatic listing; user types the path

## Implementation Phases

| Phase | Scope | Blocks on |
|-------|-------|-----------|
| 1 | `RemoteReader` interface + URI parsing + factory | — |
| 2 | S3 backend | Phase 1 |
| 3 | S3 RAG ingest + `/read` integration | Phase 2 |
| 4 | S3 tab completion | Phase 2 |
| 5 | `filterCommandEnvironment` extension | Phase 2 |
| 6 | HTTP/HTTPS backend | Phase 1 |
| 7 | SFTP/SCP backend | Phase 1 |
| 8 | Tests | Each phase |

## Security Considerations

- All remote access is **read-only**; no write operations are exposed.
- Temp files for RAG ingestion are created with `os.CreateTemp` and cleaned
  up with `defer os.Remove(f.Name())` immediately after each file is ingested.
- HTTPS → HTTP redirect downgrade is blocked in the HTTP backend.
- SFTP host key verification is strict; unknown keys are rejected with a
  clear error rather than silently accepted.
- The workspace path boundary (`resolveWorkspacePath`) does not apply to
  remote URIs; the boundary is enforced by what URIs the user explicitly
  provides in commands.

## Future Protocol Extensibility

Adding a new protocol requires only:

1. Implementing `RemoteReader` for the new scheme
2. Adding a case to `NewRemoteReader`'s scheme switch
3. Documenting credential env vars
4. Adding tests

No changes to `ragCollectFiles`, `/read`, or `/attach` are needed.
