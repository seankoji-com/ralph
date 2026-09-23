package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalReposFiltersOwnerAndRemoteAndSorts(t *testing.T) {
	root := t.TempDir()
	for name, remote := range map[string]string{
		"z":       "git@github.com:Owner/zulu.git",
		"a":       "https://github.com/owner/alpha.git",
		"foreign": "https://github.com/elsewhere/repo.git",
		"host":    "https://gitlab.com/owner/repo.git",
		"missing": "",
	} {
		dir := filepath.Join(root, name)
		if _, err := command(context.Background(), root, "git", "init", "-q", dir); err != nil {
			t.Fatal(err)
		}
		if remote != "" {
			if _, err := command(context.Background(), dir, "git", "remote", "add", "origin", remote); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.Mkdir(filepath.Join(root, "not-git"), 0700); err != nil {
		t.Fatal(err)
	}
	got := localRepos(Config{ReposDir: root, Org: "OWNER"})
	if len(got) != 2 || got[0].Name != "Owner/zulu" || got[1].Name != "owner/alpha" {
		t.Fatalf("repos=%+v", got)
	}
	for _, repo := range got {
		if repo.Path == "" {
			t.Fatal("local path lost")
		}
	}
}

func TestPruneRunsRemovesSafeFinishedRunAndRetainsBrokenSibling(t *testing.T) {
	r := fixtureRun(t, "echo done", 1)
	r.Branch = "codex/ralph-" + r.ID
	if err := writeJSON(filepath.Join(r.Dir, "run.json"), r); err != nil {
		t.Fatal(err)
	}
	if err := worker(r.Dir); err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(filepath.Dir(r.Dir))
	broken := filepath.Join(root, "runs", "broken")
	if err := os.Mkdir(broken, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "run.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	removed, kept, err := pruneRuns(Config{StateDir: root}, time.Now())
	if removed != 1 || kept != 1 || err != nil {
		t.Fatalf("removed=%d kept=%d err=%v", removed, kept, err)
	}
	for _, path := range []string{r.Dir, r.Worktree} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("not removed: %s %v", path, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(r.Repo.Path, "README"))
	if err != nil || string(data) != "uncommitted user work\n" {
		t.Fatal("original checkout damaged")
	}
}
