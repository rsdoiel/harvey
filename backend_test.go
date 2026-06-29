package harvey

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBackendPIDJSONRoundTrip(t *testing.T) {
	original := BackendPID{
		Backend: "llamafile",
		PID:     12345,
		Model:   "phi4-Q4_K_M",
		URL:     "http://127.0.0.1:8080",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BackendPID
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded != original {
		t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, original)
	}
}

func TestBackendPIDJSONFieldNames(t *testing.T) {
	p := BackendPID{Backend: "ollama", PID: 1, Model: "granite3.3:8b", URL: "http://127.0.0.1:11434"}
	data, _ := json.Marshal(p)
	raw := string(data)
	for _, want := range []string{`"backend"`, `"pid"`, `"model"`, `"url"`} {
		if !strings.Contains(raw, want) {
			t.Errorf("JSON missing field key %s in: %s", want, raw)
		}
	}
}

func TestWriteReadDeletePIDFile(t *testing.T) {
	dir := t.TempDir()

	original := BackendPID{
		Backend: "llamacpp",
		PID:     99999,
		Model:   "qwen2.5-7b-Q4_K_M.gguf",
		URL:     "http://127.0.0.1:8081",
	}

	if err := writePIDFile(dir, original); err != nil {
		t.Fatalf("writePIDFile: %v", err)
	}

	got, err := readPIDFile(dir)
	if err != nil {
		t.Fatalf("readPIDFile: %v", err)
	}
	if got != original {
		t.Errorf("readPIDFile round-trip: got %+v, want %+v", got, original)
	}

	if err := deletePIDFile(dir); err != nil {
		t.Fatalf("deletePIDFile: %v", err)
	}

	_, err = readPIDFile(dir)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("after delete: expected ErrNotExist, got %v", err)
	}
}

func TestDeletePIDFileMissing(t *testing.T) {
	dir := t.TempDir()
	if err := deletePIDFile(dir); err != nil {
		t.Errorf("deletePIDFile on missing file should be a no-op, got: %v", err)
	}
}

func TestReadPIDFileMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := readPIDFile(dir)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("readPIDFile on missing file: expected ErrNotExist, got %v", err)
	}
}

func TestModelSummaryFields(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	s := ModelSummary{
		Name:      "phi4-Q4_K_M",
		Path:      "/home/user/Models/phi4-Q4_K_M.llamafile",
		Engine:    "llamafile",
		SizeBytes: 4_000_000_000,
		Modified:  now,
	}
	if s.Name != "phi4-Q4_K_M" {
		t.Errorf("Name: %q", s.Name)
	}
	if s.Engine != "llamafile" {
		t.Errorf("Engine: %q", s.Engine)
	}
	if s.SizeBytes != 4_000_000_000 {
		t.Errorf("SizeBytes: %d", s.SizeBytes)
	}
	if !s.Modified.Equal(now) {
		t.Errorf("Modified: %v", s.Modified)
	}
}
