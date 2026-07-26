// kbmerge merges two Harvey knowledge.db files (from two machines that have
// drifted independently) into a fresh, deduped output database. It never
// modifies its inputs and never replaces a live knowledge.db automatically —
// it prints the merged path and leaves placing it into position to the user.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	harvey "github.com/rsdoiel/harvey"

	_ "github.com/glebarez/go-sqlite"
)

func main() {
	aPath := flag.String("a", "", "path to the first knowledge.db")
	bPath := flag.String("b", "", "path to the second knowledge.db")
	outPath := flag.String("out", "", "path for the merged knowledge.db (must not exist)")
	force := flag.Bool("force", false, "resolve name/uuid collisions by reconciling b's identity to a's, instead of aborting")
	flag.Parse()

	if *aPath == "" || *bPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "usage: kbmerge -a PATH -b PATH -out PATH [-force]")
		os.Exit(2)
	}

	if err := run(*aPath, *bPath, *outPath, *force, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "kbmerge:", err)
		os.Exit(1)
	}
}

func run(aPath, bPath, outPath string, force bool, out io.Writer) error {
	scratch, err := os.MkdirTemp("", "kbmerge-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)

	scratchA := filepath.Join(scratch, "a.db")
	scratchB := filepath.Join(scratch, "b.db")
	if err := checkpointAndCopy(aPath, scratchA); err != nil {
		return fmt.Errorf("checkpoint+copy %s: %w", aPath, err)
	}
	if err := checkpointAndCopy(bPath, scratchB); err != nil {
		return fmt.Errorf("checkpoint+copy %s: %w", bPath, err)
	}

	collisions, err := harvey.CollisionReport(scratchA, scratchB)
	if err != nil {
		return fmt.Errorf("collision report: %w", err)
	}
	if len(collisions) > 0 {
		fmt.Fprintf(out, "%d name/uuid collision(s) found:\n", len(collisions))
		for _, c := range collisions {
			fmt.Fprintf(out, "  %-10s %-30s %s vs %s\n", c.Table, c.Name, c.UUIDA, c.UUIDB)
		}
		if !force {
			return fmt.Errorf("aborting: resolve collisions or pass -force to reconcile b's identity to a's for each one")
		}
		if err := harvey.ReconcileCollisions(scratchB, collisions); err != nil {
			return fmt.Errorf("reconcile collisions: %w", err)
		}
		fmt.Fprintf(out, "-force set: reconciled %d collision(s) — b's rows now share a's identity, so their observations/links merge normally instead of being dropped\n", len(collisions))
	}

	summary, err := harvey.MergeKnowledgeBases(scratchA, scratchB, outPath)
	if err != nil {
		return fmt.Errorf("merge: %w", err)
	}

	fmt.Fprintf(out, "%-24s %6s %6s %6s\n", "table", "from_a", "from_b", "merged")
	for _, s := range summary {
		fmt.Fprintf(out, "%-24s %6d %6d %6d\n", s.Table, s.FromA, s.FromB, s.Merged)
	}
	fmt.Fprintf(out, "\nmerged knowledge base written to %s\n", outPath)
	fmt.Fprintln(out, "review it, then copy it into place over each machine's agents/knowledge.db yourself.")
	return nil
}

// checkpointAndCopy checkpoints srcPath's WAL (so all committed data is in
// the main file) and copies it — plus any -wal/-shm sidecars if still
// present — to dstPath.
func checkpointAndCopy(srcPath, dstPath string) error {
	db, err := sql.Open("sqlite", srcPath)
	if err != nil {
		return err
	}
	if _, err := db.Exec(`PRAGMA wal_checkpoint(FULL)`); err != nil {
		db.Close()
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}

	if err := copyFile(srcPath, dstPath); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := copyFile(srcPath+suffix, dstPath+suffix); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func copyFile(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}
