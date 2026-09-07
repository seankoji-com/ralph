#!/usr/bin/env python3
"""Exercise the real TUI and detached worker using disposable git repos and a fake agent."""
import fcntl
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
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
    env.update(RALPH_ORG="ralph-fixture", RALPH_REPOS_DIR=str(root / "repos"),
               RALPH_STATE_DIR=str(root / "state"), GIT_CONFIG_GLOBAL=str(root / "gitconfig"),
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
    agent.write_text("#!/bin/sh\nset -eu\nsleep 3\nprintf 'Verified fixture work.\\n' > .ralph-ledger.md\nprintf '<ralph>COMPLETE</ralph>\\n'\n")
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
        state = json.loads(state_path.read_text())
        assert os.getsid(state["pid"]) == state["pid"], "worker did not detach"
        send("\x03")
        process.wait(timeout=5)
        assert process.returncode == 0
        wait_for(lambda: json.loads(state_path.read_text())["status"] == "complete", "worker completed after TUI quit")
        state = json.loads(state_path.read_text())
        assert state["iteration"] == 1
        assert (Path(state["worktree"]) / ".ralph-ledger.md").read_text() == "Verified fixture work.\n"
        assert (repo / "README").read_text() == "preserve my dirty checkout\n"
        assert "COMPLETE" in (state_path.parent / "output.log").read_text()
        print("PASS: mouse tabs + Open → command palette → Huh settings → launch → quit TUI → detached completion")
        print("PASS: worktree, ledger, output log and original dirty checkout verified")
    finally:
        if process.poll() is None:
            process.terminate()
            process.wait(timeout=5)
        if state_path and state_path.exists():
            state = json.loads(state_path.read_text())
            if state["status"] in ("queued", "preparing", "running", "cooldown") and state.get("pid"):
                try:
                    os.kill(state["pid"], signal.SIGTERM)
                except ProcessLookupError:
                    pass
                time.sleep(1)
        os.close(master)
