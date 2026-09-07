# Ralph

A terminal home for your coding loops. Pick a repo, work out a prompt with DeepSeek, then let Ralph take a few passes at it.

Built with [Charm](https://charm.land/libs/): Bubble Tea v2, Bubbles, Lip Gloss, Glamour and Huh. Violet panels, mint progress bars, Markdown conversations, keyboard controls and a small robot who wants you to take a coffee break.

## Run it

Requires Go 1.25.8 or newer, Git, an authenticated GitHub CLI (`gh auth login`), and your existing `opencode2` installation. macOS and Linux are supported; detached workers use Unix process groups.

```sh
go build -o bin/ralph .
./bin/ralph
```

`./bin/ralph --demo` is a playground with sample repos, runs and simulated chat. It does not clone, write state, contact an AI service or launch agents. `--doctor` checks local tools and configuration; `--check-ai` makes one small request to LiteLLM.

If colours are missing, run `./bin/ralph --color always`. Automatic mode respects `NO_COLOR` and terminal detection; `--color never` explicitly disables colours. The main run panel uses the supplied ASCII Ralph as a colourised watermark, blended at 10% opacity against its background. Foreground text stays intact.

1. Press `n`, choose a repo, then `enter`. Use `/` to filter. Cloud repos can be cloned with `c`.
2. Describe an idea. `enter` sends to DeepSeek; `shift+enter` adds a newline; `ctrl+d` turns the conversation into a prompt. For a prompt you've already written, use `ctrl+p`.
3. Edit the prompt. `ctrl+o` opens the Huh settings form. `ctrl+l` launches the loop.
4. Follow output on the run board. Close the TUI whenever you like; workers keep running. Reopen it to reconnect.

## Controls

| Where | Key | Action |
| --- | --- | --- |
| Everywhere except settings | `ctrl+k` | Search available commands |
| Main screens | `1` / `2` / `3` | Loops / repos / last workshop |
| Main screens | `j` / `k`, arrows | Select a run or repo |
| Repos | `/`, then `enter` | Filter, then leave the search field |
| Repos | `c` / `r` | Clone / refresh GitHub |
| Workshop | `enter` / `shift+enter` | Send / insert a newline |
| Workshop | `ctrl+s` / `ctrl+d` | Retry or send / draft the prompt |
| Workshop | `ctrl+p` | Use your text or return to the saved prompt |
| Review | `ctrl+o` | Model, iterations, rest and timeout settings |
| Review | Click `−` / `+` | Adjust iteration count |
| Review | `ctrl+l` | Launch the reviewed prompt |
| Loops | `tab` | Output / run details |
| Output, details or conversation | `ctrl+b` / `ctrl+f`, scroll wheel | Scroll back / forward |
| Output | `f` | Follow the latest output |
| Loops | `s` | Stop after the current iteration |
| Loops | `x` | Stop the current agent immediately; preserve the worktree |
| Loops | `d` | Remove a finished run only if its worktree is clean and branch merged |
| Main screens | `?` / `q` | Help / quit |
| Everywhere | `ctrl+c` | Save the open draft and quit; loops continue |

Click the tabs and coloured action buttons to navigate. Click a run to view its output; click a repository to select it, then click again or use Open for its workshop. Drag inside the prompt to select text. `⌃` on a button means Control.

`esc` goes back or cancels an AI request. Drafts and conversations are saved when you send, change screens or quit normally. Aim for a 110 × 34 terminal; smaller windows use a stacked run view.

## Your configuration

Defaults match the existing setup: organisation `seankoji-com`, checkouts in `~/repos`, runner `opencode2`, and `litellm/deepseek-v4-flash` for coding. The prompt partner uses `deepseek-v4-flash` through LiteLLM's OpenAI-compatible API.

Ralph reads the `litellm` provider from the isolated OpenCode2 `opencode.json`, then the main OpenCode `opencode.json` if no provider was found. It supports literal keys, `{env:NAME}` and `{file:path}`. Credentials stay in memory and are never copied into run records. JSONC files are not parsed; use environment overrides for those configurations.

| Variable | Purpose |
| --- | --- |
| `RALPH_ORG` | GitHub organisation |
| `RALPH_REPOS_DIR` | Local checkout directory |
| `RALPH_STATE_DIR` | Private state directory; defaults to `$XDG_STATE_HOME/ralph` or `~/.local/state/ralph` |
| `RALPH_LITELLM_URL` | API base URL, including `/v1` |
| `RALPH_LITELLM_API_KEY` | API key override; `LITELLM_API_KEY` also works |
| `RALPH_ASSIST_MODEL` | Prompt partner model |
| `RALPH_MODEL` | Default coding model, in `provider/model` form |
| `RALPH_RUNNER` | Executable supporting the OpenCode2 command below |

```sh
export RALPH_LITELLM_URL='http://nas.careynas.net:4000/v1'
./bin/ralph --check-ai
```

Only the conversation and selected repo name go to the prompt partner. It has no tools or automatic source access. Org discovery uses paginated GitHub REST requests; local checkouts remain available when GitHub is offline.

Before each workshop reply or draft, Ralph reads the current provider's authenticated `/models` endpoint. For tasks that change code, the prompt partner suggests Open Code Review (OCR) and a reviewer from that live list, preferably a different model family from the selected coding model. It includes the proposed review step in the draft unless you decline. Reviewer selection uses per-run OCR flags and does not change shared OCR defaults. A failed model lookup is disclosed instead of presenting a guessed or stale model as available. `./bin/ralph --models` prints the same live inventory.

## How a loop runs

Launch fetches `origin` and creates a dedicated worktree from its default branch on `codex/ralph-<run-id>`. Every iteration starts a new process:

```text
opencode2 run --standalone --auto --model <model> -- <prompt>
```

The agent has its normal configured tool permissions. `--auto` runs unattended. A worktree isolates file edits; it is not a security sandbox or a limit on external tool actions.

Ralph asks the agent to carry progress in `.ralph-ledger.md`. It does not reset or rebase between iterations, so unfinished changes stay available. Dependencies are the agent's responsibility according to each repo's instructions. The original checkout is left alone.

Defaults are five iterations, a 15-second rest and a 30-minute timeout per iteration. A non-zero exit or timeout stops the run. The agent marks completion by writing `.ralph-complete.json` with the current iteration and a fresh token supplied in its prompt. Ralph validates and removes the file; log text cannot mark completion. Reaching the iteration budget is recorded separately. Completion is the agent's report, not independent verification. `s` creates a stop request, allowing the current iteration to finish. `x` requests an immediate abort and kills the current agent process group. Once the worker observes either request, it latches and records it. Both preserve the worktree. Timeout and worker shutdown send SIGTERM first, then SIGKILL after two seconds if needed.

Each private `runs/<id>/` directory contains `run.json`, `prompt.md`, `output.log`, per-iteration logs and a worker heartbeat. New worktrees live separately in `worktrees/<id>/`, outside the control directories. Agents retain the same OS user permissions and can access other paths; this separation prevents accidental parent-directory cleanup, not deliberate tampering. Log viewing reads only the latest 128 KiB and strips terminal control sequences. URL credentials are redacted before native output is saved; logs can still contain source code and other sensitive content printed by the agent. A lost heartbeat is recorded as interrupted only after the worker lock confirms no worker owns the run; Ralph never guesses that an interrupted run succeeded. The board refreshes every five seconds. `./bin/ralph --prune` removes finished runs older than 30 days only when their worktree is clean (including ignored files) and their branch is merged into the locally known origin default branch. Fetch that repository first to update the merge check. Interrupted runs use the same cleanup guards. The `d` action removes a selected finished run under those guards without an age cutoff. Dirty, unmerged, active and uncertain runs are preserved; Git worktree tooling remains available for manual cleanup.

Existing `~/repos/*/.ralph/logs/*.log` files are grouped by run and appear as external script logs. These are read-only: the old scripts have no heartbeat, so the app shows last-write activity rather than claiming they are running or complete. Keep using the script's `scripts/ralph/STOP` file to stop those runs. Zooma's original script, npm setup and issue-count guard remain unchanged; the app's native runner is a general coding loop.

## Development

```sh
go test -race ./...
go vet ./...
go build -o bin/ralph .
python3 scripts/smoke.py
```

The smoke test drives a real terminal through the repo picker, prompt review, Huh settings and launch. It closes the TUI, verifies detached completion, and checks that the original dirty checkout survived. It uses disposable local Git remotes and a fake agent; no real model or GitHub mutations are involved.

`RALPH_LIVE_TEST=1 go test -run TestLiveReviewerSuggestion -v` optionally verifies model discovery and an OCR recommendation against your configured provider. It sends a short sample coding request; it does not execute OCR or a coding agent.

The [Crush pattern notes](docs/ui-patterns.md) link the inspected source to Ralph's command dialog, workshop sidebar, message styling and scroll behaviour.
