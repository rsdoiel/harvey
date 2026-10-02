package harvey

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// mockRemoteReader is a test double for RemoteReader.
type mockRemoteReader struct {
	objects []RemoteObjectInfo
	content map[string]string // URI → body text
	listErr error
	getErr  error
}

func (m *mockRemoteReader) Stat(_ context.Context, uri string) (RemoteObjectInfo, error) {
	for _, o := range m.objects {
		if o.URI == uri {
			return o, nil
		}
	}
	return RemoteObjectInfo{}, fmt.Errorf("not found: %s", uri)
}

func (m *mockRemoteReader) Get(_ context.Context, uri string, dst io.Writer) error {
	if m.getErr != nil {
		return m.getErr
	}
	body, ok := m.content[uri]
	if !ok {
		return fmt.Errorf("not found: %s", uri)
	}
	_, err := io.WriteString(dst, body)
	return err
}

func (m *mockRemoteReader) List(_ context.Context, _ string) ([]RemoteObjectInfo, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.objects, nil
}

// ─── ragIngestRemotePrefix tests ─────────────────────────────────────────────

func TestRagIngestRemotePrefix_ingestsMarkdown(t *testing.T) {
	dir := t.TempDir()
	store, err := NewRagStore(filepath.Join(dir, "r.db"), "stub")
	if err != nil {
		t.Fatalf("NewRagStore: %v", err)
	}
	defer store.db.Close()

	ws, _ := NewWorkspace(dir)
	a := NewAgent(DefaultConfig(), ws)
	a.Rag = store

	reader := &mockRemoteReader{
		objects: []RemoteObjectInfo{
			{URI: "sftp://host/docs/notes.md", Size: 20},
		},
		content: map[string]string{
			"sftp://host/docs/notes.md": "# Notes\n\nSome content here.\n",
		},
	}

	var buf strings.Builder
	ragIngestRemotePrefix(a, reader, "sftp", "sftp://host/docs/", stubEmbedder{"stub"}, &buf)

	out := buf.String()
	if !strings.Contains(out, "chunk") {
		t.Errorf("expected chunk count in output, got: %s", out)
	}

	n, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n == 0 {
		t.Error("expected at least one chunk ingested")
	}
}

func TestRagIngestRemotePrefix_skipsNonIngestableExtension(t *testing.T) {
	dir := t.TempDir()
	store, err := NewRagStore(filepath.Join(dir, "r.db"), "stub")
	if err != nil {
		t.Fatalf("NewRagStore: %v", err)
	}
	defer store.db.Close()

	ws, _ := NewWorkspace(dir)
	a := NewAgent(DefaultConfig(), ws)
	a.Rag = store

	reader := &mockRemoteReader{
		objects: []RemoteObjectInfo{
			{URI: "sftp://host/bin/program.exe", Size: 1024},
		},
		content: map[string]string{},
	}

	var buf strings.Builder
	ragIngestRemotePrefix(a, reader, "sftp", "sftp://host/bin/", stubEmbedder{"stub"}, &buf)

	n, _ := store.Count()
	if n != 0 {
		t.Errorf("expected 0 chunks for non-ingestable file, got %d", n)
	}
}

func TestRagIngestRemotePrefix_skipsDirectoryEntries(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewRagStore(filepath.Join(dir, "r.db"), "stub")
	defer store.db.Close()

	ws, _ := NewWorkspace(dir)
	a := NewAgent(DefaultConfig(), ws)
	a.Rag = store

	reader := &mockRemoteReader{
		objects: []RemoteObjectInfo{
			{URI: "sftp://host/docs/", IsDir: true},
			{URI: "sftp://host/docs/file.md", Size: 30},
		},
		content: map[string]string{
			"sftp://host/docs/file.md": "# Hello\n\nContent.\n",
		},
	}

	var buf strings.Builder
	ragIngestRemotePrefix(a, reader, "sftp", "sftp://host/docs/", stubEmbedder{"stub"}, &buf)

	n, _ := store.Count()
	if n == 0 {
		t.Error("expected chunks from the non-directory file")
	}
}

func TestRagIngestRemotePrefix_listError(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewRagStore(filepath.Join(dir, "r.db"), "stub")
	defer store.db.Close()

	ws, _ := NewWorkspace(dir)
	a := NewAgent(DefaultConfig(), ws)
	a.Rag = store

	reader := &mockRemoteReader{
		listErr: fmt.Errorf("connection refused"),
	}

	var buf strings.Builder
	ragIngestRemotePrefix(a, reader, "sftp", "sftp://host/docs/", stubEmbedder{"stub"}, &buf)

	if !strings.Contains(buf.String(), "connection refused") {
		t.Errorf("expected error message in output, got: %s", buf.String())
	}
}

func TestRagIngestRemotePrefix_getError(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewRagStore(filepath.Join(dir, "r.db"), "stub")
	defer store.db.Close()

	ws, _ := NewWorkspace(dir)
	a := NewAgent(DefaultConfig(), ws)
	a.Rag = store

	reader := &mockRemoteReader{
		objects: []RemoteObjectInfo{
			{URI: "sftp://host/docs/file.md", Size: 10},
		},
		content: map[string]string{},
		getErr:  fmt.Errorf("permission denied"),
	}

	var buf strings.Builder
	ragIngestRemotePrefix(a, reader, "sftp", "sftp://host/docs/", stubEmbedder{"stub"}, &buf)

	if !strings.Contains(buf.String(), "permission denied") {
		t.Errorf("expected error message in output, got: %s", buf.String())
	}
	n, _ := store.Count()
	if n != 0 {
		t.Errorf("expected 0 chunks after get error, got %d", n)
	}
}

// ─── ragIngestHTTP tests ──────────────────────────────────────────────────────

func TestRagIngestHTTP_ingestsSingleFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "# Remote doc\n\nThis came from an HTTP server.\n")
	}))
	defer srv.Close()

	dir := t.TempDir()
	store, err := NewRagStore(filepath.Join(dir, "r.db"), "stub")
	if err != nil {
		t.Fatalf("NewRagStore: %v", err)
	}
	defer store.db.Close()

	ws, _ := NewWorkspace(dir)
	a := NewAgent(DefaultConfig(), ws)
	a.Rag = store

	uri := srv.URL + "/doc.md"
	var buf strings.Builder
	ragIngestHTTP(a, uri, stubEmbedder{"stub"}, &buf)

	n, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n == 0 {
		t.Errorf("expected at least one chunk from HTTP ingest, got 0; output: %s", buf.String())
	}
}

func TestRagIngestHTTP_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	store, _ := NewRagStore(filepath.Join(dir, "r.db"), "stub")
	defer store.db.Close()

	ws, _ := NewWorkspace(dir)
	a := NewAgent(DefaultConfig(), ws)
	a.Rag = store

	var buf strings.Builder
	ragIngestHTTP(a, srv.URL+"/missing.md", stubEmbedder{"stub"}, &buf)

	if !strings.Contains(buf.String(), "404") && !strings.Contains(buf.String(), "✗") {
		t.Errorf("expected error in output, got: %s", buf.String())
	}
	n, _ := store.Count()
	if n != 0 {
		t.Errorf("expected 0 chunks after HTTP error, got %d", n)
	}
}

// ─── ragIngest dispatch tests ─────────────────────────────────────────────────

func TestRagIngest_sftp_noLongerWarnsUnsupported(t *testing.T) {
	// Before the fix, sftp:// printed "remote ingest only supports s3://".
	// After the fix it should attempt to connect (and fail with a connection
	// error, not an "unsupported" message).
	dir := t.TempDir()
	store, _ := NewRagStore(filepath.Join(dir, "r.db"), "stub")
	defer store.db.Close()

	ws, _ := NewWorkspace(dir)
	cfg := DefaultConfig()
	cfg.Memory.RagStores = []RagStoreEntry{{Name: "test", DBPath: "r.db", EmbeddingModel: "stub"}}
	cfg.Memory.RagActive = "test"
	a := NewAgent(cfg, ws)
	a.Rag = store
	a.In = strings.NewReader("n\n")

	var buf strings.Builder
	_ = cmdRag(a, []string{"ingest", "sftp://localhost:2222/nonexistent/"}, &buf)
	out := buf.String()

	if strings.Contains(out, "remote ingest only supports s3") {
		t.Errorf("sftp:// should no longer produce the s3-only warning; got: %s", out)
	}
}

// ─── failures are returned, so a scripted session sees them ──────────────────

// offlineEmbedder stands in for an embedding server that cannot be used.
func offlineEmbedder() Embedder {
	return &failingEmbedder{name: "stub", err: fmt.Errorf("embedder offline")}
}

func ingestAgent(t *testing.T) (*Agent, *RagStore) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewRagStore(filepath.Join(dir, "r.db"), "stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.db.Close() })
	ws, _ := NewWorkspace(dir)
	a := NewAgent(DefaultConfig(), ws)
	a.Rag = store
	return a, store
}

func TestRagIngestRemotePrefix_ListFailureIsReturned(t *testing.T) {
	a, _ := ingestAgent(t)
	reader := &mockRemoteReader{listErr: fmt.Errorf("connection refused")}
	err := ragIngestRemotePrefix(a, reader, "sftp", "sftp://host/docs/", stubEmbedder{"stub"}, io.Discard)
	if ExitCodeFor(err) != ClassUnavailable || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("err = %v, want an unavailable error naming the cause", err)
	}
}

func TestRagIngestRemotePrefix_DownloadFailureIsReturned(t *testing.T) {
	a, store := ingestAgent(t)
	reader := &mockRemoteReader{
		objects: []RemoteObjectInfo{{URI: "sftp://host/docs/file.md", Size: 10}},
		getErr:  fmt.Errorf("permission denied"),
	}
	err := ragIngestRemotePrefix(a, reader, "sftp", "sftp://host/docs/", stubEmbedder{"stub"}, io.Discard)
	if ExitCodeFor(err) != ClassUnavailable {
		t.Fatalf("err = %v, want unavailable", err)
	}
	if n, _ := store.Count(); n != 0 {
		t.Errorf("%d chunks stored after a failed download", n)
	}
}

// One bad object does not stop the rest, and the failure is still reported.
func TestRagIngestRemotePrefix_ReportsTheFirstFailureAfterTryingEveryObject(t *testing.T) {
	a, store := ingestAgent(t)
	reader := &mockRemoteReader{
		objects: []RemoteObjectInfo{
			{URI: "sftp://host/docs/missing.md", Size: 10},
			{URI: "sftp://host/docs/good.md", Size: 20},
		},
		content: map[string]string{"sftp://host/docs/good.md": "# Good\n\nSome content.\n"},
	}
	err := ragIngestRemotePrefix(a, reader, "sftp", "sftp://host/docs/", stubEmbedder{"stub"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "missing.md") {
		t.Fatalf("err = %v, want the first failure (missing.md)", err)
	}
	if n, _ := store.Count(); n == 0 {
		t.Errorf("the good object was not ingested")
	}
}

func TestRagIngestRemotePrefix_EmbedderFailureIsReturned(t *testing.T) {
	a, _ := ingestAgent(t)
	reader := &mockRemoteReader{
		objects: []RemoteObjectInfo{{URI: "sftp://host/docs/a.md", Size: 20}},
		content: map[string]string{"sftp://host/docs/a.md": "# A\n\nText.\n"},
	}
	err := ragIngestRemotePrefix(a, reader, "sftp", "sftp://host/docs/", offlineEmbedder(), io.Discard)
	if ExitCodeFor(err) != ClassUnavailable {
		t.Fatalf("err = %v, want unavailable (the embedder could not be used)", err)
	}
}

func TestRagIngestRemotePrefix_NothingToIngestIsNotAFailure(t *testing.T) {
	a, _ := ingestAgent(t)
	reader := &mockRemoteReader{objects: []RemoteObjectInfo{{URI: "sftp://host/docs/", IsDir: true}}}
	if err := ragIngestRemotePrefix(a, reader, "sftp", "sftp://host/docs/", stubEmbedder{"stub"}, io.Discard); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestRagIngestHTTP_FailuresAreReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/ok.md") {
			io.WriteString(w, "# Ok\n\nContent.\n")
			return
		}
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()
	a, _ := ingestAgent(t)

	if err := ragIngestHTTP(a, srv.URL+"/ok.md", stubEmbedder{"stub"}, io.Discard); err != nil {
		t.Fatalf("a good URL: err = %v, want nil", err)
	}
	err := ragIngestHTTP(a, srv.URL+"/missing.md", stubEmbedder{"stub"}, io.Discard)
	if ExitCodeFor(err) != ClassUnavailable {
		t.Fatalf("a 404: err = %v, want unavailable", err)
	}
	err = ragIngestHTTP(a, srv.URL+"/ok.md", offlineEmbedder(), io.Discard)
	if ExitCodeFor(err) != ClassUnavailable {
		t.Fatalf("embedder failure: err = %v, want unavailable", err)
	}
}

// ragIngest as a whole: every source is tried, then the first failure is
// returned, whether it came from a remote object, an unsupported scheme or a
// local file.
func ragIngestFixture(t *testing.T) (*Agent, *RagStore) {
	a, store := ingestAgent(t)
	a.Config.Memory.RagStores = []RagStoreEntry{{Name: "s", EmbeddingModel: "stub"}}
	a.Config.Memory.RagActive = "s"
	a.Config.Ollama.URL = "http://127.0.0.1:1" // the embedder cannot be reached
	return a, store
}

func TestRagIngest_NotConfiguredIsNegative(t *testing.T) {
	a := newTestAgent(t)
	if err := ragIngest(a, []string{"x.md"}, io.Discard); ExitCodeFor(err) != ClassNegative {
		t.Fatalf("no store: err = %v, want negative", err)
	}
	a, _ = ingestAgent(t)
	if err := ragIngest(a, []string{"x.md"}, io.Discard); ExitCodeFor(err) != ClassNegative {
		t.Fatalf("no active store: err = %v, want negative", err)
	}
}

func TestRagIngest_UnsupportedSchemeIsUsage(t *testing.T) {
	a, _ := ragIngestFixture(t)
	err := ragIngest(a, []string{"gopher://host/x.md"}, io.Discard)
	if ExitCodeFor(err) != ClassUsage {
		t.Fatalf("err = %v, want usage", err)
	}
}

func TestRagIngest_RemoteFailureIsReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()
	a, _ := ragIngestFixture(t)
	err := ragIngest(a, []string{srv.URL + "/missing.md"}, io.Discard)
	if ExitCodeFor(err) != ClassUnavailable {
		t.Fatalf("err = %v, want unavailable", err)
	}
}

func TestRagIngest_LocalFileFailureIsReturned(t *testing.T) {
	a, _ := ragIngestFixture(t)
	if err := a.Workspace.WriteFile("a.md", []byte("# A\n\nSome text to embed.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ragIngest(a, []string{"a.md"}, io.Discard)
	if ExitCodeFor(err) != ClassUnavailable {
		t.Fatalf("err = %v, want unavailable (the embedding server cannot be reached)", err)
	}
}
