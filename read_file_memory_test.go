package harvey

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// read_file returns at most Config.MaxOutputBytes of a file (capOutput), but it
// reads the whole file first: os.ReadFile, then string(data), then the cut. A
// model that names one large file in the workspace (a model, a database) costs
// the harvey process that file's whole size in memory, the same failure as
// search_files (OOM kill, wren, 2026-11-06).

// readAllocDelta runs read_file through the registry and reports how many bytes
// the process allocated while it ran, and the result.
func readAllocDelta(t *testing.T, a *Agent, argsJSON string) (alloc uint64, result string, err error) {
	t.Helper()
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	result, err = r.Dispatch(context.Background(), "read_file", argsJSON, 0)
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc, result, err
}

func TestReadFile_LargeFileIsNotReadWhole(t *testing.T) {
	for _, c := range []struct {
		name string
		head []byte
	}{
		{"binary", []byte("GGUF\x00\x00\x00")},
		{"text-like", []byte("a line of text\n")},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, root := makeTestAgent(t)
			sparseFile(t, filepath.Join(root, "big.dat"), c.head, largeFileBytes)

			alloc, _, _ := readAllocDelta(t, a, `{"path":"big.dat"}`)

			if alloc > searchAllocLimit {
				t.Errorf("read_file allocated %d MiB reading a %d MiB %s file; the limit is %d MiB (it reads the whole file, then cuts the output)",
					alloc>>20, largeFileBytes>>20, c.name, searchAllocLimit>>20)
			}
		})
	}
}

// A small file is still returned whole: the fix must not change ordinary reads.
func TestReadFile_SmallFileStillReturnedWhole(t *testing.T) {
	a, root := makeTestAgent(t)
	os.WriteFile(filepath.Join(root, "small.txt"), []byte("hello world\nsecond line\n"), 0o644)

	_, result, err := readAllocDelta(t, a, `{"path":"small.txt"}`)
	if err != nil || result != "hello world\nsecond line\n" {
		t.Errorf("got %q, %v", result, err)
	}
}

// A binary file is not read and not shown as garbage: the model gets a plain
// message saying what it is. A message, not an error, so a small model does not
// retry the call.
func TestReadFile_BinaryFileGetsAMessage(t *testing.T) {
	a, root := makeTestAgent(t)
	os.WriteFile(filepath.Join(root, "blob.bin"), append([]byte("GGUF\x00\x01\x02"), make([]byte, 4096)...), 0o644)

	_, result, err := readAllocDelta(t, a, `{"path":"blob.bin"}`)

	if err != nil {
		t.Fatalf("a binary file should be a message, not an error: %v", err)
	}
	if !strings.Contains(result, "looks binary") || !strings.Contains(result, "blob.bin") {
		t.Errorf("expected a message naming blob.bin as binary, got %q", result)
	}
	if strings.ContainsRune(result, 0) {
		t.Errorf("binary content leaked into the result")
	}
}

// When a file is cut at MaxOutputBytes the note says how big the file is, so
// the model knows how much it did not see.
func TestReadFile_TruncationNamesTheFileSize(t *testing.T) {
	a, root := makeTestAgent(t)
	a.Config.MaxOutputBytes = 1000
	sparseFile(t, filepath.Join(root, "big.txt"), []byte(strings.Repeat("a line of text\n", 64)), largeFileBytes)

	_, result, err := readAllocDelta(t, a, `{"path":"big.txt"}`)

	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(result, "[output truncated at 1000 bytes; the file is 256.0 MB]") {
		t.Errorf("truncation note should end with the cut and the file size, got ...%q", result[max(0, len(result)-90):])
	}
	if len(result) > 1200 {
		t.Errorf("result is %d bytes for a 1000 byte cap", len(result))
	}
}

// Chunked analysis needs the whole text, so above a ceiling the file is refused
// before the operator is asked anything and before it is read.
func TestReadFile_ChunkingRefusesAFileOverTheCeiling(t *testing.T) {
	old := maxChunkFileBytes
	maxChunkFileBytes = 1 << 20
	defer func() { maxChunkFileBytes = old }()

	a, root := makeChunkTestAgent(t, 10, "Extract all headings.\n") // tiny context: every file is over budget
	text := []byte(strings.Repeat("a line of text\n", 64))
	sparseFile(t, filepath.Join(root, "huge.txt"), text, 16<<20)

	alloc, result, err := readAllocDelta(t, a, `{"path":"huge.txt"}`)

	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "too large to chunk") || result == "chunk synthesis result" {
		t.Errorf("expected a refusal naming the size limit, got %q", result[:min(len(result), 200)])
	}
	if alloc > 4<<20 {
		t.Errorf("read_file allocated %d MiB for a refused file", alloc>>20)
	}
}
