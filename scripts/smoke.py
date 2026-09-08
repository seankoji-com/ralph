#!/usr/bin/env python3
"""Exercise the real TUI and detached worker using disposable git repos and a fake agent."""
import fcntl
import json
import os
from pathlib import Path
import pty
import re
import select
import struct
import subprocess
import sys
import tempfile
import termios
import time

binary = Path(sys.argv[1] if len(sys.argv) > 1 else "bin/ralph").resolve()

with tempfile.TemporaryDirectory(prefix="ralph-smoke-") as scratch:
    root = Path(scratch)
    env = os.environ.copy()
    for key in list(env):
        if key.startswith("RALPH_") or key in ("LITELLM_API_KEY", "DEVPASS_API_KEY"):
            env.pop(key)
    env.update(RALPH_ORG="ralph-fixture", RALPH_REPOS_DIR=str(root / "repos"),
               RALPH_STATE_DIR=str(root / "state"), GIT_CONFIG_GLOBAL=str(root / "gitconfig"),
               RALPH_MODEL="litellm/smoke", RALPH_FALLBACK_MODEL="devpass/smoke",
               RALPH_FALLBACK_PROVIDER="devpass",
               OPENCODE2_ROOT=str(root / "opencode2"), XDG_CONFIG_HOME=str(root / "config"),
               GIT_CONFIG_NOSYSTEM="1", TERM="xterm-256color", COLORTERM="truecolor")

    def run(*args, cwd=root):
        return subprocess.check_output(args, cwd=cwd, env=env, stderr=subprocess.STDOUT).decode()

    remote = root / "remote" / "example.git"
    remote.parent.mkdir()
    (root / "repos").mkdir()
    run("git", "init", "--bare", "--initial-branch=main", str(remote))
    run("git", "config", "--global", f"url.file://{root}/remote/.insteadOf", "https://github.com/ralph-fixture/")
    repo = root / "repos" / "example"
    run("git", "clone", "https://github.com/ralph-fixture/example.git", str(repo))
    run("git", "config", "user.name", "Ralph smoke", cwd=repo)
    run("git", "config", "user.email", "smoke@example.invalid", cwd=repo)
    (repo / "README").write_text("fixture\n")
    run("git", "add", "README", cwd=repo)
    run("git", "commit", "-m", "fixture", cwd=repo)
    run("git", "push", "-u", "origin", "main", cwd=repo)
    run("git", "remote", "set-head", "origin", "-a", cwd=repo)
    (repo / "README").write_text("preserve my dirty checkout\n")

    fakebin = root / "fakebin"
    fakebin.mkdir()
    gh = fakebin / "gh"
    gh.write_text('#!/bin/sh\nprintf \'[[{"full_name":"ralph-fixture/example","description":"Detached smoke fixture"}]]\\n\'\n')
    gh.chmod(0o700)
    agent = fakebin / "opencode2"
    agent.write_text("#!/bin/sh\nset -eu\nif [ \"$1\" = models ]; then echo devpass/smoke; exit 0; fi\nif [ \"$5\" = 'litellm/smoke' ]; then\n  echo 'Error: HTTP 503' >&2\n  exit 1\nfi\nsleep 8\nprintf 'Verified fixture work.\\n' > .ralph-ledger.md\nprintf '{\"token\":\"%s\",\"iteration\":%s}' \"$RALPH_COMPLETION_TOKEN\" \"$RALPH_ITERATION\" > .ralph-complete.json\necho COMPLETE\n")
    agent.chmod(0o700)
    env["PATH"] = str(fakebin) + os.pathsep + env["PATH"]
    env["RALPH_RUNNER"] = str(agent)

    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 38, 120, 0, 0))
    process = subprocess.Popen([str(binary), "--color", "always"], stdin=slave, stdout=slave, stderr=slave,
                               env=env, cwd=root, start_new_session=True)
    os.close(slave)
    output = bytearray()
    state_path = None

    def wait_for(predicate, label, timeout=15):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if select.select([master], [], [], 0.05)[0]:
                try:
                    output.extend(os.read(master, 65536))
                except OSError:
                    pass
            if predicate():
                return
        raise AssertionError(f"Timed out: {label}\n{output[-3000:].decode(errors='replace')}")

    def screen(text):
        wait_for(lambda: text.encode() in output, text)
        output.clear()

    def send(text):
        os.write(master, text.encode())

    def worker_finished():
        state = json.loads(state_path.read_text())
        if state["status"] in ("queued", "preparing", "running", "cooldown", "stopping", "aborting", "orphaned"):
            return False
        for name in ("worker.lock", "agent.lock"):
            path = state_path.parent / name
            if not path.exists():
                continue
            with path.open("rb") as lock:
                try:
                    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                except BlockingIOError:
                    return False
        return True

    try:
        wait_for(lambda: re.search(rb"\x1b\[[0-9;]*38;2;", output), "Lip Gloss true-colour output")
        screen("LOOP STATION")
        send("\x1b[<0;17;3M\x1b[<0;17;3m")  # click Repositories tab
        screen("Detached smoke fixture")
        send("\x1b[<0;5;35M\x1b[<0;5;35m")  # click Open
        screen("PROMPT WORKSHOP")
        send("Write a fixture ledger and verify the task.\x0b")  # ctrl+k
        screen("available")
        send("review prompt\r")
        screen("READY TO RALPH?")
        send("\x0f")  # ctrl+o, Huh settings
        screen("TUNE YOUR LOOP")
        send("\r\r\r\r")  # preserve defaults and submit both groups
        screen("Loop settings updated.")
        send("\x0c")  # ctrl+l
        wait_for(lambda: bool(list((root / "state" / "runs").glob("*/run.json"))), "run record")
        state_path = next((root / "state" / "runs").glob("*/run.json"))
        wait_for(lambda: json.loads(state_path.read_text())["status"] == "running", "worker running")
        wait_for(lambda: json.loads(state_path.read_text()).get("active_model") == "devpass/smoke", "fallback running")
        screen("Fallback")
        state = json.loads(state_path.read_text())
        assert os.getsid(state["pid"]) == state["pid"], "worker did not detach"
        send("\x03")
        process.wait(timeout=5)
        assert process.returncode == 0
        wait_for(lambda: json.loads(state_path.read_text())["status"] == "complete", "worker completed after TUI quit")
        wait_for(worker_finished, "worker and guardian released the fixture")
        state = json.loads(state_path.read_text())
        assert state["iteration"] == 1
        assert state["active_model"] == "devpass/smoke"
        assert (Path(state["worktree"]) / ".ralph-ledger.md").read_text() == "Verified fixture work.\n"
        assert (repo / "README").read_text() == "preserve my dirty checkout\n"
        assert "COMPLETE" in (state_path.parent / "output.log").read_text()
        print("PASS: mouse tabs + Open → command palette → Huh settings → launch → fallback → quit TUI → detached completion")
        print("PASS: worktree, ledger, output log and original dirty checkout verified")

        # Reopen the built app and exercise deletion against this fixture only.
        os.close(master)
        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 38, 120, 0, 0))
        process = subprocess.Popen([str(binary), "--color", "always"], stdin=slave, stdout=slave, stderr=slave,
                                   env=env, cwd=root, start_new_session=True)
        os.close(slave)
        output.clear()
        screen("SAVED OUTPUT")
        send("d")
        screen("Remove this loop?")
        send("\x1b")
        screen("Removal cancelled.")
        assert state_path.exists()
        send("d")
        screen("Remove this loop?")
        send("y")
        screen("worktree contains local files or changes")
        assert state_path.exists() and Path(state["worktree"]).exists()

        # Remove only the generated fixture ledger so the unchanged, merged
        # fixture branch becomes eligible for Ralph's guarded deletion.
        (Path(state["worktree"]) / ".ralph-ledger.md").unlink()
        send("d")
        screen("Remove this loop?")
        send("Y")
        wait_for(lambda: not state_path.parent.exists(), "run and logs removed")
        screen("Nothing looping. Yet.")
        assert not Path(state["worktree"]).exists()
        assert state["worktree"] not in run("git", "worktree", "list", "--porcelain", cwd=repo)
        assert not run("git", "for-each-ref", "--format=%(refname)", "refs/heads/" + state["branch"], cwd=repo).strip()
        assert (repo / "README").read_text() == "preserve my dirty checkout\n"
        send("\x03")
        process.wait(timeout=5)
        assert process.returncode == 0
        print("PASS: reopen → cancel deletion → refuse dirty loop → remove clean loop → empty board, disk and Git refs verified")
    finally:
        if process.poll() is None:
            process.terminate()
            process.wait(timeout=5)
        if state_path and state_path.exists():
            if not worker_finished():
                (state_path.parent / "ABORT").write_text("smoke fixture cleanup\n")
                wait_for(worker_finished, "fixture stopped before cleanup")
        os.close(master)
