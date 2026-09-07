package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWorkerAbortDuringIteration(t *testing.T) {
	r := fixtureRun(t, `touch "__RUN_DIR__/agent-started"
sleep 30 &
wait
touch "__RUN_DIR__/should-not-finish"`, 3)
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

func TestLargeStreamingOutput(t *testing.T) {
	plain := strings.Repeat("x", 2*1024*1024)
	for _, input := range []string{plain, "https://user:" + strings.Repeat("secret", 20000) + "@example.com\n" + plain} {
		var b bytes.Buffer
		w := &redactingWriter{dst: &b}
		if _, err := w.Write([]byte(input)); err != nil {
			t.Fatal(err)
		}
		if err := w.Flush(); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(b.String(), plain) || strings.Contains(b.String(), "secret") {
			t.Fatal("lost ordinary output or leaked credential")
		}
	}
}

func TestUnreadableRunRemainsVisible(t *testing.T) {
	c := Config{StateDir: t.TempDir()}
	dir := filepath.Join(c.StateDir, "runs", "broken")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	runs := loadRuns(c, nil)
	if len(runs) != 1 || runs[0].Status != "unreadable" || runs[0].Dir != dir {
		t.Fatalf("invisible run: %+v", runs)
	}
}

func TestPruneRetriesAfterWorktreeRemoval(t *testing.T) {
	r := fixtureRun(t, "echo done", 1)
	r.Branch = "codex/ralph-" + r.ID
	_ = writeJSON(filepath.Join(r.Dir, "run.json"), r)
	if err := worker(r.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := command(context.Background(), r.Repo.Path, "git", "worktree", "remove", r.Worktree); err != nil {
		t.Fatal(err)
	}
	if err := pruneRun(r.Dir, time.Now()); err != nil {
		t.Fatal("partial cleanup not retryable", err)
	}
	if _, err := os.Stat(r.Dir); !os.IsNotExist(err) {
		t.Fatal("run retained after successful retry")
	}
}

func TestPrunePreservesWork(t *testing.T) {
	for _, kind := range []string{"clean", "interrupted", "stale", "dirty", "ignored", "unmerged", "active", "recent", "locked"} {
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
			case "interrupted":
				r.Status = "interrupted"
			case "stale":
				r.Status = "running"
			case "active":
				r.Status = "running"
				r.Updated = time.Now()
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
			if kind == "clean" || kind == "interrupted" || kind == "stale" {
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

func TestStopRequestStaysLatched(t *testing.T) {
	r := fixtureRun(t, `touch "__RUN_DIR__/agent-started"
while [ ! -f "__RUN_DIR__/release" ]; do sleep 0.1; done`, 3)
	done := make(chan error, 1)
	go func() { done <- worker(r.Dir) }()
	wait := func(ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !ready() {
			if time.Now().After(deadline) {
				_ = requestAbort(r)
				t.Fatal("worker did not acknowledge request")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	wait(func() bool { _, err := os.Stat(filepath.Join(r.Dir, "agent-started")); return err == nil })
	if err := requestStop(r); err != nil {
		t.Fatal(err)
	}
	wait(func() bool {
		var got Run
		return readJSON(filepath.Join(r.Dir, "run.json"), &got) == nil && got.StopRequested
	})
	if err := os.Remove(filepath.Join(r.Dir, "STOP")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("latched stop lost")
	}
	var got Run
	_ = readJSON(filepath.Join(r.Dir, "run.json"), &got)
	if got.Status != "stopped" || got.Iteration != 1 || !got.StopRequested {
		t.Fatalf("state=%+v", got)
	}
}

func TestWorkerShutdownGrace(t *testing.T) {
	if dir := os.Getenv("RALPH_TEST_WORKER_DIR"); dir != "" {
		if err := worker(dir); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	r := fixtureRun(t, `trap 'printf flushed > .ralph-ledger.md; exit 0' TERM
touch "__RUN_DIR__/agent-started"
while :; do sleep 1; done`, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWorkerShutdownGrace$")
	cmd.Env = append(os.Environ(), "RALPH_TEST_WORKER_DIR="+r.Dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	for {
		if _, err := os.Stat(filepath.Join(r.Dir, "agent-started")); err == nil {
			break
		}
		if ctx.Err() != nil {
			_ = cmd.Wait()
			t.Fatal("worker did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(r.Worktree, ".ralph-ledger.md"))
	if err != nil || string(b) != "flushed" {
		t.Fatalf("agent could not flush: %q %v", b, err)
	}
	var got Run
	_ = readJSON(filepath.Join(r.Dir, "run.json"), &got)
	if got.Status != "interrupted" {
		t.Fatalf("state=%s", got.Status)
	}
}

func TestGuardianStopsAgentAfterWorkerSIGKILL(t *testing.T) {
	r := fixtureRun(t, `trap 'sleep 1; exit 0' TERM
touch "__RUN_DIR__/agent-started"
while :; do sleep 1; done`, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWorkerShutdownGrace$")
	cmd.Env = append(os.Environ(), "RALPH_TEST_WORKER_DIR="+r.Dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	var agent agentRecord
	for {
		_, ready := os.Stat(filepath.Join(r.Dir, "agent-started"))
		if ready == nil && readJSON(filepath.Join(r.Dir, "agent.json"), &agent) == nil {
			break
		}
		if ctx.Err() != nil {
			_ = cmd.Wait()
			t.Fatal("agent did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	// Make the record prune-eligible while the guardian is still stopping the
	// live agent. The independent lease must veto deletion, even on a clean tree.
	r.Status = "interrupted"
	r.Updated = time.Now().Add(-time.Hour)
	if err := writeJSON(filepath.Join(r.Dir, "run.json"), r); err != nil {
		t.Fatal(err)
	}
	if err := pruneRun(r.Dir, time.Now()); err == nil {
		t.Fatal("pruned beneath a live agent")
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(-agent.PGID, 0) != syscall.ESRCH {
		if time.Now().After(deadline) {
			t.Fatal("agent survived worker SIGKILL")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(r.Worktree); err != nil {
		t.Fatal("worktree lost", err)
	}
}
