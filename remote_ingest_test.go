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
