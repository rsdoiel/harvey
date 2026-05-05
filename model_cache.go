// Package harvey — model_cache.go provides SQLite-backed caching of Ollama
// model capability metadata. The cache stores probe results (model family,
// parameter size, quantization, context length, and capability flags) to avoid
// re-probing models on every Harvey startup. This significantly speeds up
// initialization when working with many models.
//
// The cache is stored in harvey/model_cache.db and is automatically created
// on first use. Each entry is scoped to a single model name and tracks when
// it was last probed and at what level ("fast" heuristic or "thorough" live test).
//
// See OpenModelCache for the entry point and FastProbeModel/ThoroughProbeModel
// in ollama.go for the probing implementations.

package harvey

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

const modelCacheSchema = `
CREATE TABLE IF NOT EXISTS model_capabilities (
    name           TEXT PRIMARY KEY,
    family         TEXT    NOT NULL DEFAULT '',
    parameter_size TEXT    NOT NULL DEFAULT '',
    quantization   TEXT    NOT NULL DEFAULT '',
    size_bytes     INTEGER NOT NULL DEFAULT 0,
    context_length INTEGER NOT NULL DEFAULT 0,
    supports_tools INTEGER NOT NULL DEFAULT -1,
    supports_embed INTEGER NOT NULL DEFAULT -1,
    probe_level    TEXT    NOT NULL DEFAULT 'none',
    probed_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
`

/** CapabilityStatus records whether a model capability has been confirmed,
 * denied, or not yet probed.
 *
 * Values:
 *   CapUnknown (-1) — not yet probed.
 *   CapNo      (0)  — confirmed absent.
 *   CapYes     (1)  — confirmed present.
 *
 * Example:
 *   if cap.SupportsTools == CapYes { fmt.Println("tools supported") }
 */
type CapabilityStatus int

const (
	CapUnknown CapabilityStatus = -1
	CapNo      CapabilityStatus = 0
	CapYes     CapabilityStatus = 1
)

/** String returns a display symbol for the capability status:
 * "✓" for yes, "—" for no, "?" for unknown.
 *
 * Returns:
 *   string — one of "✓", "—", "?".
 *
 * Example:
 *   fmt.Println(CapYes.String()) // "✓"
 */
func (c CapabilityStatus) String() string {
	switch c {
	case CapYes:
		return "✓"
	case CapNo:
		return "—"
	default:
		return "?"
	}
}

/** ModelCapability holds the cached capability metadata for a single Ollama
 * model. It is stored in harvey/model_cache.db and populated by
 * FastProbeModel or ThoroughProbeModel.
 *
 * Fields:
 *   Name          (string)           — full model identifier, e.g. "llama3.2:latest".
 *   Family        (string)           — model family, e.g. "llama".
 *   ParameterSize (string)           — human-readable size, e.g. "8.0B".
 *   Quantization  (string)           — quantization level, e.g. "Q4_K_M".
 *   SizeBytes     (int64)            — bytes on disk.
 *   ContextLength (int)              — context window in tokens; 0 = unknown.
 *   SupportsTools (CapabilityStatus) — whether the model supports tool/function calling.
 *   SupportsEmbed (CapabilityStatus) — whether the model can produce embeddings.
 *   ProbeLevel    (string)           — "none", "fast", or "thorough".
 *   ProbedAt      (time.Time)        — when the last probe ran.
 *
 * Example:
 *   caps, _ := cache.Get("llama3.2:latest")
 *   fmt.Printf("tools: %s  embed: %s\n", caps.SupportsTools, caps.SupportsEmbed)
 */
type ModelCapability struct {
	Name          string
	Family        string
	ParameterSize string
	Quantization  string
	SizeBytes     int64
	ContextLength int
	SupportsTools CapabilityStatus
	SupportsEmbed CapabilityStatus
	ProbeLevel    string
	ProbedAt      time.Time
}

/** ModelCache is a SQLite3-backed store for Ollama model capability metadata.
 * The database file lives at harvey/model_cache.db inside the workspace and
 * is created automatically on first use.
 *
 * Example:
 *   mc, err := OpenModelCache(ws, "")
 *   if err != nil {
 *       log.Fatal(err)
 *   }
 *   defer mc.Close()
 */
type ModelCache struct {
	db   *sql.DB
	path string
}

// Path returns the absolute path of the open model cache file.
func (mc *ModelCache) Path() string { return mc.path }

/** OpenModelCache opens (or creates) the model capability cache. customPath
 * overrides the default location (harvey/model_cache.db inside the workspace);
 * pass an empty string to use the default.
 *
 * Parameters:
 *   ws         (*Workspace) — the Harvey workspace that owns the cache file.
 *   customPath (string)     — override path; empty = harvey/model_cache.db.
 *
 * Returns:
 *   *ModelCache — ready-to-use cache handle.
 *   error       — if the file cannot be opened or the schema cannot be applied.
 *
 * Example:
 *   mc, err := OpenModelCache(ws, "")
 *   if err != nil {
 *       log.Fatal(err)
 *   }
 *   defer mc.Close()
 */
func OpenModelCache(ws *Workspace, customPath string) (*ModelCache, error) {
	var dbPath string
	if customPath != "" {
		if filepath.IsAbs(customPath) {
			dbPath = customPath
		} else {
			var err error
			dbPath, err = ws.AbsPath(customPath)
			if err != nil {
				return nil, err
			}
		}
	} else {
		var err error
		dbPath, err = ws.AbsPath(harveySubdir + "/model_cache.db")
		if err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("model_cache: open %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(modelCacheSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("model_cache: apply schema: %w", err)
	}
	return &ModelCache{db: db, path: dbPath}, nil
}

/** Close releases the database connection. Defer immediately after a
 * successful OpenModelCache call.
 *
 * Returns:
 *   error — from the underlying sql.DB.Close call.
 *
 * Example:
 *   mc, _ := OpenModelCache(ws, "")
 *   defer mc.Close()
 */
func (mc *ModelCache) Close() error {
	return mc.db.Close()
}

/** Get returns the cached ModelCapability for the named model, or nil when
 * no entry exists. The name must be the full model identifier including tag,
 * e.g. "llama3.2:latest".
 *
 * Parameters:
 *   name (string) — full model name.
 *
 * Returns:
 *   *ModelCapability — cached entry; nil if not found.
 *   error            — non-nil on database error (not on missing row).
 *
 * Example:
 *   cap, err := mc.Get("llama3.2:latest")
 *   if cap == nil { fmt.Println("not cached") }
 */
func (mc *ModelCache) Get(name string) (*ModelCapability, error) {
	const q = `
	SELECT name, family, parameter_size, quantization, size_bytes,
	       context_length, supports_tools, supports_embed, probe_level, probed_at
	FROM model_capabilities WHERE name = ?`
	row := mc.db.QueryRow(q, name)
	c, err := scanCapability(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("model_cache: get %s: %w", name, err)
	}
	return c, nil
}

/** Set upserts a ModelCapability into the cache. An existing entry for the
 * same model name is completely replaced. ProbeLevel and ProbedAt are written
 * as supplied; the caller is responsible for setting them correctly.
 *
 * Parameters:
 *   cap (*ModelCapability) — capability record to store.
 *
 * Returns:
 *   error — non-nil on database write failure.
 *
 * Example:
 *   err := mc.Set(&ModelCapability{Name: "llama3.2:latest", SupportsTools: CapYes})
 */
func (mc *ModelCache) Set(cap *ModelCapability) error {
	const q = `
	INSERT INTO model_capabilities
	    (name, family, parameter_size, quantization, size_bytes,
	     context_length, supports_tools, supports_embed, probe_level, probed_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(name) DO UPDATE SET
	    family         = excluded.family,
	    parameter_size = excluded.parameter_size,
	    quantization   = excluded.quantization,
	    size_bytes     = excluded.size_bytes,
	    context_length = excluded.context_length,
	    supports_tools = excluded.supports_tools,
	    supports_embed = excluded.supports_embed,
	    probe_level    = excluded.probe_level,
	    probed_at      = excluded.probed_at`
	_, err := mc.db.Exec(q,
		cap.Name, cap.Family, cap.ParameterSize, cap.Quantization,
		cap.SizeBytes, cap.ContextLength,
		int(cap.SupportsTools), int(cap.SupportsEmbed),
		cap.ProbeLevel, cap.ProbedAt,
	)
	if err != nil {
		return fmt.Errorf("model_cache: set %s: %w", cap.Name, err)
	}
	return nil
}

/** Delete removes the cache entry for the named model. It is a no-op when
 * the model is not in the cache.
 *
 * Parameters:
 *   name (string) — full model name, e.g. "llama3.2:latest".
 *
 * Returns:
 *   error — non-nil on database write failure.
 *
 * Example:
 *   _ = mc.Delete("llama3.2:latest")
 */
func (mc *ModelCache) Delete(name string) error {
	_, err := mc.db.Exec(`DELETE FROM model_capabilities WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("model_cache: delete %s: %w", name, err)
	}
	return nil
}

/** All returns every ModelCapability entry in the cache, ordered by name.
 *
 * Returns:
 *   []ModelCapability — all cached entries; empty slice when the cache is empty.
 *   error             — non-nil on database read failure.
 *
 * Example:
 *   caps, err := mc.All()
 *   for _, c := range caps { fmt.Println(c.Name) }
 */
func (mc *ModelCache) All() ([]ModelCapability, error) {
	const q = `
	SELECT name, family, parameter_size, quantization, size_bytes,
	       context_length, supports_tools, supports_embed, probe_level, probed_at
	FROM model_capabilities ORDER BY name`
	rows, err := mc.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("model_cache: all: %w", err)
	}
	defer rows.Close()

	var out []ModelCapability
	for rows.Next() {
		c, err := scanCapability(rows)
		if err != nil {
			return nil, fmt.Errorf("model_cache: all scan: %w", err)
		}
		out = append(out, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("model_cache: all: %w", err)
	}
	return out, nil
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scanCapability reads one ModelCapability from a scanner.
func scanCapability(s scanner) (*ModelCapability, error) {
	var c ModelCapability
	var tools, embed int
	var probedAt string
	err := s.Scan(
		&c.Name, &c.Family, &c.ParameterSize, &c.Quantization,
		&c.SizeBytes, &c.ContextLength,
		&tools, &embed,
		&c.ProbeLevel, &probedAt,
	)
	if err != nil {
		return nil, err
	}
	c.SupportsTools = CapabilityStatus(tools)
	c.SupportsEmbed = CapabilityStatus(embed)
	c.ProbedAt, _ = time.Parse(time.DateTime, probedAt)
	return &c, nil
}
