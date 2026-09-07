package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Pruning is opt-in. Failed safety checks preserve the entire run, including
// logs, so the user can inspect and recover it with ordinary Git tooling.
func pruneRuns(c Config, before time.Time) (removed, kept int, err error) {
	root := filepath.Join(c.StateDir, "runs")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if pruneRun(dir, before) == nil {
			removed++
		} else {
			kept++
		}
	}
	return removed, kept, nil
}

func pruneRun(dir string, before time.Time) error {
	lock, err := os.OpenFile(filepath.Join(dir, "worker.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	var r Run
	if err = readJSON(filepath.Join(dir, "run.json"), &r); err != nil {
		return err
	}
	switch r.Status {
	case "complete", "failed", "stopped", "aborted", "budget reached":
	default:
		return fmt.Errorf("run is not finished")
	}
	if r.Updated.IsZero() || !r.Updated.Before(before) {
		return fmt.Errorf("run is recent")
	}
	if r.ID != filepath.Base(dir) || r.Branch != "codex/ralph-"+r.ID || r.Worktree != filepath.Join(dir, "worktree") {
		return fmt.Errorf("unexpected run paths")
	}
	// Refuse symlink run entries and worktrees. The configured state root may
	// have system aliases such as macOS /var -> /private/var.
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("run directory is missing or a symlink")
	}
	info, err := os.Lstat(r.Worktree)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("worktree is missing or not a directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	branch, err := command(ctx, r.Worktree, "git", "symbolic-ref", "--short", "HEAD")
	if err != nil || branch != r.Branch {
		return fmt.Errorf("worktree branch changed")
	}
	status, err := command(ctx, r.Worktree, "git", "status", "--porcelain", "--ignored")
	if err != nil || status != "" {
		return fmt.Errorf("worktree contains local files or changes")
	}
	base, err := command(ctx, r.Repo.Path, "git", "symbolic-ref", "refs/remotes/origin/HEAD")
	if err != nil || !strings.HasPrefix(base, "refs/remotes/origin/") {
		return fmt.Errorf("default branch is unknown")
	}
	if _, err = command(ctx, r.Repo.Path, "git", "merge-base", "--is-ancestor", r.Branch, base); err != nil {
		return fmt.Errorf("branch contains unmerged commits")
	}
	if _, err = command(ctx, r.Repo.Path, "git", "worktree", "remove", r.Worktree); err != nil {
		return err
	}
	if _, err = command(ctx, r.Repo.Path, "git", "branch", "-d", r.Branch); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
