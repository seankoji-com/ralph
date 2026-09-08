# UI patterns from Crush

The [8 September catalogue review](design.md) updates this initial direction: quieter chrome, contextual actions, clear reading surfaces and a dedicated removal dialog. The artwork is now reserved for welcome states.

Inspected upstream commit `35a7bcab084a6022717d31b110c538a68d6fadf7` on 7 September 2026. These are original implementations of interaction and layout patterns in Ralph; no Crush code or assets were vendored. The inspected source uses [FSL-1.1-MIT](https://github.com/charmbracelet/crush/blob/35a7bcab084a6022717d31b110c538a68d6fadf7/LICENSE.md).

| Pattern and source | Applied in Ralph |
| --- | --- |
| [Searchable command dialog](https://github.com/charmbracelet/crush/blob/35a7bcab084a6022717d31b110c538a68d6fadf7/internal/ui/dialog/commands.go) | Ctrl+K opens a filtered, keyboard- and mouse-operated overlay; actions use Ralph's existing handlers. |
| [Workspace and model context](https://github.com/charmbracelet/crush/blob/35a7bcab084a6022717d31b110c538a68d6fadf7/internal/ui/model/sidebar.go) | A workshop sidebar shows the repository, assistant, coding model and loop budget on terminals at least 120 columns and 32 rows. |
| [Message boundaries and markdown](https://github.com/charmbracelet/crush/blob/35a7bcab084a6022717d31b110c538a68d6fadf7/internal/ui/styles/quickstyle.go) | Coloured message rails, a bounded reading width, and markdown colours consistent with the composer. |
| [Scroll position as visible state](https://github.com/charmbracelet/crush/blob/35a7bcab084a6022717d31b110c538a68d6fadf7/internal/ui/common/scrollbar.go) | One reserved scrollbar column in chat and output; click to jump, wheel to scroll. Replies preserve your reading position. |
| [Stable gradient identity](https://github.com/charmbracelet/crush/blob/35a7bcab084a6022717d31b110c538a68d6fadf7/internal/ui/logo/logo.go) | An original pink-to-violet Ralph wordmark. The existing bright action colours and 10%-opacity artwork remain. |
| [Contextual help](https://github.com/charmbracelet/crush/blob/35a7bcab084a6022717d31b110c538a68d6fadf7/internal/ui/model/status.go) | Keep the existing Bubbles help; command search makes actions accessible without a crowded footer. |

## Useful next steps

- Use the live provider catalog in a searchable model picker, following [Crush's model dialog](https://github.com/charmbracelet/crush/blob/35a7bcab084a6022717d31b110c538a68d6fadf7/internal/ui/dialog/models.go). Keep assistant, runner and OCR choices distinct.
- Add collapsible iteration results and diffs when the worker emits structured events, following [Crush's chat rendering](https://github.com/charmbracelet/crush/blob/35a7bcab084a6022717d31b110c538a68d6fadf7/internal/ui/model/chat.go). Raw log text should stay available.
- Separate temporary success notices from persistent failures, following [Crush's status model](https://github.com/charmbracelet/crush/blob/35a7bcab084a6022717d31b110c538a68d6fadf7/internal/ui/model/status.go). Errors must remain inspectable after a notice expires.

Validation: command filtering and focus isolation, rendered mouse coordinates at three terminal sizes, scrollbar navigation, conversation reading-position preservation, and the detached-worker smoke flow. Optional demo renders use `RALPH_SNAPSHOT_DIR=/tmp/ralph-visuals go test -run 'Test(Visual|Pattern)Snapshots'`.
