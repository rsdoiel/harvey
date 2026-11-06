package harvey

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Found 2026-11-06 on wren: harvey was OOM-killed at about 10 GiB resident
// after the model called search_files with no path. The tool read every file
// in the workspace whole (os.ReadFile, then string(data)) before it asked
// whether the file was binary, so one large model file or database cost its
// whole size in memory. These tests make the search's allocation, not its
// output, the thing under test.

// searchAllocDelta runs search_files through the registry and reports how many
// bytes the process allocated while it ran. TotalAlloc only grows, so the
// answer does not depend on when the collector runs.
func searchAllocDelta(t *testing.T, a *Agent, argsJSON string) (alloc uint64, result string) {
	t.Helper()
	r := NewToolRegistry()
	RegisterBuiltinTools(r, a)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	res, err := r.Dispatch(context.Background(), "search_files", argsJSON, 0)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("search_files: %v", err)
	}
	return after.TotalAlloc - before.TotalAlloc, res
}

// sparseFile makes a file of size bytes that takes almost no disk: a hole,
// with head written at the start. Reading it whole still returns size bytes.
func sparseFile(t *testing.T, path string, head []byte, size int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(head); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
}

const (
	// largeFileBytes is far more than a source file and far less than the
	// model files in the real workspace; 256 MiB shows the effect without
	// strain on a small machine.
	largeFileBytes = 256 << 20
	// searchAllocLimit is what a search over a few small files and one large
	// file may allocate: a 32 MiB limit is 8 times below the large file.
	searchAllocLimit = 32 << 20
)

// A large binary file (a model, a database) is recognised by its first bytes
// and must not be read whole.
func TestSearchFiles_LargeBinaryFileIsNotReadWhole(t *testing.T) {
	a, root := makeTestAgent(t)
	os.WriteFile(filepath.Join(root, "notes.md"), []byte("the memory store is retired\n"), 0o644)
	sparseFile(t, filepath.Join(root, "model.gguf"), []byte("GGUF\x00\x00\x00"), largeFileBytes)

	alloc, result := searchAllocDelta(t, a, `{"pattern":"memory store"}`)

	if !strings.Contains(result, "notes.md:1:the memory store is retired") {
		t.Errorf("the small file's match is missing from the result: %q", result)
	}
	if alloc > searchAllocLimit {
		t.Errorf("search_files allocated %d MiB while searching a workspace with one %d MiB binary file; the limit is %d MiB (it reads files whole before checking for binary)",
			alloc>>20, largeFileBytes>>20, searchAllocLimit>>20)
	}
}

// A large file that begins like text passes the binary check, so reading it
// whole is not allowed either: the size has to be looked at first, or the file
// streamed.
func TestSearchFiles_LargeTextLikeFileIsNotReadWhole(t *testing.T) {
	a, root := makeTestAgent(t)
	os.WriteFile(filepath.Join(root, "notes.md"), []byte("the memory store is retired\n"), 0o644)
	head := []byte(strings.Repeat("a line of text\n", 64)) // over 512 bytes, no NUL
	sparseFile(t, filepath.Join(root, "dump.txt"), head, largeFileBytes)

	alloc, result := searchAllocDelta(t, a, `{"pattern":"memory store"}`)

	if !strings.Contains(result, "notes.md:1:the memory store is retired") {
		t.Errorf("the small file's match is missing from the result: %q", result)
	}
	if alloc > searchAllocLimit {
		t.Errorf("search_files allocated %d MiB while searching a workspace with one %d MiB text-like file; the limit is %d MiB",
			alloc>>20, largeFileBytes>>20, searchAllocLimit>>20)
	}
}

// A file the search skips for its size is reported, so the model does not take
// "no matches" for "not there". Binary files are not reported: a workspace is
// full of them and the note would drown the result.
func TestSearchFiles_ReportsLargeFilesItSkipped(t *testing.T) {
	a, root := makeTestAgent(t)
	os.WriteFile(filepath.Join(root, "notes.md"), []byte("the memory store is retired\n"), 0o644)
	head := []byte(strings.Repeat("a line of text\n", 64))
	sparseFile(t, filepath.Join(root, "dump.txt"), head, largeFileBytes)
	sparseFile(t, filepath.Join(root, "model.gguf"), []byte("GGUF\x00\x00\x00"), largeFileBytes)

	_, result := searchAllocDelta(t, a, `{"pattern":"memory store"}`)

	if !strings.Contains(result, "skipped") || !strings.Contains(result, "dump.txt") {
		t.Errorf("the result does not say dump.txt was skipped for its size: %q", result)
	}
	if strings.Contains(result, "model.gguf") {
		t.Errorf("a binary file should be passed over without a note: %q", result)
	}

	_, result = searchAllocDelta(t, a, `{"pattern":"no such text anywhere"}`)
	if !strings.Contains(result, "no matches") || !strings.Contains(result, "dump.txt") {
		t.Errorf("no matches must still name the skipped file, so it is not read as absence: %q", result)
	}
}

// A file under the size limit is searched all the way to its last line: the fix
// must not turn into skipping everything that is not tiny.
func TestSearchFiles_MidSizeTextFileIsStillSearched(t *testing.T) {
	a, root := makeTestAgent(t)
	body := strings.Repeat("filler line of ordinary text\n", 40000) + "needle at the very end\n" // about 1.1 MiB
	os.WriteFile(filepath.Join(root, "mid.txt"), []byte(body), 0o644)

	_, result := searchAllocDelta(t, a, `{"pattern":"needle"}`)

	if !strings.Contains(result, "mid.txt:40001:needle at the very end") {
		t.Errorf("a 1 MiB text file was not searched to its end: %q", result)
	}
}
