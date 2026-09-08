# Ralph catalogue review

Reviewed 8 September 2026 against [Charm](https://charm.land), [Crush](https://github.com/charmbracelet/crush) and [Mods](https://github.com/charmbracelet/mods). This is an independent critique, not a Charm endorsement. The bar is a terminal app that explains its state, protects the user's attention and makes recovery straightforward.

## Direction

Ralph is a workshop for briefing and supervising autonomous coding loops. Its distinctive moment is the character greeting you before work starts. Once work begins, the brief, result and next action take precedence.

Use terminal-native monospace with three levels: bold task/repository headings, regular content, muted context. Keep reading widths bounded. Left-align task content; centre only temporary dialogs. Use `#17141F` for the canvas, `#292336` for the composer and inactive tabs, `#FFF5FC` for text, `#BCB4D0` for context, pink `#FF4FA3` for the primary action and mint `#37E6B5` for active/positive state. Red and amber indicate risk or attention. Borders use `#554963` and do not compete with selected controls.

This revises the previous design's full-width purple banner, rainbow action strip and artwork behind text. The character and pink identity stay; the surrounding surfaces become quieter.

## Adversarial findings and changes

| Finding | Change and evidence |
| --- | --- |
| Every button appeared primary; unavailable controls remained visible. | One filled primary action. Secondary actions use text. Empty, external, finished, orphaned, busy and repository states have appropriate controls. |
| Two loops for one repository looked alike. | The list shows the saved brief and iteration count. The selected loop shows its brief and outcome above output. |
| Completion looked like ongoing execution. | Finished loops display Saved output and no Follow action. Outcomes distinguish task completion from an exhausted iteration budget. |
| Deletion depended on a clipped footer, accepted only lowercase confirmation and swallowed quit. | A Lip Gloss layered dialog shows the target and consequences. Mouse targets are tested against rendered labels at four sizes. Refresh retains the captured target; either case of Y confirms; Ctrl+C quits. |
| The demo claimed removal without changing the board. | Confirmation removes the demo row and clears or replaces its detail/output panes. |
| A provider failure could be understood only through a truncated notice. | The conversation retains a wrapped diagnostic and recovery instructions. Retry is the primary action and preserves the brief without duplicating it. |
| Configuring fallback could break a healthy primary response. | Keep the HTTP request context alive until the response body closes. A regression test holds the body until after headers arrive; it reproduced `context canceled` before the fix. |
| Artwork competed with code and conversation. | Keep it on the welcome surface; populated logs and conversations have clear backgrounds. |

## Ecosystem references

Inspected Crush `5d55bb5048460eb74af799bb5091a13bd16a1201`: `internal/ui/model/landing.go` separates workspace/model context, and `status.go` uses Bubbles help with semantic notifications. Inspected Mods `0425d0d7861e4bbc396200b3bd2eee1825715300`: `styles.go` gives errors and confirmations distinct treatments; `mods.go` uses Glamour for bounded Markdown output. No reference code or assets were copied.

Ralph uses Bubble Tea for asynchronous updates, Lip Gloss canvases for modal layering, Bubbles for input, viewport, scroll state, progress, spinner, contextual help and command search, Huh for loop settings, and Glamour for conversation Markdown. Adding more libraries is not a quality metric. Animation belongs to active work; completed work stays still.

## Verification and remaining limits

`ui_catalogue_test.go` exercises rendered modal coordinates, resize events, background input isolation, confirmation after a refresh, contextual actions and provider-error recovery. `ui_failover_test.go` covers provider disclosure, reply/draft attribution and quit safeguards. `failover_test.go` covers slow healthy completions, cancellation, combined errors, bounded diagnostics and selected-model retries. `scripts/smoke.py` drives the built app through a PTY: mouse navigation, command search, Huh settings, launch, fallback, quit, detached completion, reopen, cancelled/refused deletion and verified removal from the board, disk and Git.

Render evidence with `RALPH_SNAPSHOT_DIR=<existing-directory> go test -run 'Test(Visual|Pattern|Catalogue)Snapshots'`. The output is actual application ANSI. PNG previews render those cells in Menlo; they are not screenshots of a particular terminal emulator.

This pass does not establish superiority over Crush or Mods. Live provider/runner failover, a searchable runner model picker and a structured per-iteration results view still need work before an unqualified catalogue recommendation. The current runner fallback depends on recognised error text and configured OpenCode providers. Deterministic tests do not prove a live subscription or endpoint works.
