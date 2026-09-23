# Agent guide

Ralph is a Go terminal UI for detached coding loops. Read the relevant section
of [README.md](README.md) for runtime configuration and lifecycle details.

## Start here

| Change | Files |
| --- | --- |
| Repository discovery and GitHub pagination | `repos.go`, `repos_*_test.go` |
| Worker lifecycle, stop/abort and provider retry | `runs.go`, `guardian.go`, `worker_safety_test.go`, `failover_test.go` |
| Safe run/worktree cleanup | `prune.go`, `prune_batch_test.go` |
| Prompt partner and live model discovery | `assistant.go`, `provider_models.go` |
| TUI events and commands | `ui.go`, `ui_commands.go`, `ui_interaction_test.go` |

## Verify

```sh
go test -race ./...
go vet ./...
go build -o bin/ralph .
python3 scripts/smoke.py
```

The smoke test uses disposable Git remotes and a fake agent. Use `--demo` for
manual UI checks. Live provider tests are opt-in and send a request externally.

## Preserve

- Dirty, ignored, unmerged, active and uncertain worktrees must survive cleanup.
- Stop and abort preserve work; quitting the UI leaves detached workers running.
- Completion requires the current iteration's fresh token, never matching log text.
- Credentials stay in memory. Never copy them into run records or committed fixtures.
- Test state belongs in temporary directories, never the user's normal Ralph state.
