# Harvey Remote Protocol Integration — Implementation Plan

See [remote-protocol-design.md](remote-protocol-design.md) for the full design rationale.

## New Module Dependencies

| Module | Purpose | Phase |
|--------|---------|-------|
| `github.com/minio/minio-go/v7` | S3-compatible object storage | Phase 2 |
| `golang.org/x/crypto` | SSH for SFTP/SCP (verify if already in graph) | Phase 7 |

After adding dependencies:

```bash
cd harvey && go mod tidy
```

## Files to Create

| File | Contents |
|------|---------|
| `harvey/remote.go` | `RemoteReader` interface, `RemoteObjectInfo`, `NewRemoteReader` factory, `parseURIScheme` |
| `harvey/remote_s3.go` | S3 backend: `s3Reader`, `newS3Reader`, `parseS3URI`, `Stat/Get/List` |
| `harvey/remote_http.go` | HTTP/HTTPS backend: `httpReader`, auth headers, downgrade protection |
| `harvey/remote_sftp.go` | SFTP/SCP backend: `sftpReader`, strict host key verification |
| `harvey/remote_test.go` | Tests for all backends and integration points |

## Files to Modify

| File | Change |
|------|--------|
| `harvey/commands.go` | `ragCollectFiles`: detect `s3://` and delegate to S3 list/download; `cmdRead`/`cmdAttach`: detect `://` and delegate to `NewRemoteReader` |
| `harvey/terminal.go` | `buildCompleter`: detect `s3://` prefix in word and call `remotePathCandidates` |
| `harvey/environment.go` (or whichever file defines `filterCommandEnvironment`) | Extend blocklist with credential env var names |

## Implementation Phases

### Phase 1 — Abstraction Layer (`remote.go`)

```go
type RemoteReader interface {
    Stat(ctx context.Context, uri string) (RemoteObjectInfo, error)
    Get(ctx context.Context, uri string, dst io.Writer) error
    List(ctx context.Context, prefix string) ([]RemoteObjectInfo, error)
}

func parseURIScheme(uri string) string
func NewRemoteReader(uri string) (RemoteReader, error)
```

- `parseURIScheme` returns the scheme (e.g., `"s3"`) or `""` for local paths
- `NewRemoteReader` dispatches on scheme; returns error for unsupported schemes

### Phase 2 — S3 Backend (`remote_s3.go`)

```go
type s3Reader struct{ client *minio.Client }

func newS3Reader() (*s3Reader, error)          // reads env vars
func parseS3URI(uri string) (bucket, key string, isPrefix bool, err error)
func (r *s3Reader) Stat(ctx, uri) (RemoteObjectInfo, error)
func (r *s3Reader) Get(ctx, uri string, dst io.Writer) error
func (r *s3Reader) List(ctx, prefix string) ([]RemoteObjectInfo, error)
```

- `newS3Reader` reads `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`,
  `AWS_REGION`, `AWS_ENDPOINT_URL` from environment; constructs MinIO client
- `List` uses `ListObjects` with recursive=false to walk one prefix level;
  caller walks recursively if needed
- Files are filtered by `ragIngestableExts` in the RAG integration layer,
  not inside `List` itself (keeps the backend general-purpose)

### Phase 3 — RAG + Read Integration (`commands.go`)

**`ragCollectFiles` extension:**

```go
if parseURIScheme(path) == "s3" {
    reader, err := newS3Reader()
    // list objects, download each to os.CreateTemp, append temp path to files
    // defer cleanup of each temp file after ingest
}
```

- Large-file confirmation (> 100 KB) and per-file progress apply as-is
- Temp file lifecycle: create → ingest → remove (per file, not deferred to end)

**`cmdRead` / `cmdAttach` extension:**

```go
if parseURIScheme(arg) != "" {
    reader, err := NewRemoteReader(arg)
    // Get to a bytes.Buffer, return content
    // Show URI in output; never show credentials
}
```

### Phase 4 — S3 Tab Completion (`terminal.go`)

```go
func remotePathCandidates(word string) []string
```

- If `word` == `"s3://"` or `"s3://<partial-bucket>"`: list buckets
- If `word` == `"s3://bucket/"` or deeper: list keys under the prefix
- Returns nil if credentials are absent (no error shown; completion is silent)

In `buildCompleter`, before local path handling:

```go
if strings.HasPrefix(word, "s3://") {
    return remotePathCandidates(word)
}
```

### Phase 5 — Credential Filtering

Locate `filterCommandEnvironment` (search `harvey/*.go` for the function).
Extend its blocklist:

```go
var sensitiveEnvVars = map[string]bool{
    "AWS_ACCESS_KEY_ID":     true,
    "AWS_SECRET_ACCESS_KEY": true,
    "AWS_SESSION_TOKEN":     true,
    "AWS_SECURITY_TOKEN":    true,
    "MINIO_ACCESS_KEY":      true,
    "MINIO_SECRET_KEY":      true,
    "SFTP_PASSWORD":         true,
    "SFTP_KEY_PATH":         true,
    "HTTP_BEARER_TOKEN":     true,
    "HTTP_BASIC_PASSWORD":   true,
}
```

### Phase 6 — HTTP/HTTPS Backend (`remote_http.go`)

```go
type httpReader struct{ client *http.Client; bearerToken, basicUser, basicPassword string }

func newHTTPReader() *httpReader   // reads HTTP_* env vars
func (r *httpReader) Stat(ctx, uri) (RemoteObjectInfo, error)   // HEAD request
func (r *httpReader) Get(ctx, uri string, dst io.Writer) error  // GET; reject HTTPS→HTTP redirect
func (r *httpReader) List(ctx, prefix string) ([]RemoteObjectInfo, error)  // always error
```

- Custom `http.Client` with `CheckRedirect` that rejects scheme downgrades
- Auth header applied only when credentials are present

### Phase 7 — SFTP/SCP Backend (`remote_sftp.go`)

```go
type sftpReader struct{ /* ssh.Client + sftp.Client */ }

func newSFTPReader(host, user string) (*sftpReader, error)
func (r *sftpReader) Stat(ctx, uri) (RemoteObjectInfo, error)
func (r *sftpReader) Get(ctx, uri string, dst io.Writer) error
func (r *sftpReader) List(ctx, prefix string) ([]RemoteObjectInfo, error)
```

- `newSFTPReader` reads `SFTP_USER`, `SFTP_PASSWORD`, `SFTP_KEY_PATH`,
  `SFTP_HOST_KEY` from env
- Host key callback: compare fingerprint against `SFTP_HOST_KEY`; reject if
  missing or mismatch with a clear error: `"sftp: unknown host key for <host>; set SFTP_HOST_KEY=<fingerprint>"`
- `scp://` URIs resolved to `sftp://` before dispatch in `NewRemoteReader`

### Phase 8 — Tests (`remote_test.go`)

| Test | Method |
|------|--------|
| S3 `parseS3URI` — valid, prefix, invalid | Unit |
| S3 `Get` / `List` | `httptest.Server` mimicking MinIO responses |
| HTTP `Get` with bearer token | `httptest.Server` |
| HTTP HTTPS→HTTP redirect blocked | `httptest.Server` chain |
| HTTP `List` returns error | Unit |
| `filterCommandEnvironment` strips all credential vars | Unit |
| `remotePathCandidates` with mocked S3 | `httptest.Server` |
| SFTP unknown host key rejected | Mocked SSH handshake |

## Acceptance Criteria

- [ ] `/rag ingest s3://bucket/prefix/` lists, confirms if large, ingests with progress
- [ ] `/read s3://bucket/key` returns file content to user
- [ ] `/read https://example.com/file.txt` returns public content
- [ ] Credentials never appear in tool results, Fountain recording, or subprocess env
- [ ] HTTP backend rejects HTTPS → HTTP redirect
- [ ] SFTP backend rejects unrecognised host key with actionable error
- [ ] S3 tab completion lists buckets and keys when credentials present
- [ ] `go test ./...` passes
- [ ] `go mod tidy` leaves no unused deps
