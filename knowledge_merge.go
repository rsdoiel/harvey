package harvey

import (
	"database/sql"
	"fmt"
	"os"
)

/** NameCollision is a projects.name or concepts.name value that exists
 * independently in both source databases under two different uuids —
 * almost certainly the same real-world entity, created before the UUID
 * migration, now indistinguishable by name alone.
 *
 * Example:
 *   collisions, _ := CollisionReport("a.db", "b.db")
 *   for _, c := range collisions {
 *       fmt.Printf("%s %q: %s vs %s\n", c.Table, c.Name, c.UUIDA, c.UUIDB)
 *   }
 */
type NameCollision struct {
	Table string // "projects" or "concepts"
	Name  string
	UUIDA string
	UUIDB string
}

/** CollisionReport opens aPath and bPath read-only and reports every
 * projects/concepts name that exists in both under different uuids.
 * Callers should review (and resolve, out of band) any collisions before
 * calling MergeKnowledgeBases — a collision is silently resolved
 * "first insert wins" by MergeKnowledgeBases's INSERT OR IGNORE, which may
 * not be the row the caller wants to keep.
 *
 * Parameters:
 *   aPath (string) — path to the first knowledge.db.
 *   bPath (string) — path to the second knowledge.db.
 *
 * Returns:
 *   []NameCollision — one entry per colliding name; empty if none found.
 *   error            — on database failure.
 *
 * Example:
 *   collisions, err := CollisionReport("/machine-a/knowledge.db", "/machine-b/knowledge.db")
 */
func CollisionReport(aPath, bPath string) ([]NameCollision, error) {
	db, err := sql.Open("sqlite", aPath)
	if err != nil {
		return nil, fmt.Errorf("knowledge: open %s: %w", aPath, err)
	}
	defer db.Close()
	if _, err := db.Exec(`ATTACH DATABASE ? AS b`, bPath); err != nil {
		return nil, fmt.Errorf("knowledge: attach %s: %w", bPath, err)
	}

	var out []NameCollision
	for _, table := range []string{"projects", "concepts"} {
		rows, err := db.Query(
			`SELECT main.` + table + `.name, main.` + table + `.uuid, b.` + table + `.uuid
			 FROM main.` + table + ` JOIN b.` + table + ` USING(name)
			 WHERE main.` + table + `.uuid != b.` + table + `.uuid`,
		)
		if err != nil {
			return nil, fmt.Errorf("knowledge: collision query on %s: %w", table, err)
		}
		for rows.Next() {
			c := NameCollision{Table: table}
			if err := rows.Scan(&c.Name, &c.UUIDA, &c.UUIDB); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, c)
		}
		rows.Close()
	}
	return out, nil
}

/** ReconcileCollisions rewrites, in bPath, the uuid of every row named in
 * collisions to match its counterpart's uuid (UUIDA) — "a" wins. This must
 * be called (and MergeKnowledgeBases must be given the now-rewritten bPath)
 * before merging past a CollisionReport hit: without it, MergeKnowledgeBases
 * resolves a name collision by keeping whichever row it inserts first and
 * silently dropping the other via the uuid/name UNIQUE constraints — which
 * also orphans and drops every child row (observations, links) that pointed
 * at the dropped row through its uuid. Reconciling first means both sides'
 * child rows correctly attach to the single surviving merged parent instead.
 *
 * Parameters:
 *   bPath      (string)          — path to the knowledge.db whose colliding rows will be rewritten.
 *   collisions ([]NameCollision) — the result of CollisionReport(aPath, bPath).
 *
 * Returns:
 *   error — on database failure.
 *
 * Example:
 *   collisions, _ := CollisionReport(aPath, bPath)
 *   if len(collisions) > 0 {
 *       _ = ReconcileCollisions(bPath, collisions)
 *   }
 *   summary, _ := MergeKnowledgeBases(aPath, bPath, mergedPath)
 */
func ReconcileCollisions(bPath string, collisions []NameCollision) error {
	if len(collisions) == 0 {
		return nil
	}
	db, err := sql.Open("sqlite", bPath)
	if err != nil {
		return fmt.Errorf("knowledge: open %s: %w", bPath, err)
	}
	defer db.Close()
	for _, c := range collisions {
		if _, err := db.Exec(
			`UPDATE `+c.Table+` SET uuid = ? WHERE name = ? AND uuid = ?`,
			c.UUIDA, c.Name, c.UUIDB,
		); err != nil {
			return fmt.Errorf("knowledge: reconcile %s %q: %w", c.Table, c.Name, err)
		}
	}
	return nil
}

/** MergeTableSummary reports, per table, how many rows came from each
 * source and how many rows the merged table ended up with (less than
 * FromA+FromB when uuid or name collisions caused an intentional drop).
 */
type MergeTableSummary struct {
	Table  string
	FromA  int
	FromB  int
	Merged int
}

/** MergeKnowledgeBases creates a fresh knowledge base at mergedPath (which
 * must not already exist) containing the set union of aPath and bPath,
 * deduped by uuid (and, for projects/concepts, by the pre-existing name
 * UNIQUE constraint). aPath and bPath are opened read-only via ATTACH;
 * neither is modified.
 *
 * Parameters:
 *   aPath      (string) — path to the first source knowledge.db.
 *   bPath      (string) — path to the second source knowledge.db.
 *   mergedPath (string) — path for the new merged knowledge.db; must not exist.
 *
 * Returns:
 *   []MergeTableSummary — per-table row counts; nil until wired in a later work item.
 *   error                — on database failure, or if mergedPath already exists.
 *
 * Example:
 *   summary, err := MergeKnowledgeBases("/machine-a/knowledge.db", "/machine-b/knowledge.db", "/tmp/merged.db")
 */
func MergeKnowledgeBases(aPath, bPath, mergedPath string) ([]MergeTableSummary, error) {
	if _, err := os.Stat(mergedPath); err == nil {
		return nil, fmt.Errorf("knowledge: merge target %s already exists", mergedPath)
	}

	ws, err := NewWorkspace(os.TempDir())
	if err != nil {
		return nil, err
	}
	kb, err := OpenKnowledgeBase(ws, mergedPath)
	if err != nil {
		return nil, err
	}
	kb.Close()

	db, err := sql.Open("sqlite", mergedPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err := db.Exec(`ATTACH DATABASE ? AS a`, aPath); err != nil {
		return nil, fmt.Errorf("knowledge: attach %s: %w", aPath, err)
	}
	if _, err := db.Exec(`ATTACH DATABASE ? AS b`, bPath); err != nil {
		return nil, fmt.Errorf("knowledge: attach %s: %w", bPath, err)
	}

	parentCols := map[string]string{
		"projects": "name, description, status, created_at, updated_at, uuid, origin_host",
		"concepts": "name, description, created_at, identifier_type, identifier_value, uuid, origin_host",
		"sources":  "title, identifier_type, identifier_value, authors, published_date, publisher, rights, version, retracted, retraction_note, first_seen_at, last_checked_at, uuid, origin_host",
	}
	for _, table := range []string{"projects", "concepts", "sources"} {
		cols := parentCols[table]
		for _, src := range []string{"a", "b"} {
			if _, err := db.Exec(fmt.Sprintf(
				`INSERT OR IGNORE INTO %s (%s) SELECT %s FROM %s.%s`,
				table, cols, cols, src, table,
			)); err != nil {
				return nil, fmt.Errorf("knowledge: merge %s from %s: %w", table, src, err)
			}
		}
	}

	const obsCols = "kind, body, created_at, source_doi, uuid, origin_host"
	for _, src := range []string{"a", "b"} {
		if _, err := db.Exec(fmt.Sprintf(`
			INSERT OR IGNORE INTO observations (project_id, %s)
			SELECT mp.id, o.kind, o.body, o.created_at, o.source_doi, o.uuid, o.origin_host
			FROM %s.observations o
			JOIN %s.projects sp ON sp.id = o.project_id
			JOIN projects mp ON mp.uuid = sp.uuid`,
			obsCols, src, src,
		)); err != nil {
			return nil, fmt.Errorf("knowledge: merge observations from %s: %w", src, err)
		}
	}

	for _, src := range []string{"a", "b"} {
		if _, err := db.Exec(fmt.Sprintf(`
			INSERT OR IGNORE INTO observation_concepts (observation_id, concept_id)
			SELECT mo.id, mc.id
			FROM %s.observation_concepts j
			JOIN %s.observations so ON so.id = j.observation_id
			JOIN %s.concepts     sc ON sc.id = j.concept_id
			JOIN observations mo ON mo.uuid = so.uuid
			JOIN concepts     mc ON mc.uuid = sc.uuid`,
			src, src, src,
		)); err != nil {
			return nil, fmt.Errorf("knowledge: merge observation_concepts from %s: %w", src, err)
		}

		if _, err := db.Exec(fmt.Sprintf(`
			INSERT OR IGNORE INTO project_concepts (project_id, concept_id)
			SELECT mp.id, mc.id
			FROM %s.project_concepts j
			JOIN %s.projects sp ON sp.id = j.project_id
			JOIN %s.concepts sc ON sc.id = j.concept_id
			JOIN projects mp ON mp.uuid = sp.uuid
			JOIN concepts mc ON mc.uuid = sc.uuid`,
			src, src, src,
		)); err != nil {
			return nil, fmt.Errorf("knowledge: merge project_concepts from %s: %w", src, err)
		}

		if _, err := db.Exec(fmt.Sprintf(`
			INSERT OR IGNORE INTO observation_sources (observation_id, source_id, relationship)
			SELECT mo.id, ms.id, j.relationship
			FROM %s.observation_sources j
			JOIN %s.observations so ON so.id = j.observation_id
			JOIN %s.sources      ss ON ss.id = j.source_id
			JOIN observations mo ON mo.uuid = so.uuid
			JOIN sources      ms ON ms.uuid = ss.uuid`,
			src, src, src,
		)); err != nil {
			return nil, fmt.Errorf("knowledge: merge observation_sources from %s: %w", src, err)
		}
	}

	allTables := []string{
		"projects", "concepts", "sources", "observations",
		"observation_concepts", "project_concepts", "observation_sources",
	}
	var summary []MergeTableSummary
	for _, table := range allTables {
		s := MergeTableSummary{Table: table}
		if err := db.QueryRow(`SELECT COUNT(*) FROM a.` + table).Scan(&s.FromA); err != nil {
			return nil, fmt.Errorf("knowledge: count a.%s: %w", table, err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM b.` + table).Scan(&s.FromB); err != nil {
			return nil, fmt.Errorf("knowledge: count b.%s: %w", table, err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&s.Merged); err != nil {
			return nil, fmt.Errorf("knowledge: count merged %s: %w", table, err)
		}
		summary = append(summary, s)
	}

	if _, err := db.Exec(`DETACH DATABASE a`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`DETACH DATABASE b`); err != nil {
		return nil, err
	}
	db.Close()

	kb2, err := OpenKnowledgeBase(ws, mergedPath)
	if err != nil {
		return nil, err
	}
	defer kb2.Close()

	return summary, nil
}
