package harvey

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// s3Reader implements RemoteReader for s3:// URIs using the AWS SDK v2.
// It works with AWS S3, MinIO server, Cloudflare R2, and any S3-compatible
// endpoint via BaseEndpoint + UsePathStyle.
type s3Reader struct {
	client   *s3.Client
	endpoint string
}

/** newS3Reader constructs an s3Reader from AWS-compatible environment credentials.
 * Reads AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN, AWS_REGION,
 * and AWS_ENDPOINT_URL from the environment. All are optional; the standard AWS
 * credential chain is used when key variables are absent.
 *
 * Returns:
 *   *s3Reader — ready to use.
 *   error     — when the configuration or client cannot be created.
 *
 * Example:
 *   // With AWS_ENDPOINT_URL=http://localhost:9000 (MinIO)
 *   r, err := newS3Reader()
 */
func newS3Reader() (*s3Reader, error) {
	endpointURL := os.Getenv("AWS_ENDPOINT_URL")
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	sessionToken := os.Getenv("AWS_SESSION_TOKEN")
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	var opts []func(*awsconfig.LoadOptions) error
	opts = append(opts, awsconfig.WithRegion(region))
	if accessKey != "" || secretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken),
		))
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("s3: load config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpointURL != "" {
			o.BaseEndpoint = aws.String(endpointURL)
		}
		o.UsePathStyle = true // required for non-AWS S3-compatible endpoints
	})

	return &s3Reader{client: client, endpoint: endpointURL}, nil
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

// isNotFound reports whether an AWS SDK error represents a missing object or bucket.
func isNotFound(err error) bool {
	var nsk *types.NoSuchKey
	var nsb *types.NoSuchBucket
	return errors.As(err, &nsk) || errors.As(err, &nsb)
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
	resp, err := r.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return RemoteObjectInfo{}, fmt.Errorf("s3: stat %s: not found", uri)
		}
		return RemoteObjectInfo{}, fmt.Errorf("s3: stat %s: %w", uri, err)
	}
	size := int64(0)
	if resp.ContentLength != nil {
		size = *resp.ContentLength
	}
	ct := ""
	if resp.ContentType != nil {
		ct = *resp.ContentType
	}
	return RemoteObjectInfo{
		URI:         uri,
		Size:        size,
		ContentType: ct,
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
	resp, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return fmt.Errorf("s3: get %s: not found", uri)
		}
		return fmt.Errorf("s3: get %s: %w", uri, err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(dst, resp.Body); err != nil {
		return fmt.Errorf("s3: read %s: %w", uri, err)
	}
	return nil
}

/** List returns all objects under the prefix URI. The prefix must end with "/".
 * Uses ListObjectsV2 with a paginator for scalable enumeration.
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

	paginator := s3.NewListObjectsV2Paginator(r.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(keyPrefix),
	})

	var results []RemoteObjectInfo
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3: list %s: %w", prefix, err)
		}
		for _, obj := range page.Contents {
			size := int64(0)
			if obj.Size != nil {
				size = *obj.Size
			}
			key := aws.ToString(obj.Key)
			results = append(results, RemoteObjectInfo{
				URI:   "s3://" + bucket + "/" + key,
				Size:  size,
				IsDir: strings.HasSuffix(key, "/"),
			})
		}
	}
	return results, nil
}
