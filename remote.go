package harvey

import (
	"context"
	"fmt"
	"io"
	"strings"
)

/** RemoteReader provides read-only access to a remote storage backend.
 * All Harvey remote access is read-only; no write operations are exposed.
 * Implementations are returned by NewRemoteReader based on the URI scheme.
 *
 * Methods:
 *   Stat(ctx, uri)      — metadata for a single object
 *   Get(ctx, uri, dst)  — download a single object into dst
 *   List(ctx, prefix)   — enumerate objects under a prefix URI (trailing /)
 *
 * Example:
 *   r, err := NewRemoteReader("s3://mybucket/docs/spec.md")
 *   if err != nil { ... }
 *   var buf bytes.Buffer
 *   if err := r.Get(ctx, "s3://mybucket/docs/spec.md", &buf); err != nil { ... }
 */
type RemoteReader interface {
	Stat(ctx context.Context, uri string) (RemoteObjectInfo, error)
	Get(ctx context.Context, uri string, dst io.Writer) error
	List(ctx context.Context, prefix string) ([]RemoteObjectInfo, error)
}

/** RemoteObjectInfo describes a single remote object.
 *
 * Fields:
 *   URI         (string) — the full URI of the object
 *   Size        (int64)  — size in bytes; -1 when unknown
 *   ContentType (string) — MIME type; may be empty
 *   IsDir       (bool)   — true when the entry represents a prefix/directory
 *
 * Example:
 *   info, err := r.Stat(ctx, "s3://mybucket/file.md")
 *   fmt.Println(info.Size) // e.g. 4096
 */
type RemoteObjectInfo struct {
	URI         string
	Size        int64
	ContentType string
	IsDir       bool
}

/** parseURIScheme returns the lower-case scheme from a URI (the part before ://),
 * or "" when the string contains no "://" separator (i.e. a local path).
 *
 * Parameters:
 *   uri (string) — the URI to inspect.
 *
 * Returns:
 *   string — lower-case scheme such as "s3", "http", "sftp", or "" for local paths.
 *
 * Example:
 *   parseURIScheme("s3://bucket/key") // "s3"
 *   parseURIScheme("/local/path")     // ""
 *   parseURIScheme("https://x.com")  // "https"
 */
func parseURIScheme(uri string) string {
	idx := strings.Index(uri, "://")
	if idx < 0 {
		return ""
	}
	return strings.ToLower(uri[:idx])
}

/** NewRemoteReader returns a RemoteReader appropriate for the URI scheme.
 * Supported schemes: s3, http, https, sftp, scp.
 * Returns an error when the scheme is unsupported or required credentials
 * are absent.
 *
 * Parameters:
 *   uri (string) — any URI with a "scheme://" prefix.
 *
 * Returns:
 *   RemoteReader — backend implementation for the scheme.
 *   error        — when the scheme is unsupported or construction fails.
 *
 * Example:
 *   r, err := NewRemoteReader("https://example.com/file.txt")
 *   // r is an *httpReader
 */
func NewRemoteReader(uri string) (RemoteReader, error) {
	scheme := parseURIScheme(uri)
	switch scheme {
	case "s3":
		return newS3Reader()
	case "http", "https":
		return newHTTPReader(), nil
	case "sftp", "scp":
		host, user, err := parseSFTPURI(uri)
		if err != nil {
			return nil, err
		}
		return newSFTPReader(host, user)
	case "":
		return nil, fmt.Errorf("remote: %q is a local path, not a URI", uri)
	default:
		return nil, fmt.Errorf("remote: unsupported scheme %q in %q", scheme, uri)
	}
}
