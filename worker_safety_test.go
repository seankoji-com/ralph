package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWorkerAbortDuringIteration(t *testing.T) {
	r := fixtureRun(t, `touch ../agent-started
sleep 30 &
wait
touch ../should-not-finish`, 3)
	done := make(chan error, 1)
	go func() { done <- worker(r.Dir) }()
	deadline := time.After(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(r.Dir, "agent-started")); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("worker finished early: %v", err)
		case <-deadline:
			t.Fatal("agent did not start")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err := requestAbort(r); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("abort did not interrupt the agent")
	}
	var got Run
	if err := readJSON(filepath.Join(r.Dir, "run.json"), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "aborted" || got.Iteration != 1 {
		t.Fatalf("state=%+v", got)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "should-not-finish")); !os.IsNotExist(err) {
		t.Fatal("agent continued")
	}
	if _, err := os.Stat(r.Worktree); err != nil {
		t.Fatal("aborted worktree lost", err)
	}
}

func TestCompletionCannotComeFromLogs(t *testing.T) {
	r := fixtureRun(t, `printf '%s\n' "$@"
printf '<ralph>COMPLETE</ralph>\n'
printf '{"token":"old","iteration":1}' > .ralph-complete.json`, 1)
	if err := worker(r.Dir); err != nil {
		t.Fatal(err)
	}
	var got Run
	_ = readJSON(filepath.Join(r.Dir, "run.json"), &got)
	if got.Status != "budget reached" {
		t.Fatalf("false completion: %s", got.Status)
	}
}

func TestRedactionAcrossWrites(t *testing.T) {
	var b bytes.Buffer
	w := &redactingWriter{dst: &b}
	for _, chunk := range []string{"failure: https://user:", "secret@example.com/private\n", "ssh://token@host/repo"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "secret") || strings.Contains(b.String(), "token") || strings.Count(b.String(), "[redacted]") != 2 {
		t.Fatal(b.String())
	}
	_, err := command(context.Background(), "", "sh", "-c", "echo https://user:secret@example.com >&2; exit 1")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe command error: %v", err)
	}
}

func TestPrunePreservesWork(t *testing.T) {
	for _, kind := range []string{"clean", "dirty", "ignored", "unmerged", "active", "recent", "locked"} {
		t.Run(kind, func(t *testing.T) {
			r := fixtureRun(t, "echo done", 1)
			// Match the production branch naming invariant.
			r.Branch = "codex/ralph-" + r.ID
			_ = writeJSON(filepath.Join(r.Dir, "run.json"), r)
			if err := worker(r.Dir); err != nil {
				t.Fatal(err)
			}
			_ = readJSON(filepath.Join(r.Dir, "run.json"), &r)
			r.Updated = time.Now().Add(-40 * 24 * time.Hour)
			switch kind {
			case "dirty":
				_ = os.WriteFile(filepath.Join(r.Worktree, "precious"), []byte("keep me"), 0600)
			case "ignored":
				_ = os.WriteFile(filepath.Join(r.Worktree, ".gitignore"), []byte("precious\n.gitignore\n"), 0600)
				_ = os.WriteFile(filepath.Join(r.Worktree, "precious"), []byte("keep me"), 0600)
			case "unmerged":
				_, err := command(context.Background(), r.Worktree, "git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.com", "commit", "--allow-empty", "-m", "keep commit")
				if err != nil {
					t.Fatal(err)
				}
			case "active":
				r.Status = "running"
			case "recent":
				r.Updated = time.Now()
			case "locked":
				f, err := os.OpenFile(filepath.Join(r.Dir, "worker.lock"), os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
					t.Fatal(err)
				}
			}
			_ = writeJSON(filepath.Join(r.Dir, "run.json"), r)
			err := pruneRun(r.Dir, time.Now().Add(-30*24*time.Hour))
			if kind == "clean" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err = os.Stat(r.Dir); !os.IsNotExist(err) {
					t.Fatal("old run remains")
				}
			} else {
				if err == nil {
					t.Fatal("unsafe prune allowed")
				}
				if _, err = os.Stat(r.Worktree); err != nil {
					t.Fatal("worktree lost")
				}
			}
		})
	}
}

func TestModelCannotBeAFlag(t *testing.T) {
	if (RunOptions{Max: 1, Timeout: 1, Model: "--help"}).validate() == nil {
		t.Fatal("model flag accepted")
	}
}
