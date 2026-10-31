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
	err := cmdAttach(a, []string{srv.URL + "/file.txt"}, &out)
	if len(a.History) != 0 {
		t.Error("no history entry expected on fetch error")
	}
	if err == nil || ExitCodeFor(err) == ClassOK {
		t.Errorf("expected a classed error for the failed fetch; got: %v", err)
	}
}

// ─── parseURIScheme ────────────────────────────────────────────────────────────

func TestParseURIScheme_s3(t *testing.T) {
	if got := parseURIScheme("s3://bucket/key"); got != "s3" {
		t.Errorf("got %q, want %q", got, "s3")
	}
}

// The s3:// reader was removed (workspace DR-0042); the error says so.
func TestNewRemoteReader_s3_removed(t *testing.T) {
	_, err := NewRemoteReader("s3://bucket/key")
	if err == nil || !strings.Contains(err.Error(), "DR-0042") {
		t.Fatalf("want an error naming DR-0042, got %v", err)
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

// ─── S3 backend ───────────────────────────────────────────────────────────────

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
