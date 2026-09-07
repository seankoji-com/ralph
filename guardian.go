package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
)

type agentRecord struct {
	PGID int  `json:"pgid"`
	Live bool `json:"live"`
}

func agentLease(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "agent.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("agent still owns this run")
	}
	return f, nil
}

// A persisted PID is used only to refuse cleanup, never to signal a process:
// after a crash it may have been reused by an unrelated process group.
func agentMayBeAlive(dir string) bool {
	var r agentRecord
	err := readJSON(filepath.Join(dir, "agent.json"), &r)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		return true
	}
	return r.Live && r.PGID > 1 && syscall.Kill(-r.PGID, 0) != syscall.ESRCH
}

func runGuarded(ctx context.Context, dir, worktree string, args, env []string, output io.Writer, abort *atomic.Bool) error {
	lease, err := agentLease(dir)
	if err != nil {
		return err
	}
	defer lease.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	defer writer.Close()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, exe, append([]string{"guardian", dir}, args...)...)
	cmd.Dir, cmd.Env = worktree, env
	cmd.Stdout, cmd.Stderr = output, output
	// The lease is acquired before spawning, closing the crash/startup window.
	cmd.ExtraFiles = []*os.File{reader, lease}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		message := byte('T')
		if abort.Load() {
			message = 'A'
		}
		_, err := writer.Write([]byte{message})
		_ = writer.Close()
		return err
	}
	cmd.WaitDelay = 10 * time.Second
	if err = cmd.Start(); err != nil {
		return err
	}
	_ = reader.Close()
	_ = lease.Close() // guardian (and then agent) retain the same locked file description
	return cmd.Wait()
}

// The worker owns only the write end of an anonymous control pipe. Its death,
// including SIGKILL, closes that pipe and cancels the agent independently.
func guardian(dir string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("guardian requires a runner")
	}
	control, lease := os.NewFile(3, "worker-control"), os.NewFile(4, "agent-lease")
	defer control.Close()
	defer lease.Close()
	if _, err := lease.Stat(); err != nil {
		return fmt.Errorf("missing agent lease: %w", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	var abort atomic.Bool
	go func() {
		var b [1]byte
		_, _ = control.Read(b[:])
		abort.Store(b[0] == 'A')
		cancel()
	}()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.ExtraFiles = []*os.File{lease}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var killTimer *time.Timer
	cmd.Cancel = func() error {
		if abort.Load() {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		killTimer = time.AfterFunc(2*time.Second, func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
		return err
	}
	cmd.WaitDelay = 3 * time.Second
	if err := cmd.Start(); err != nil {
		return err
	}
	record := agentRecord{PGID: cmd.Process.Pid, Live: true}
	if err := writeJSON(filepath.Join(dir, "agent.json"), record); err != nil {
		cancel()
		_ = cmd.Wait()
		if killTimer != nil {
			killTimer.Stop()
		}
		return err
	}
	err := cmd.Wait()
	if killTimer != nil {
		killTimer.Stop()
	}
	record.Live = syscall.Kill(-record.PGID, 0) != syscall.ESRCH
	if saveErr := writeJSON(filepath.Join(dir, "agent.json"), record); saveErr != nil && err == nil {
		err = saveErr
	}
	return err
}
