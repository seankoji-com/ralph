package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type Run struct {
	ID             string    `json:"id"`
	Repo           Repo      `json:"repo"`
	Prompt         string    `json:"prompt"`
	Model          string    `json:"model"`
	Runner         string    `json:"runner"`
	Status         string    `json:"status"`
	Iteration      int       `json:"iteration"`
	Max            int       `json:"max"`
	Cooldown       int       `json:"cooldown_seconds"`
	Timeout        int       `json:"timeout_minutes"`
	Started        time.Time `json:"started"`
	Updated        time.Time `json:"updated"`
	PID            int       `json:"pid"`
	Worktree       string    `json:"worktree"`
	Branch         string    `json:"branch"`
	Error          string    `json:"error,omitempty"`
	StopRequested  bool      `json:"stop_requested,omitempty"`
	AbortRequested bool      `json:"abort_requested,omitempty"`
	Dir            string    `json:"-"`
	External       bool      `json:"-"`
	LogPath        string    `json:"-"`
}

func (r Run) active() bool {
	return r.Status == "queued" || r.Status == "preparing" || r.Status == "running" || r.Status == "cooldown" || r.Status == "stopping" || r.Status == "aborting" || r.Status == "orphaned"
}
func (r Run) logPath() string {
	if r.External {
		return r.LogPath
	}
	return filepath.Join(r.Dir, "output.log")
}

func launchRun(c Config, repo Repo, prompt string, options RunOptions) (Run, error) {
	if repo.Path == "" {
		return Run{}, fmt.Errorf("clone this repository first")
	}
	if strings.TrimSpace(prompt) == "" {
		return Run{}, fmt.Errorf("provide a prompt")
	}
	if err := options.validate(); err != nil {
		return Run{}, err
	}
	runner, err := exec.LookPath(c.Runner)
	if err != nil {
		return Run{}, fmt.Errorf("runner %s is not on PATH", c.Runner)
	}
	runner, err = filepath.Abs(runner)
	if err != nil {
		return Run{}, err
	}
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Run{}, err
	}
	id := time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(random[:])
	dir := filepath.Join(c.StateDir, "runs", id)
	r := Run{ID: id, Repo: repo, Prompt: prompt, Model: options.Model, Runner: runner, Status: "queued", Max: options.Max, Cooldown: options.Cooldown, Timeout: options.Timeout, Started: time.Now(), Updated: time.Now(), Dir: dir, Branch: "codex/ralph-" + id, Worktree: filepath.Join(c.StateDir, "worktrees", id)}
	if err := writeJSON(filepath.Join(dir, "run.json"), r); err != nil {
		return r, err
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte(prompt), 0600); err != nil {
		return r, err
	}
	exe, err := os.Executable()
	if err != nil {
		return r, err
	}
	log, err := os.OpenFile(r.logPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return r, err
	}
	defer log.Close()
	cmd := exec.Command(exe, "worker", dir)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err != nil {
		r.Status = "failed"
		r.Error = err.Error()
		_ = writeJSON(filepath.Join(dir, "run.json"), r)
		return r, err
	}
	// Only the worker writes its state after spawning. This avoids a startup race.
	go func() { _ = cmd.Wait() }()
	return r, nil
}

func requestStop(r Run) error {
	if r.Status == "orphaned" {
		return fmt.Errorf("worker unavailable; inspect %s before manual agent recovery", filepath.Join(r.Dir, "agent.json"))
	}
	if r.External {
		return fmt.Errorf("external logs are read-only; use the script's STOP file")
	}
	if !r.active() {
		return fmt.Errorf("this run is not active")
	}
	return os.WriteFile(filepath.Join(r.Dir, "STOP"), []byte("stop after current iteration\n"), 0600)
}

func requestAbort(r Run) error {
	if r.Status == "orphaned" {
		return fmt.Errorf("guardian could not finish recovery; inspect %s before manually stopping the process group", filepath.Join(r.Dir, "agent.json"))
	}
	if r.External || !r.active() {
		return fmt.Errorf("only active native runs can be stopped immediately")
	}
	return os.WriteFile(filepath.Join(r.Dir, "ABORT"), []byte("stop now\n"), 0600)
}

func loadRuns(c Config, repos []Repo) []Run {
	entries, _ := os.ReadDir(filepath.Join(c.StateDir, "runs"))
	runs := []Run{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(c.StateDir, "runs", entry.Name())
		var r Run
		if err := readJSON(filepath.Join(dir, "run.json"), &r); err != nil {
			r = Run{ID: entry.Name(), Dir: dir, Repo: Repo{Name: "Unknown repository"}, Status: "unreadable", Error: "Run metadata could not be read: " + redactCredentials(err.Error()) + ". Logs are preserved at " + dir + "; inspect this directory to recover the run."}
			if info, err := entry.Info(); err == nil {
				r.Started, r.Updated = info.ModTime(), info.ModTime()
			}
			runs = append(runs, r)
			continue
		}
		r.Dir = dir
		if r.active() {
			last := r.Updated
			if info, err := os.Stat(filepath.Join(dir, "heartbeat")); err == nil {
				last = info.ModTime()
			}
			if time.Since(last) > 20*time.Second {
				r = recoverInterrupted(r)
			} else if _, err := os.Stat(filepath.Join(dir, "ABORT")); err == nil || r.AbortRequested {
				r.Status = "aborting"
			} else if _, err := os.Stat(filepath.Join(dir, "STOP")); err == nil || r.StopRequested {
				r.Status = "stopping"
			}
		}
		runs = append(runs, r)
	}
	// Existing scripts have no state protocol. Report activity, never invent a PID or success.
	for _, repo := range repos {
		if repo.Path == "" {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(repo.Path, ".ralph/logs/*.log"))
		groups := map[string]Run{}
		for _, path := range files {
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			base := filepath.Base(path)
			key := strings.TrimSuffix(base, filepath.Ext(base))
			if i := strings.LastIndex(key, "-"); i > 0 {
				key = key[:i]
			}
			r := Run{ID: repo.Name + "/" + key, Repo: repo, Status: "external", Started: info.ModTime(), Updated: info.ModTime(), External: true, LogPath: path, Dir: filepath.Dir(path)}
			if old, ok := groups[key]; !ok || r.Updated.After(old.Updated) {
				groups[key] = r
			}
		}
		for _, r := range groups {
			runs = append(runs, r)
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].active() != runs[j].active() {
			return runs[i].active()
		}
		return runs[i].Started.After(runs[j].Started)
	})
	return runs
}

func tailFile(path string, limit int64) string {
	f, err := os.Open(path)
	if err != nil {
		return "Waiting for output…"
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err.Error()
	}
	start := max(int64(0), info.Size()-limit)
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return err.Error()
	}
	b, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return err.Error()
	}
	if start > 0 {
		if i := strings.IndexByte(string(b), '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	return string(b)
}

func worker(dir string) (result error) {
	var r Run
	if err := readJSON(filepath.Join(dir, "run.json"), &r); err != nil {
		return err
	}
	r.Dir = dir
	// A lock prevents accidentally starting a second worker for the same record.
	lock, err := os.OpenFile(filepath.Join(dir, "worker.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("worker already owns this run")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if r.Status != "queued" {
		return fmt.Errorf("run is already %s; create a new run to retry", r.Status)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	var stopRequested, abortRequested atomic.Bool
	stopRequested.Store(r.StopRequested)
	abortRequested.Store(r.AbortRequested)
	var stateMu sync.Mutex
	r.PID = os.Getpid()
	save := func(status string) error {
		stateMu.Lock()
		defer stateMu.Unlock()
		if status != "" {
			r.Status = status
		}
		r.StopRequested, r.AbortRequested = stopRequested.Load(), abortRequested.Load()
		r.Updated = time.Now()
		return writeJSON(filepath.Join(dir, "run.json"), r)
	}
	observe := func() {
		changed := false
		if _, err := os.Stat(filepath.Join(dir, "STOP")); err == nil {
			changed = stopRequested.CompareAndSwap(false, true)
		}
		if _, err := os.Stat(filepath.Join(dir, "ABORT")); err == nil {
			changed = abortRequested.CompareAndSwap(false, true) || changed
		}
		if abortRequested.Load() {
			cancel()
		}
		if changed {
			_ = save("")
		}
	}
	stopStatus := func() string {
		if abortRequested.Load() {
			return "aborted"
		}
		if !stopRequested.Load() && ctx.Err() != nil {
			return "interrupted"
		}
		return "stopped"
	}
	heartbeat := filepath.Join(dir, "heartbeat")
	if err = os.WriteFile(heartbeat, nil, 0600); err != nil {
		return err
	}
	done, monitorDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case t := <-ticker.C:
				observe()
				_ = os.Chtimes(heartbeat, t, t)
			}
		}
	}()
	defer func() { close(done); <-monitorDone }()
	defer func() {
		if result != nil {
			if ctx.Err() != nil {
				result = save(stopStatus())
				return
			}
			stateMu.Lock()
			r.Error = redactCredentials(result.Error())
			stateMu.Unlock()
			_ = save("failed")
			fmt.Println("ralph: failed:", r.Error)
		}
	}()
	if err = save("preparing"); err != nil {
		return err
	}
	stopped := func() bool {
		observe()
		return stopRequested.Load() || abortRequested.Load() || ctx.Err() != nil
	}
	if stopped() {
		return save(stopStatus())
	}
	fmt.Printf("ralph: preparing %s on %s\n", r.Repo.Name, r.Branch)
	setupCtx, setupCancel := context.WithTimeout(ctx, 3*time.Minute)
	defer setupCancel()
	if _, err = command(setupCtx, r.Repo.Path, "git", "fetch", "origin"); err != nil {
		return err
	}
	base, err := command(setupCtx, r.Repo.Path, "git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
	if err != nil {
		if _, err = command(setupCtx, r.Repo.Path, "git", "remote", "set-head", "origin", "-a"); err != nil {
			return err
		}
		base, err = command(setupCtx, r.Repo.Path, "git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
		if err != nil {
			return err
		}
	}
	if stopped() {
		return save(stopStatus())
	}
	if err = os.MkdirAll(filepath.Dir(r.Worktree), 0700); err != nil {
		return err
	}
	if _, err = command(setupCtx, r.Repo.Path, "git", "worktree", "add", "-b", r.Branch, r.Worktree, base); err != nil {
		return err
	}
	fmt.Printf("ralph: worktree %s\n", r.Worktree)
	for i := 1; i <= r.Max; i++ {
		if stopped() {
			return save(stopStatus())
		}
		stateMu.Lock()
		r.Iteration = i
		stateMu.Unlock()
		if err = save("running"); err != nil {
			return err
		}
		fmt.Printf("\nralph: ── iteration %d/%d ── %s\n", i, r.Max, time.Now().Format(time.RFC3339))
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return err
		}
		token := hex.EncodeToString(nonce[:])
		completionPath := filepath.Join(r.Worktree, ".ralph-complete.json")
		if err = os.Remove(completionPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		signal := completionSignal{Token: token, Iteration: i}
		payload, _ := json.Marshal(signal)
		prompt := r.Prompt + fmt.Sprintf("\n\nRalph loop iteration %d of %d. Work only in this dedicated worktree. Read .ralph-ledger.md if present and update it with progress, validation, and remaining work before finishing. Do not overwrite unrelated changes. Do not publish, merge, or send messages unless the task explicitly authorizes it. If the entire task is complete and verified, write this exact JSON to .ralph-complete.json: %s. Do not commit this completion file; Ralph removes it after reading. Otherwise leave concrete next steps in the ledger and do not create the completion file.", i, r.Max, payload)
		iterationLog := filepath.Join(dir, fmt.Sprintf("iteration-%02d.log", i))
		f, err := os.OpenFile(iterationLog, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		iterationCtx, iterationCancel := context.WithTimeout(ctx, time.Duration(r.Timeout)*time.Minute)
		output := &redactingWriter{dst: io.MultiWriter(os.Stdout, f)}
		args := []string{r.Runner, "run", "--standalone", "--auto", "--model", r.Model, "--", prompt}
		env := append(os.Environ(), "RALPH_COMPLETION_TOKEN="+token, fmt.Sprintf("RALPH_ITERATION=%d", i))
		err = runGuarded(iterationCtx, dir, r.Worktree, args, env, output, &abortRequested)
		iterationCancel()
		flushErr := output.Flush()
		closeErr := f.Close()
		if ctx.Err() != nil {
			return save(stopStatus())
		}
		if err != nil {
			return fmt.Errorf("iteration %d: %w (see iteration log)", i, err)
		}
		if closeErr != nil {
			return closeErr
		}
		if flushErr != nil {
			return flushErr
		}
		complete := readCompletion(completionPath, signal)
		if err = os.Remove(completionPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		if complete {
			fmt.Println("\nralph: task complete. Nice work, Ralph.")
			return save("complete")
		}
		if stopped() {
			return save(stopStatus())
		}
		if i < r.Max {
			if err = save("cooldown"); err != nil {
				return err
			}
			for s := 0; s < r.Cooldown; s++ {
				if stopped() {
					return save(stopStatus())
				}
				select {
				case <-ctx.Done():
					return save(stopStatus())
				case <-time.After(time.Second):
				}
			}
		}
	}
	fmt.Println("\nralph: iteration budget reached; review the ledger before continuing.")
	return save("budget reached")
}

type completionSignal struct {
	Token     string `json:"token"`
	Iteration int    `json:"iteration"`
}

func readCompletion(path string, want completionSignal) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024 {
		return false
	}
	var got completionSignal
	return readJSON(path, &got) == nil && got == want
}
