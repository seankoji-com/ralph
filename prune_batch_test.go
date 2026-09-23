package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneRunsMissingRootAndUnreadableRoot(t *testing.T) {
	c := Config{StateDir: t.TempDir()}
	if removed, kept, err := pruneRuns(c, time.Now()); removed != 0 || kept != 0 || err != nil {
		t.Fatalf("missing root: %d %d %v", removed, kept, err)
	}
	if err := os.WriteFile(filepath.Join(c.StateDir, "runs"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := pruneRuns(c, time.Now()); err == nil {
		t.Fatal("invalid runs root silently accepted")
	}
}

func TestPruneRunsPreservesMalformedRunsAndIgnoresFiles(t *testing.T) {
	c := Config{StateDir: t.TempDir()}
	root := filepath.Join(c.StateDir, "runs")
	dir := filepath.Join(root, "broken")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	removed, kept, err := pruneRuns(c, time.Now())
	if removed != 0 || kept != 1 || err != nil {
		t.Fatalf("prune: %d %d %v", removed, kept, err)
	}
	for _, path := range []string{filepath.Join(dir, "run.json"), filepath.Join(root, "notes")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("lost %s: %v", path, err)
		}
	}
}
