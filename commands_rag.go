// Package harvey — commands_rag.go implements the /rag slash command family
// for managing RAG stores and ingesting documents.
package harvey

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ─── /rag ────────────────────────────────────────────────────────────────────

// ragIngest chunks and embeds each path into the RAG store.
// ragLargeFileThreshold is the file size above which /rag ingest shows the
// document list and asks for confirmation before starting.
const ragLargeFileThreshold = 1000 * 1024 // 1000 KB

// ragIngestableExts is the set of file extensions eligible for RAG ingestion.
var ragIngestableExts = map[string]bool{
	".md": true, ".txt": true, ".go": true, ".ts": true, ".js": true, ".css": true, ".py": true,
	".Mod": true, ".obn": true, ".pas": true, ".lisp": true, ".bas": true, ".c": true, ".cpp": true,
	".rs": true, ".yaml": true, ".yml": true, ".toml": true, ".sql": true, ".pdf": true,
}

/** ragIngestRemotePrefix lists all ingestable objects under a remote prefix URI
 * using r and ingests each by downloading to a temp file, ingesting, then
 * deleting the temp file immediately. This keeps peak disk usage bounded to a
 * single object regardless of prefix size.
 *
 * Works for any RemoteReader that implements List (sftp://, scp://).
 * HTTP/HTTPS callers should use ragIngestHTTP instead, as HTTP has no directory
 * listing protocol.
 *
 * Parameters:
 *   a       (*Agent)       — Harvey agent; a.Rag must be non-nil.
 *   r       (RemoteReader) — backend to use for List and Get.
 *   scheme  (string)       — URI scheme ("sftp", "scp"); used in progress messages.
 *   uri     (string)       — prefix URI to list and ingest.
 *   embedder (Embedder)    — embedding model for the active store.
 *   out     (io.Writer)    — progress and error output.
 *
 * Example:
 *   r, _ := NewRemoteReader("sftp://host/docs/")
 *   ragIngestRemotePrefix(a, r, "sftp", "sftp://host/docs/", embedder, out)
 */
func ragIngestRemotePrefix(a *Agent, r RemoteReader, scheme, uri string, embedder Embedder, out io.Writer) error {
	objects, err := r.List(context.Background(), uri)
	if err != nil {
		fmt.Fprintf(out, "  ✗ list %s: %v\n", uri, err)
		return defaultClass(ClassUnavailable, fmt.Errorf("list %s: %w", uri, err))
	}
	var ingested int
	var failed error // first failure; every object is still tried
	for _, obj := range objects {
		if obj.IsDir {
			continue
		}
		ext := strings.ToLower(filepath.Ext(obj.URI))
		if !ragIngestableExts[ext] {
			continue
		}
		f, err := os.CreateTemp("", "harvey-"+scheme+"-*"+ext)
		if err != nil {
			fmt.Fprintf(out, "  ✗ temp for %s: %v\n", obj.URI, err)
			failed = keepFirstFailure(failed, AsCreate(fmt.Errorf("temp for %s: %w", obj.URI, err)))
			continue
		}
		tmpPath := f.Name()
		if err := r.Get(context.Background(), obj.URI, f); err != nil {
			f.Close()
			os.Remove(tmpPath)
			fmt.Fprintf(out, "  ✗ download %s: %v\n", obj.URI, err)
			failed = keepFirstFailure(failed, defaultClass(ClassUnavailable, fmt.Errorf("download %s: %w", obj.URI, err)))
			continue
		}
		f.Close()

		fmt.Fprintf(out, "  %s", obj.URI)
		var n int
		if ext == ".pdf" {
			var diagrams []int
			n, diagrams, err = ragIngestPDF(a.Rag, embedder, tmpPath, ProvenanceMeta{})
			if err != nil {
				fmt.Fprintf(out, " — error: %v\n", err)
				failed = keepFirstFailure(failed, defaultClass(ClassUnavailable, fmt.Errorf("%s: %w", obj.URI, err)))
			} else {
				fmt.Fprintf(out, " — %d chunk(s)", n)
				if len(diagrams) > 0 {
					fmt.Fprintf(out, " (%d diagram-only page(s) flagged)", len(diagrams))
				}
				fmt.Fprintln(out)
				ingested += n
			}
		} else {
			n, err = ragIngestFile(a.Rag, embedder, tmpPath, ProvenanceMeta{})
			if err != nil {
				fmt.Fprintf(out, " — error: %v\n", err)
				failed = keepFirstFailure(failed, defaultClass(ClassUnavailable, fmt.Errorf("%s: %w", obj.URI, err)))
			} else {
				fmt.Fprintf(out, " — %d chunk(s)\n", n)
				ingested += n
			}
		}
		os.Remove(tmpPath) // immediate cleanup regardless of ingest outcome
	}
	if ingested > 0 {
		fmt.Fprintf(out, "  %s: ingested %d chunk(s) from %s\n", scheme, ingested, uri)
	}
	return failed
}

/** ragIngestHTTP downloads a single HTTP or HTTPS resource, writes it to a
 * temp file, ingests it into the active RAG store, then deletes the temp file.
 *
 * Parameters:
 *   a        (*Agent)    — Harvey agent; a.Rag must be non-nil.
 *   uri      (string)    — http:// or https:// URL of the resource.
 *   embedder (Embedder)  — embedding model for the active store.
 *   out      (io.Writer) — progress and error output.
 *
 * Example:
 *   ragIngestHTTP(a, "https://example.com/spec.md", embedder, out)
 */
func ragIngestHTTP(a *Agent, uri string, embedder Embedder, out io.Writer) error {
	ext := strings.ToLower(filepath.Ext(uri))
	if ext == "" {
		ext = ".txt" // treat extensionless URLs as plain text
	}
	f, err := os.CreateTemp("", "harvey-http-*"+ext)
	if err != nil {
		fmt.Fprintf(out, "  ✗ temp for %s: %v\n", uri, err)
		return AsCreate(fmt.Errorf("temp for %s: %w", uri, err))
	}
	tmpPath := f.Name()
	r := newHTTPReader()
	if err := r.Get(context.Background(), uri, f); err != nil {
		f.Close()
		os.Remove(tmpPath)
		fmt.Fprintf(out, "  ✗ %s: %v\n", uri, err)
		return defaultClass(ClassUnavailable, fmt.Errorf("%s: %w", uri, err))
	}
	f.Close()
	defer os.Remove(tmpPath)

	fmt.Fprintf(out, "  %s", uri)
	var n int
	if ext == ".pdf" {
		var diagrams []int
		n, diagrams, err = ragIngestPDF(a.Rag, embedder, tmpPath, ProvenanceMeta{})
		if err != nil {
			fmt.Fprintf(out, " — error: %v\n", err)
			return defaultClass(ClassUnavailable, fmt.Errorf("%s: %w", uri, err))
		}
		fmt.Fprintf(out, " — %d chunk(s)", n)
		if len(diagrams) > 0 {
			fmt.Fprintf(out, " (%d diagram-only page(s) flagged)", len(diagrams))
		}
		fmt.Fprintln(out)
	} else {
		n, err = ragIngestFile(a.Rag, embedder, tmpPath, ProvenanceMeta{})
		if err != nil {
			fmt.Fprintf(out, " — error: %v\n", err)
			return defaultClass(ClassUnavailable, fmt.Errorf("%s: %w", uri, err))
		}
		fmt.Fprintf(out, " — %d chunk(s)\n", n)
	}
	if n > 0 {
		fmt.Fprintf(out, "  http: ingested %d chunk(s) from %s\n", n, uri)
	}
	return nil
}

// ragIngestFile reads a file, splits it into chunks (language-aware when a
// chunker is registered, paragraph-based otherwise), and ingests them.
// Binary files are silently skipped (returns 0, nil).
func ragIngestFile(store *RagStore, embedder Embedder, path string, meta ProvenanceMeta) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	// Skip binary files silently.
	if !isTextContent(data) {
		return 0, nil
	}

	// Use filepath.Ext directly (case-preserving) so .Mod → oberon and .mod → go.
	ext := filepath.Ext(path)
	langID, _ := globalRegistry.DetectFromExtension(ext)

	var enriched []EnrichedChunk
	if langID != "" {
		if chunker := globalRegistry.GetChunker(langID); chunker != nil {
			enriched = chunker.Chunk(string(data), path)
		}
	}

	// Fallback: generic paragraph chunking wrapped as EnrichedChunks.
	if len(enriched) == 0 {
		for _, text := range ragChunk(string(data)) {
			enriched = append(enriched, EnrichedChunk{
				Content:   text,
				ChunkType: "code",
			})
		}
	}

	if len(enriched) == 0 {
		return 0, nil
	}

	// Populate Docs fields using documentation extractor when available.
	if langID != "" {
		if extractor := globalRegistry.GetExtractor(langID); extractor != nil {
			symDocs := extractor.ExtractSymbols(string(data))
			for i := range enriched {
				if len(enriched[i].Symbols) > 0 {
					if doc, ok := symDocs[enriched[i].Symbols[0]]; ok {
						enriched[i].Docs = doc
					}
				}
			}
		}
	}

	if err := store.IngestEnriched(path, enriched, embedder, meta); err != nil {
		return 0, err
	}
	return len(enriched), nil
}

// ragIngestPDF extracts text from a PDF with pdfExtract and ingests it into
// store. If the document looks like a scholarly paper (isPaperLike), it is
// chunked by section via scholarlyChunk and ingested with IngestEnriched so
// each chunk carries its section type and the document's scholarly
// identifiers/citations. Otherwise it falls back to flat per-page chunks,
// each prefixed with the document title and page number so retrieved context
// always carries its provenance. Diagram-only pages (sparse text, no raster
// images) are stored with an incomplete-content marker so retrieval results
// can surface the caveat.
// Returns (chunkCount, diagramPageNumbers, error).
func ragIngestPDF(store *RagStore, embedder Embedder, path string, meta ProvenanceMeta) (int, []int, error) {
	result, err := pdfExtract(path, "")
	if err != nil {
		return 0, nil, err
	}

	title := filepath.Base(path)
	if result.Info.Title != "" {
		title = result.Info.Title
	}

	diagramSet := make(map[int]bool, len(result.DiagramPages))
	for _, p := range result.DiagramPages {
		diagramSet[p] = true
	}

	pageTexts := strings.Split(result.Text, "\f")
	for len(pageTexts) > 0 && strings.TrimSpace(pageTexts[len(pageTexts)-1]) == "" {
		pageTexts = pageTexts[:len(pageTexts)-1]
	}

	if isPaperLike(pageTexts) {
		chunks := scholarlyChunk(pageTexts, title, diagramSet)
		if len(chunks) == 0 {
			return 0, result.DiagramPages, nil
		}
		if err := store.IngestEnriched(path, chunks, embedder, meta); err != nil {
			return 0, nil, err
		}
		return len(chunks), result.DiagramPages, nil
	}

	totalPages := result.Info.Pages
	if totalPages == 0 {
		totalPages = len(pageTexts)
	}

	var allChunks []string
	for i, pageText := range pageTexts {
		pageNum := i + 1
		isDiagram := diagramSet[pageNum]

		header := fmt.Sprintf("[PDF: %q, page %d of %d]", title, pageNum, totalPages)
		if isDiagram {
			header += diagramPageWarning
		}

		chunks := ragChunk(pageText)
		if len(chunks) == 0 {
			if isDiagram {
				// Store a placeholder so the diagram page is retrievable.
				allChunks = append(allChunks, header)
			}
			continue
		}
		for _, chunk := range chunks {
			allChunks = append(allChunks, header+"\n\n"+chunk)
		}
	}

	if len(allChunks) == 0 {
		return 0, result.DiagramPages, nil
	}
	if err := store.Ingest(path, allChunks, embedder, meta); err != nil {
		return 0, nil, err
	}
	return len(allChunks), result.DiagramPages, nil
}


