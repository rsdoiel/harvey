package harvey

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// maxHTTPResponseBytes caps how many bytes Get will read from an HTTP response
// body, guarding against memory exhaustion from large or hostile responses.
const maxHTTPResponseBytes = 256 * 1024 * 1024 // 256 MB

// httpReader implements RemoteReader for http:// and https:// URIs.
type httpReader struct {
	client      *http.Client
	bearerToken string
	basicUser   string
	basicPass   string
}

/** newHTTPReader constructs an httpReader from environment credentials.
 * Reads HTTP_BEARER_TOKEN, HTTP_BASIC_USER, and HTTP_BASIC_PASSWORD from the
 * environment. All three are optional; public resources need no credentials.
 *
 * Returns:
 *   *httpReader — ready to use; never returns nil.
 *
 * Example:
 *   r := newHTTPReader()
 *   var buf bytes.Buffer
 *   _ = r.Get(ctx, "https://example.com/file.txt", &buf)
 */
func newHTTPReader() *httpReader {
	return &httpReader{
		client:      &http.Client{Timeout: 5 * time.Minute, CheckRedirect: httpsDowngradeCheck},
		bearerToken: os.Getenv("HTTP_BEARER_TOKEN"),
		basicUser:   os.Getenv("HTTP_BASIC_USER"),
		basicPass:   os.Getenv("HTTP_BASIC_PASSWORD"),
	}
}

// httpsDowngradeCheck is the http.Client.CheckRedirect callback that blocks
// redirects from HTTPS to HTTP (downgrade protection) and caps redirect chains.
func httpsDowngradeCheck(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("http: too many redirects")
	}
	if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme == "http" {
		return fmt.Errorf("http: refusing HTTPS→HTTP downgrade redirect to %s", req.URL)
	}
	return nil
}

// applyAuth attaches credentials to req when the reader has them configured.
func (r *httpReader) applyAuth(req *http.Request) {
	if r.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+r.bearerToken)
	} else if r.basicUser != "" || r.basicPass != "" {
		req.SetBasicAuth(r.basicUser, r.basicPass)
	}
}

/** Get downloads the resource at uri and writes its body to dst.
 * Returns an error for any non-2xx HTTP status code.
 *
 * Parameters:
 *   ctx (context.Context) — governs the request lifetime.
 *   uri (string)          — http:// or https:// URL.
 *   dst (io.Writer)       — destination for the response body.
 *
 * Returns:
 *   error — network error, non-2xx status, or HTTPS→HTTP downgrade attempt.
 *
 * Example:
 *   var buf bytes.Buffer
 *   err := r.Get(ctx, "https://example.com/file.txt", &buf)
 */
func (r *httpReader) Get(ctx context.Context, uri string, dst io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return fmt.Errorf("http: build request: %w", err)
	}
	r.applyAuth(req)

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("http: get %s: %w", uri, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http: get %s: status %d %s", uri, resp.StatusCode, resp.Status)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, maxHTTPResponseBytes))
	if err != nil {
		return fmt.Errorf("http: read body %s: %w", uri, err)
	}
	if n == maxHTTPResponseBytes {
		return fmt.Errorf("http: response body for %s exceeds %d-byte limit", uri, maxHTTPResponseBytes)
	}
	return nil
}

/** Stat sends a HEAD request for uri and returns metadata.
 * Size is -1 when the server omits Content-Length.
 *
 * Parameters:
 *   ctx (context.Context) — governs the request lifetime.
 *   uri (string)          — http:// or https:// URL.
 *
 * Returns:
 *   RemoteObjectInfo — URI, Size, ContentType; IsDir is always false for HTTP.
 *   error            — network error or non-2xx status.
 *
 * Example:
 *   info, err := r.Stat(ctx, "https://example.com/file.pdf")
 *   fmt.Println(info.Size)
 */
func (r *httpReader) Stat(ctx context.Context, uri string) (RemoteObjectInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, uri, nil)
	if err != nil {
		return RemoteObjectInfo{}, fmt.Errorf("http: build HEAD request: %w", err)
	}
	r.applyAuth(req)

	resp, err := r.client.Do(req)
	if err != nil {
		return RemoteObjectInfo{}, fmt.Errorf("http: head %s: %w", uri, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return RemoteObjectInfo{}, fmt.Errorf("http: head %s: status %d %s", uri, resp.StatusCode, resp.Status)
	}

	size := int64(-1)
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(cl, 10, 64); err == nil {
			size = n
		}
	}
	ct := resp.Header.Get("Content-Type")
	if idx := strings.IndexByte(ct, ';'); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}
	return RemoteObjectInfo{URI: uri, Size: size, ContentType: ct}, nil
}

/** List always returns an error for HTTP backends.
 * There is no standard directory-listing protocol over HTTP/HTTPS.
 *
 * Parameters:
 *   ctx    (context.Context) — unused.
 *   prefix (string)          — the URL prefix that was requested.
 *
 * Returns:
 *   nil, error — always an error.
 *
 * Example:
 *   _, err := r.List(ctx, "https://example.com/docs/")
 *   // err != nil always
 */
func (r *httpReader) List(_ context.Context, prefix string) ([]RemoteObjectInfo, error) {
	return nil, fmt.Errorf("http: List not supported for %s (no standard directory listing over HTTP)", prefix)
}
