package harvey

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// s3Reader implements RemoteReader for s3:// URIs using the MinIO Go client.
// It works with AWS S3, MinIO, and Cloudflare R2.
type s3Reader struct {
	client *minio.Client
	bucket string // retained only for constructing URIs in List results
}

/** newS3Reader constructs an s3Reader from AWS-compatible environment credentials.
 * Reads AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN, AWS_REGION,
 * and AWS_ENDPOINT_URL from the environment. All are optional; anonymous access
 * is used when the key variables are empty.
 *
 * Returns:
 *   *s3Reader — ready to use.
 *   error     — when the endpoint URL is malformed or the client cannot be created.
 *
 * Example:
 *   // With AWS_ENDPOINT_URL=http://localhost:9000 (MinIO)
 *   r, err := newS3Reader()
 */
func newS3Reader() (*s3Reader, error) {
	endpointURL := os.Getenv("AWS_ENDPOINT_URL")

	var endpoint string
	var useSSL bool

	if endpointURL != "" {
		u, err := url.Parse(endpointURL)
		if err != nil {
			return nil, fmt.Errorf("s3: invalid AWS_ENDPOINT_URL %q: %w", endpointURL, err)
		}
		endpoint = u.Host
		useSSL = u.Scheme == "https"
	} else {
		endpoint = "s3.amazonaws.com"
		useSSL = true
	}

	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	sessionToken := os.Getenv("AWS_SESSION_TOKEN")
	region := os.Getenv("AWS_REGION")

	var creds *credentials.Credentials
	if accessKey != "" || secretKey != "" {
		creds = credentials.NewStaticV4(accessKey, secretKey, sessionToken)
	} else {
		creds = credentials.NewEnvAWS()
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:        creds,
		Secure:       useSSL,
		Region:       region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("s3: create client: %w", err)
	}
	return &s3Reader{client: client}, nil
}

/** parseS3URI extracts the bucket name, object key, and whether the URI
 * addresses a prefix (directory) from an s3:// URI.
 * A trailing "/" signals prefix semantics; isPrefix is true in that case.
 *
 * Parameters:
 *   uri (string) — must begin with "s3://".
 *
 * Returns:
 *   bucket   (string) — S3 bucket name.
 *   key      (string) — object key within the bucket; empty for bucket-root.
 *   isPrefix (bool)   — true when uri ends with "/".
 *   error            — when the URI is malformed or the scheme is not s3.
 *
 * Example:
 *   bucket, key, isPrefix, _ := parseS3URI("s3://mybucket/docs/")
 *   // bucket="mybucket", key="docs/", isPrefix=true
 */
func parseS3URI(uri string) (bucket, key string, isPrefix bool, err error) {
	if !strings.HasPrefix(strings.ToLower(uri), "s3://") {
		return "", "", false, fmt.Errorf("s3: not an S3 URI: %q", uri)
	}
	rest := uri[5:] // skip "s3://"
	if rest == "" {
		return "", "", false, fmt.Errorf("s3: no bucket in URI: %q", uri)
	}
	idx := strings.IndexByte(rest, '/')
	if idx < 0 {
		// s3://bucket — no key, no trailing slash
		return rest, "", false, nil
	}
	bucket = rest[:idx]
	if bucket == "" {
		return "", "", false, fmt.Errorf("s3: no bucket in URI: %q", uri)
	}
	key = rest[idx+1:]
	isPrefix = strings.HasSuffix(uri, "/")
	return bucket, key, isPrefix, nil
}

/** Stat returns metadata for the S3 object at uri.
 *
 * Parameters:
 *   ctx (context.Context) — governs the request lifetime.
 *   uri (string)          — s3:// URI for a single object.
 *
 * Returns:
 *   RemoteObjectInfo — URI, Size, ContentType, IsDir.
 *   error            — when the object does not exist or the request fails.
 *
 * Example:
 *   info, err := r.Stat(ctx, "s3://mybucket/docs/file.md")
 *   fmt.Println(info.Size)
 */
func (r *s3Reader) Stat(ctx context.Context, uri string) (RemoteObjectInfo, error) {
	bucket, key, isPrefix, err := parseS3URI(uri)
	if err != nil {
		return RemoteObjectInfo{}, err
	}
	info, err := r.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return RemoteObjectInfo{}, fmt.Errorf("s3: stat %s: %w", uri, err)
	}
	return RemoteObjectInfo{
		URI:         uri,
		Size:        info.Size,
		ContentType: info.ContentType,
		IsDir:       isPrefix,
	}, nil
}

/** Get downloads the S3 object at uri and writes its content to dst.
 *
 * Parameters:
 *   ctx (context.Context) — governs the request lifetime.
 *   uri (string)          — s3:// URI for a single object.
 *   dst (io.Writer)       — destination for the object content.
 *
 * Returns:
 *   error — when the object does not exist, credentials are invalid, or I/O fails.
 *
 * Example:
 *   var buf bytes.Buffer
 *   err := r.Get(ctx, "s3://mybucket/docs/spec.md", &buf)
 */
func (r *s3Reader) Get(ctx context.Context, uri string, dst io.Writer) error {
	bucket, key, _, err := parseS3URI(uri)
	if err != nil {
		return err
	}
	obj, err := r.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("s3: get %s: %w", uri, err)
	}
	defer obj.Close()
	if _, err := io.Copy(dst, obj); err != nil {
		return fmt.Errorf("s3: read %s: %w", uri, err)
	}
	return nil
}

/** List returns all objects under the prefix URI. The prefix must end with "/".
 * Results include only objects at the immediate level (non-recursive); callers
 * that need recursive walks should call List on returned IsDir entries.
 *
 * Parameters:
 *   ctx    (context.Context) — governs the request lifetime.
 *   prefix (string)          — s3:// URI ending with "/" for the key prefix.
 *
 * Returns:
 *   []RemoteObjectInfo — one entry per object; IsDir marks common prefixes.
 *   error              — when the URI is malformed or the list request fails.
 *
 * Example:
 *   items, err := r.List(ctx, "s3://mybucket/docs/")
 */
func (r *s3Reader) List(ctx context.Context, prefix string) ([]RemoteObjectInfo, error) {
	bucket, keyPrefix, _, err := parseS3URI(prefix)
	if err != nil {
		return nil, err
	}
	var results []RemoteObjectInfo
	for obj := range r.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    keyPrefix,
		Recursive: false,
	}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("s3: list %s: %w", prefix, obj.Err)
		}
		results = append(results, RemoteObjectInfo{
			URI:         "s3://" + bucket + "/" + obj.Key,
			Size:        obj.Size,
			ContentType: obj.ContentType,
			IsDir:       strings.HasSuffix(obj.Key, "/"),
		})
	}
	return results, nil
}
