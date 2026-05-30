package harvey

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// ─── cmdRead remote integration ────────────────────────────────────────────────

func TestCmdRead_remoteHTTP_injectsContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "# Remote Doc\nHello from HTTP.")
	}))
	defer srv.Close()

	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdRead(a, []string{srv.URL + "/doc.md"}, &out); err != nil {
		t.Fatalf("cmdRead: %v", err)
	}
	if len(a.History) == 0 {
		t.Fatal("expected message added to history")
	}
	if !strings.Contains(a.History[0].Content, "Remote Doc") {
		t.Errorf("history content missing expected text; got: %s", a.History[0].Content)
	}
}

func TestCmdRead_remoteHTTP_404_reportsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	a := newTestAgent(t)
	var out strings.Builder
	cmdRead(a, []string{srv.URL + "/missing.md"}, &out)
	if len(a.History) != 0 {
		t.Error("no message should be added when remote fetch fails")
	}
	if !strings.Contains(out.String(), "✗") {
		t.Errorf("expected error indicator in output; got: %s", out.String())
	}
}

func TestCmdRead_remoteS3_injectsContent(t *testing.T) {
	srv := fakeS3Server(t)
	defer srv.Close()
	t.Setenv("AWS_ENDPOINT_URL", srv.URL)
	t.Setenv("AWS_ACCESS_KEY_ID", "testkey")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "testsecret")

	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdRead(a, []string{"s3://testbucket/file.md"}, &out); err != nil {
		t.Fatalf("cmdRead S3: %v", err)
	}
	if len(a.History) == 0 {
		t.Fatal("expected message added to history for S3 read")
	}
	if !strings.Contains(a.History[0].Content, "Hello from S3") {
		t.Errorf("history content missing S3 text; got: %s", a.History[0].Content)
	}
}

func TestCmdRead_mixedLocalAndRemote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "remote content")
	}))
	defer srv.Close()

	a := newTestAgent(t)
	// Write a local file
	if err := a.Workspace.WriteFile("local.md", []byte("local content"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var out strings.Builder
	cmdRead(a, []string{"local.md", srv.URL + "/remote.md"}, &out)
	if len(a.History) == 0 {
		t.Fatal("expected history entry")
	}
	combined := a.History[0].Content
	if !strings.Contains(combined, "local content") {
		t.Errorf("missing local content: %s", combined)
	}
	if !strings.Contains(combined, "remote content") {
		t.Errorf("missing remote content: %s", combined)
	}
}

// ─── cmdAttach remote integration ─────────────────────────────────────────────

func TestCmdAttach_remoteHTTP_injectsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "attached remote text")
	}))
	defer srv.Close()

	a := newTestAgent(t)
	var out strings.Builder
	if err := cmdAttach(a, []string{srv.URL + "/file.txt"}, &out); err != nil {
		t.Fatalf("cmdAttach: %v", err)
	}
	if len(a.History) == 0 {
		t.Fatal("expected message added to history")
	}
	if !strings.Contains(a.History[0].Content, "attached remote text") {
		t.Errorf("history content missing attached text; got: %s", a.History[0].Content)
	}
}

func TestCmdAttach_remoteHTTP_fetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := newTestAgent(t)
	var out strings.Builder
	cmdAttach(a, []string{srv.URL + "/file.txt"}, &out)
	if len(a.History) != 0 {
		t.Error("no history entry expected on fetch error")
	}
	if !strings.Contains(out.String(), "✗") {
		t.Errorf("expected error indicator in output; got: %s", out.String())
	}
}

// ─── remotePathCandidates ─────────────────────────────────────────────────────

func TestRemotePathCandidates_s3returnsKeys(t *testing.T) {
	srv := fakeS3Server(t)
	defer srv.Close()
	t.Setenv("AWS_ENDPOINT_URL", srv.URL)
	t.Setenv("AWS_ACCESS_KEY_ID", "testkey")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "testsecret")

	candidates := remotePathCandidates("s3://testbucket/docs/")
	if len(candidates) == 0 {
		t.Fatal("expected candidates for s3://testbucket/docs/")
	}
	var hasFile1, hasFile2 bool
	for _, c := range candidates {
		if strings.HasSuffix(c, "file1.md") {
			hasFile1 = true
		}
		if strings.HasSuffix(c, "file2.txt") {
			hasFile2 = true
		}
	}
	if !hasFile1 {
		t.Errorf("file1.md not in candidates: %v", candidates)
	}
	if !hasFile2 {
		t.Errorf("file2.txt not in candidates: %v", candidates)
	}
}

func TestRemotePathCandidates_noBucket_returnsNil(t *testing.T) {
	// Just "s3://" with no bucket — cannot complete without a bucket name.
	candidates := remotePathCandidates("s3://")
	if len(candidates) != 0 {
		t.Errorf("expected nil for bare s3://, got %v", candidates)
	}
}

func TestRemotePathCandidates_nonS3_returnsNil(t *testing.T) {
	candidates := remotePathCandidates("http://example.com/")
	if len(candidates) != 0 {
		t.Errorf("expected nil for non-s3 URI, got %v", candidates)
	}
}

func TestBuildCompleter_s3prefix(t *testing.T) {
	srv := fakeS3Server(t)
	defer srv.Close()
	t.Setenv("AWS_ENDPOINT_URL", srv.URL)
	t.Setenv("AWS_ACCESS_KEY_ID", "testkey")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "testsecret")

	a := newTestAgent(t)
	completer := a.buildCompleter()
	results := completer("/read s3://testbucket/docs/")
	if len(results) == 0 {
		t.Fatal("expected completions for /read s3://testbucket/docs/")
	}
	for _, r := range results {
		if !strings.HasPrefix(r, "s3://") {
			t.Errorf("completion %q does not start with s3://", r)
		}
	}
}

// ─── parseURIScheme ────────────────────────────────────────────────────────────

func TestParseURIScheme_s3(t *testing.T) {
	if got := parseURIScheme("s3://bucket/key"); got != "s3" {
		t.Errorf("got %q, want %q", got, "s3")
	}
}

func TestParseURIScheme_https(t *testing.T) {
	if got := parseURIScheme("https://example.com/file.txt"); got != "https" {
		t.Errorf("got %q, want %q", got, "https")
	}
}

func TestParseURIScheme_http(t *testing.T) {
	if got := parseURIScheme("http://example.com/file.txt"); got != "http" {
		t.Errorf("got %q, want %q", got, "http")
	}
}

func TestParseURIScheme_sftp(t *testing.T) {
	if got := parseURIScheme("sftp://user@host/path"); got != "sftp" {
		t.Errorf("got %q, want %q", got, "sftp")
	}
}

func TestParseURIScheme_scp(t *testing.T) {
	if got := parseURIScheme("scp://user@host/path"); got != "scp" {
		t.Errorf("got %q, want %q", got, "scp")
	}
}

func TestParseURIScheme_localPath(t *testing.T) {
	if got := parseURIScheme("/local/path/file.md"); got != "" {
		t.Errorf("got %q, want empty string for local path", got)
	}
}

func TestParseURIScheme_relPath(t *testing.T) {
	if got := parseURIScheme("relative/path"); got != "" {
		t.Errorf("got %q, want empty string for relative path", got)
	}
}

func TestParseURIScheme_upperCase(t *testing.T) {
	// Scheme should be normalised to lower-case.
	if got := parseURIScheme("S3://bucket/key"); got != "s3" {
		t.Errorf("got %q, want %q", got, "s3")
	}
}

// ─── NewRemoteReader dispatch ──────────────────────────────────────────────────

func TestNewRemoteReader_localPath_error(t *testing.T) {
	_, err := NewRemoteReader("/local/path")
	if err == nil {
		t.Fatal("expected error for local path, got nil")
	}
}

func TestNewRemoteReader_unknownScheme_error(t *testing.T) {
	_, err := NewRemoteReader("ftp://ftp.example.com/file")
	if err == nil {
		t.Fatal("expected error for unsupported scheme, got nil")
	}
	if !strings.Contains(err.Error(), "ftp") {
		t.Errorf("error should mention the bad scheme: %v", err)
	}
}

func TestNewRemoteReader_http_returnsReader(t *testing.T) {
	r, err := NewRemoteReader("http://example.com/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r == nil {
		t.Fatal("expected non-nil RemoteReader for http scheme")
	}
}

func TestNewRemoteReader_https_returnsReader(t *testing.T) {
	r, err := NewRemoteReader("https://example.com/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r == nil {
		t.Fatal("expected non-nil RemoteReader for https scheme")
	}
}

// ─── parseS3URI ───────────────────────────────────────────────────────────────

func TestParseS3URI_file(t *testing.T) {
	bucket, key, isPrefix, err := parseS3URI("s3://mybucket/docs/file.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bucket != "mybucket" {
		t.Errorf("bucket: got %q, want %q", bucket, "mybucket")
	}
	if key != "docs/file.md" {
		t.Errorf("key: got %q, want %q", key, "docs/file.md")
	}
	if isPrefix {
		t.Error("isPrefix: got true, want false for non-/ path")
	}
}

func TestParseS3URI_prefix(t *testing.T) {
	bucket, key, isPrefix, err := parseS3URI("s3://mybucket/docs/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bucket != "mybucket" {
		t.Errorf("bucket: got %q, want %q", bucket, "mybucket")
	}
	if key != "docs/" {
		t.Errorf("key: got %q, want %q", key, "docs/")
	}
	if !isPrefix {
		t.Error("isPrefix: got false, want true for trailing-/ path")
	}
}

func TestParseS3URI_rootPrefix(t *testing.T) {
	bucket, key, isPrefix, err := parseS3URI("s3://mybucket/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bucket != "mybucket" {
		t.Errorf("bucket: got %q, want %q", bucket, "mybucket")
	}
	if key != "" {
		t.Errorf("key: got %q, want empty", key)
	}
	if !isPrefix {
		t.Error("isPrefix: got false, want true for root prefix")
	}
}

func TestParseS3URI_bucketOnly(t *testing.T) {
	bucket, key, isPrefix, err := parseS3URI("s3://mybucket")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bucket != "mybucket" {
		t.Errorf("bucket: got %q, want %q", bucket, "mybucket")
	}
	if key != "" {
		t.Errorf("key: got %q, want empty", key)
	}
	if isPrefix {
		t.Error("isPrefix: got true, want false for bare bucket (no trailing /)")
	}
}

func TestParseS3URI_noBucket_error(t *testing.T) {
	_, _, _, err := parseS3URI("s3://")
	if err == nil {
		t.Fatal("expected error for URI with no bucket, got nil")
	}
}

func TestParseS3URI_wrongScheme_error(t *testing.T) {
	_, _, _, err := parseS3URI("http://bucket/key")
	if err == nil {
		t.Fatal("expected error for non-s3 scheme, got nil")
	}
}

// ─── S3 backend ───────────────────────────────────────────────────────────────

// fakeS3Server returns an httptest.Server that mimics a minimal S3-compatible
// endpoint. It returns canned responses keyed on the request URL path and query.
func fakeS3Server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// BucketLocation: GET /bucket/?location — MinIO client pre-flight to discover region.
		if r.Method == http.MethodGet && r.URL.Query().Has("location") {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`+
				`<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`)
			return
		}

		// ListObjectsV2: GET /bucket?list-type=2
		if r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2" {
			prefix := r.URL.Query().Get("prefix")
			body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>testbucket</Name>
  <Prefix>%s</Prefix>
  <KeyCount>2</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>false</IsTruncated>
  <Contents>
    <Key>%sfile1.md</Key>
    <Size>42</Size>
    <LastModified>2024-01-01T00:00:00.000Z</LastModified>
    <ETag>&quot;abc123&quot;</ETag>
  </Contents>
  <Contents>
    <Key>%sfile2.txt</Key>
    <Size>17</Size>
    <LastModified>2024-01-01T00:00:00.000Z</LastModified>
    <ETag>&quot;def456&quot;</ETag>
  </Contents>
</ListBucketResult>`, prefix, prefix, prefix)
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, body)
			return
		}

		// HeadObject: HEAD /bucket/key
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "42")
			w.Header().Set("Content-Type", "text/markdown")
			w.Header().Set("ETag", `"abc123"`)
			w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
			w.WriteHeader(http.StatusOK)
			return
		}

		// GetObject: GET /bucket/key (no list-type or location query param)
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/markdown")
			w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "# Hello from S3\n")
			return
		}

		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
}

func TestS3Reader_Get(t *testing.T) {
	srv := fakeS3Server(t)
	defer srv.Close()

	t.Setenv("AWS_ENDPOINT_URL", srv.URL)
	t.Setenv("AWS_ACCESS_KEY_ID", "testkey")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "testsecret")

	r, err := newS3Reader()
	if err != nil {
		t.Fatalf("newS3Reader: %v", err)
	}

	var buf bytes.Buffer
	if err := r.Get(context.Background(), "s3://testbucket/file.md", &buf); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(buf.String(), "Hello from S3") {
		t.Errorf("Get: unexpected content %q", buf.String())
	}
}

func TestS3Reader_Stat(t *testing.T) {
	srv := fakeS3Server(t)
	defer srv.Close()

	t.Setenv("AWS_ENDPOINT_URL", srv.URL)
	t.Setenv("AWS_ACCESS_KEY_ID", "testkey")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "testsecret")

	r, err := newS3Reader()
	if err != nil {
		t.Fatalf("newS3Reader: %v", err)
	}

	info, err := r.Stat(context.Background(), "s3://testbucket/file.md")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.URI != "s3://testbucket/file.md" {
		t.Errorf("URI: got %q, want %q", info.URI, "s3://testbucket/file.md")
	}
	if info.Size != 42 {
		t.Errorf("Size: got %d, want 42", info.Size)
	}
}

func TestS3Reader_List(t *testing.T) {
	srv := fakeS3Server(t)
	defer srv.Close()

	t.Setenv("AWS_ENDPOINT_URL", srv.URL)
	t.Setenv("AWS_ACCESS_KEY_ID", "testkey")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "testsecret")

	r, err := newS3Reader()
	if err != nil {
		t.Fatalf("newS3Reader: %v", err)
	}

	items, err := r.List(context.Background(), "s3://testbucket/docs/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("List: got %d items, want 2", len(items))
	}
	if !strings.HasSuffix(items[0].URI, "file1.md") {
		t.Errorf("item[0].URI: got %q, want suffix file1.md", items[0].URI)
	}
	if !strings.HasSuffix(items[1].URI, "file2.txt") {
		t.Errorf("item[1].URI: got %q, want suffix file2.txt", items[1].URI)
	}
}

func TestS3Reader_newS3Reader_missingCredentials(t *testing.T) {
	// Without credentials, newS3Reader should still succeed (anonymous access
	// is valid for public buckets). The error surfaces on Get/Stat/List.
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_ENDPOINT_URL", "")
	_, err := newS3Reader()
	if err != nil {
		t.Fatalf("newS3Reader with no creds: %v", err)
	}
}

// ─── HTTP backend ─────────────────────────────────────────────────────────────

func TestHTTPReader_Get_basic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "hello remote")
	}))
	defer srv.Close()

	r := newHTTPReader()
	var buf bytes.Buffer
	if err := r.Get(context.Background(), srv.URL+"/file.txt", &buf); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if buf.String() != "hello remote" {
		t.Errorf("content: got %q, want %q", buf.String(), "hello remote")
	}
}

func TestHTTPReader_Get_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	r := newHTTPReader()
	var buf bytes.Buffer
	err := r.Get(context.Background(), srv.URL+"/missing.txt", &buf)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestHTTPReader_Get_bearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	t.Setenv("HTTP_BEARER_TOKEN", "mytoken")
	r := newHTTPReader()
	var buf bytes.Buffer
	if err := r.Get(context.Background(), srv.URL+"/file", &buf); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotAuth != "Bearer mytoken" {
		t.Errorf("Authorization: got %q, want %q", gotAuth, "Bearer mytoken")
	}
}

func TestHTTPReader_Get_basicAuth(t *testing.T) {
	var gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	t.Setenv("HTTP_BASIC_USER", "alice")
	t.Setenv("HTTP_BASIC_PASSWORD", "s3cret")
	r := newHTTPReader()
	var buf bytes.Buffer
	if err := r.Get(context.Background(), srv.URL+"/file", &buf); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotUser != "alice" || gotPass != "s3cret" {
		t.Errorf("BasicAuth: got user=%q pass=%q, want alice/s3cret", gotUser, gotPass)
	}
}

func TestHTTPReader_Stat_basic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Length", "512")
		w.Header().Set("Content-Type", "application/pdf")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := newHTTPReader()
	info, err := r.Stat(context.Background(), srv.URL+"/doc.pdf")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size != 512 {
		t.Errorf("Size: got %d, want 512", info.Size)
	}
	if info.ContentType != "application/pdf" {
		t.Errorf("ContentType: got %q, want %q", info.ContentType, "application/pdf")
	}
}

func TestHTTPReader_List_alwaysErrors(t *testing.T) {
	r := newHTTPReader()
	_, err := r.List(context.Background(), "http://example.com/")
	if err == nil {
		t.Fatal("expected error from List on HTTP backend, got nil")
	}
}

// ─── HTTPS→HTTP redirect blocking ─────────────────────────────────────────────

func TestHTTPSDowngradeCheck_blocked(t *testing.T) {
	// Simulate: original request was HTTPS, redirect target is HTTP.
	via := []*http.Request{{URL: &url.URL{Scheme: "https", Host: "example.com"}}}
	req := &http.Request{URL: &url.URL{Scheme: "http", Host: "example.com"}}
	err := httpsDowngradeCheck(req, via)
	if err == nil {
		t.Fatal("expected error for HTTPS→HTTP downgrade, got nil")
	}
}

func TestHTTPSDowngradeCheck_allowsHTTPStoHTTPS(t *testing.T) {
	via := []*http.Request{{URL: &url.URL{Scheme: "https", Host: "example.com"}}}
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: "other.example.com"}}
	if err := httpsDowngradeCheck(req, via); err != nil {
		t.Errorf("unexpected error for HTTPS→HTTPS redirect: %v", err)
	}
}

func TestHTTPSDowngradeCheck_allowsHTTPtoHTTP(t *testing.T) {
	via := []*http.Request{{URL: &url.URL{Scheme: "http", Host: "example.com"}}}
	req := &http.Request{URL: &url.URL{Scheme: "http", Host: "other.example.com"}}
	if err := httpsDowngradeCheck(req, via); err != nil {
		t.Errorf("unexpected error for HTTP→HTTP redirect: %v", err)
	}
}

func TestHTTPSDowngradeCheck_tooManyRedirects(t *testing.T) {
	via := make([]*http.Request, 10)
	for i := range via {
		via[i] = &http.Request{URL: &url.URL{Scheme: "http"}}
	}
	req := &http.Request{URL: &url.URL{Scheme: "http"}}
	err := httpsDowngradeCheck(req, via)
	if err == nil {
		t.Fatal("expected error for too many redirects, got nil")
	}
}

// ─── filterCommandEnvironment credential stripping ────────────────────────────

func TestFilterCommandEnvironment_stripsAWSCredentials(t *testing.T) {
	env := []string{
		"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
		"AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG",
		"AWS_SESSION_TOKEN=AQoXnyc4lcK4w",
		"AWS_SECURITY_TOKEN=oldtoken",
		"PATH=/usr/bin:/bin",
	}
	filtered := filterCommandEnvironment(env)
	for _, v := range filtered {
		if strings.HasPrefix(v, "AWS_ACCESS_KEY_ID=") ||
			strings.HasPrefix(v, "AWS_SECRET_ACCESS_KEY=") ||
			strings.HasPrefix(v, "AWS_SESSION_TOKEN=") ||
			strings.HasPrefix(v, "AWS_SECURITY_TOKEN=") {
			t.Errorf("credential leaked into filtered env: %q", v)
		}
	}
	// PATH should survive
	found := false
	for _, v := range filtered {
		if strings.HasPrefix(v, "PATH=") {
			found = true
		}
	}
	if !found {
		t.Error("PATH was stripped from filtered env")
	}
}

func TestFilterCommandEnvironment_stripsMINIOCredentials(t *testing.T) {
	env := []string{
		"MINIO_ACCESS_KEY=minioadmin",
		"MINIO_SECRET_KEY=minioadmin",
		"HOME=/home/user",
	}
	filtered := filterCommandEnvironment(env)
	for _, v := range filtered {
		if strings.HasPrefix(v, "MINIO_ACCESS_KEY=") || strings.HasPrefix(v, "MINIO_SECRET_KEY=") {
			t.Errorf("credential leaked into filtered env: %q", v)
		}
	}
}

func TestFilterCommandEnvironment_stripsSFTPCredentials(t *testing.T) {
	env := []string{
		"SFTP_PASSWORD=s3cr3t",
		"SFTP_KEY_PATH=/home/user/.ssh/id_ed25519",
		"HOME=/home/user",
	}
	filtered := filterCommandEnvironment(env)
	for _, v := range filtered {
		if strings.HasPrefix(v, "SFTP_PASSWORD=") || strings.HasPrefix(v, "SFTP_KEY_PATH=") {
			t.Errorf("credential leaked into filtered env: %q", v)
		}
	}
}

func TestFilterCommandEnvironment_stripsHTTPCredentials(t *testing.T) {
	env := []string{
		"HTTP_BEARER_TOKEN=tok123",
		"HTTP_BASIC_PASSWORD=hunter2",
		"HOME=/home/user",
	}
	filtered := filterCommandEnvironment(env)
	for _, v := range filtered {
		if strings.HasPrefix(v, "HTTP_BEARER_TOKEN=") || strings.HasPrefix(v, "HTTP_BASIC_PASSWORD=") {
			t.Errorf("credential leaked into filtered env: %q", v)
		}
	}
}
