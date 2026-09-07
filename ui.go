package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	ink    = lipgloss.Color("#FFF5FC")
	muted  = lipgloss.Color("#BCB4D0")
	purple = lipgloss.Color("#9D75FF")
	green  = lipgloss.Color("#37E6B5")
	amber  = lipgloss.Color("#FFD166")
	red    = lipgloss.Color("#FF6295")
	dim    = lipgloss.NewStyle().Foreground(muted)
	accent = lipgloss.NewStyle().Foreground(purple).Bold(true)
	panel  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#8655E8")).Padding(0, 1)
)

type screen int

const (
	board screen = iota
	repositories
	workshop
	review
)

type tickMsg time.Time
type reposMsg struct {
	repos  []Repo
	err    error
	remote bool
}
type runsMsg struct {
	runs    []Run
	log, id string
}
type assistantMsg struct {
	text    string
	err     error
	draft   bool
	request int
}
type launchedMsg struct {
	run Run
	err error
}
type clonedMsg struct {
	repo Repo
	err  error
}
type noticeMsg struct {
	text string
	err  error
}
type draftState struct {
	Messages      []Message
	Input, Prompt string
	Options       RunOptions
}

type model struct {
	chatContent                                      string
	palette                                          *commandPalette
	config                                           Config
	page                                             screen
	width, height                                    int
	repos                                            []Repo
	runs                                             []Run
	repoIndex, runIndex                              int
	repo                                             Repo
	filter                                           textinput.Model
	input                                            textarea.Model
	logs, chat, info                                 viewport.Model
	spin                                             spinner.Model
	messages                                         []Message
	prompt                                           string
	options                                          RunOptions
	settings                                         *loopSettings
	busy, following, details, help, demo, refreshing bool
	notice                                           string
	request                                          int
	cancel                                           context.CancelFunc
	hover                                            string
	dragging                                         bool
}

func newModel(c Config, demo bool) model {
	f := textinput.New()
	f.Placeholder = "Find a repository…"
	f.SetWidth(40)
	in := textarea.New()
	in.Placeholder = "What should Ralph work on? A rough idea is plenty."
	in.ShowLineNumbers = false
	in.Prompt = "│ "
	in.CharLimit = 24000
	in.MaxHeight = 0
	in.SetHeight(5)
	in.SetVirtualCursor(true)
	style := in.Styles()
	style.Focused.Base = lipgloss.NewStyle().Background(composerBackground).Foreground(ink)
	style.Focused.Text = lipgloss.NewStyle().Foreground(ink)
	style.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("#D4C1F5"))
	style.Focused.Prompt = lipgloss.NewStyle().Foreground(green)
	style.Focused.CursorLine = lipgloss.NewStyle()
	style.Focused.Selection = lipgloss.NewStyle().Background(green).Foreground(mainBackground)
	style.Blurred = style.Focused
	in.SetStyles(style)
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = accent
	l := viewport.New(viewport.WithWidth(60), viewport.WithHeight(15))
	l.SoftWrap = true
	ch := viewport.New(viewport.WithWidth(80), viewport.WithHeight(12))
	ch.SoftWrap = true
	info := viewport.New(viewport.WithWidth(60), viewport.WithHeight(15))
	info.SoftWrap = true
	m := model{config: c, width: 110, height: 34, filter: f, input: in, logs: l, chat: ch, info: info, spin: s, options: RunOptions{Max: 5, Cooldown: 15, Timeout: 30, Model: c.Model}, following: true, demo: demo}
	if demo {
		m.repos, m.runs = demoData()
		m.logs.SetContent(demoLog)
		m.notice = "Demo playground. No agents, API calls, clones or file writes."
	}
	m.resize()
	return m
}

func (m model) Init() tea.Cmd {
	if m.demo {
		return tea.Batch(m.spin.Tick, clockTick())
	}
	return tea.Batch(m.spin.Tick, clockTick(), func() tea.Msg { return reposMsg{repos: localRepos(m.config)} })
}
func clockTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) refresh() tea.Cmd {
	c, repos := m.config, append([]Repo(nil), m.repos...)
	id, path := "", ""
	if r, ok := m.selectedRun(); ok {
		id, path = r.ID, r.logPath()
	}
	return func() tea.Msg {
		rs := loadRuns(c, repos)
		log := ""
		if path != "" {
			log = tailFile(path, 128<<10)
		}
		return runsMsg{rs, log, id}
	}
}

func (m model) selectedRun() (Run, bool) {
	if m.runIndex >= 0 && m.runIndex < len(m.runs) {
		return m.runs[m.runIndex], true
	}
	return Run{}, false
}
func (m model) filteredRepos() []Repo {
	q := strings.ToLower(m.filter.Value())
	out := []Repo{}
	for _, r := range m.repos {
		if strings.Contains(strings.ToLower(r.Name+" "+r.Description), q) {
			out = append(out, r)
		}
	}
	return out
}

func (m *model) resize() {
	w := max(30, m.width-10)
	m.filter.SetWidth(max(20, w-10))
	if m.page == workshop {
		w = m.workshopWidth() - 4
	}
	m.input.SetWidth(w)
	if m.page == review {
		m.input.SetHeight(max(5, m.height-17))
	} else {
		m.input.SetHeight(5)
	}
	m.chat.SetWidth(max(20, w-1))
	m.chat.SetHeight(max(3, m.height-20))
	logWidth := m.width - 41
	if m.width < 100 {
		logWidth = m.width - 10
	}
	m.logs.SetWidth(max(20, logWidth-1))
	m.logs.SetHeight(max(3, m.height-17))
	if m.width < 100 {
		m.logs.SetHeight(max(1, m.height-22))
	}
	m.info.SetWidth(m.logs.Width())
	m.info.SetHeight(m.logs.Height())
	if m.settings != nil {
		m.settings.WithWidth(max(30, m.width-12))
	}
}

func (m *model) updateChat() {
	var b strings.Builder
	renderer := workshopRenderer(min(78, m.chat.Width()-4))
	if len(m.messages) == 0 {
		b.WriteString(workshopWelcome(m.chat.Width(), m.chat.Height()))
	}
	for _, msg := range m.messages {
		name, colour := "You", mint
		content := safeText(msg.Content)
		if msg.Role == "assistant" {
			name, colour = "Ralph", pink
			if renderer != nil {
				if rendered, err := renderer.Render(content); err == nil {
					content = strings.TrimSpace(rendered)
				}
			}
		}
		rail := lipgloss.NewStyle().Border(lipgloss.Border{Left: "▌"}, false, false, false, true).BorderForeground(colour).PaddingLeft(1).Width(min(82, m.chat.Width()))
		heading := lipgloss.NewStyle().Foreground(colour).Bold(true).Render(name)
		if msg.Role == "assistant" {
			heading += "  " + dim.Render(m.config.AssistModel)
		}
		b.WriteString(rail.Render(heading+"\n\n"+content) + "\n\n")
	}
	m.chatContent = b.String()
	m.refreshChat()
}

// Animate the pending reply without re-rendering the conversation's markdown.
func (m *model) refreshChat() {
	follow, offset := m.chat.AtBottom(), m.chat.YOffset()
	content := m.chatContent
	if m.busy && m.page == workshop {
		rail := lipgloss.NewStyle().Border(lipgloss.Border{Left: "▌"}, false, false, false, true).BorderForeground(pink).PaddingLeft(1).Width(min(82, m.chat.Width()))
		heading := lipgloss.NewStyle().Foreground(pink).Bold(true).Render("Ralph") + "  " + dim.Render(m.config.AssistModel)
		content += rail.Render(heading + "\n\n" + m.spin.View() + " Thinking…")
	}
	m.chat.SetContent(content)
	if len(m.messages) == 0 {
		m.chat.GotoTop()
	} else if follow {
		m.chat.GotoBottom()
	} else {
		m.chat.SetYOffset(offset)
	}
}

func (m model) draftPath() string {
	return filepath.Join(m.config.StateDir, "drafts", strings.ReplaceAll(m.repo.Name, "/", "--")+".json")
}
func (m model) saveDraft() error {
	if m.demo || m.repo.Name == "" || (m.page != workshop && m.page != review) {
		return nil
	}
	d := draftState{Messages: m.messages, Prompt: m.prompt, Options: m.options}
	if m.page == review {
		d.Prompt = m.input.Value()
	} else {
		d.Input = m.input.Value()
	}
	return writeJSON(m.draftPath(), d)
}

func (m *model) openWorkshop(r Repo) tea.Cmd {
	m.repo = r
	m.page = workshop
	m.messages = nil
	m.prompt = ""
	m.input.Reset()
	m.notice = ""
	if !m.demo {
		var d draftState
		if readJSON(m.draftPath(), &d) == nil {
			m.messages = d.Messages
			m.prompt = d.Prompt
			if d.Options.validate() == nil {
				m.options = d.Options
			}
			m.input.SetValue(d.Input)
			m.notice = "Restored your saved draft."
		}
	}
	m.resize()
	m.updateChat()
	if len(m.messages) > 0 {
		m.chat.GotoBottom()
	}
	return m.input.Focus()
}

func (m *model) ask(draft bool) tea.Cmd {
	if m.busy {
		return nil
	}
	value := strings.TrimSpace(m.input.Value())
	if value != "" {
		m.messages = append(m.messages, Message{Role: "user", Content: value})
		m.input.Reset()
	}
	if len(m.messages) == 0 {
		m.notice = "Give Ralph an idea first."
		return nil
	}
	m.updateChat()
	m.chat.GotoBottom()
	if err := m.saveDraft(); err != nil {
		m.notice = "Cannot save draft: " + err.Error()
		return nil
	}
	m.busy = true
	m.refreshChat()
	m.chat.GotoBottom()
	m.request++
	request := m.request
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	m.cancel = cancel
	c, r, h := m.config, m.repo, append([]Message(nil), m.messages...)
	c.Model = m.options.Model
	if m.demo {
		return func() tea.Msg {
			defer cancel()
			if draft {
				return assistantMsg{text: "Improve the search experience. Inspect the existing search flow, identify one usability problem per iteration, implement a focused fix and validate it with relevant tests. Preserve unrelated work. Stop when keyboard navigation, empty states and error recovery work consistently.", draft: true, request: request}
			}
			return assistantMsg{text: "Let's give this a clear finish line. Which part matters most: finding the right result, keyboard navigation, or handling empty results?", request: request}
		}
	}
	return func() tea.Msg {
		defer cancel()
		s, e := askAssistant(ctx, c, r, h, draft)
		return assistantMsg{s, e, draft, request}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if k, ok := msg.(tea.KeyPressMsg); ok {
		if k.String() == "ctrl+k" && m.settings == nil {
			if m.palette != nil {
				m.palette = nil
				return m, nil
			}
			cmd = m.openPalette()
			return m, cmd
		}
		if m.palette != nil && k.String() != "ctrl+c" {
			return m.updatePalette(k)
		}
	}
	if m.palette != nil {
		if paste, ok := msg.(tea.PasteMsg); ok {
			var c tea.Cmd
			m.palette.input, c = m.palette.input.Update(paste)
			m.palette.index = 0
			return m, c
		}
	}
	if mouse, ok := msg.(tea.MouseMsg); ok {
		return m.updateMouse(mouse)
	}
	if m.settings != nil {
		if k, ok := msg.(tea.KeyPressMsg); ok {
			if k.String() == "esc" {
				m.settings = nil
				return m, m.input.Focus()
			}
			if k.String() == "ctrl+c" {
				if err := m.saveDraft(); err != nil {
					m.notice = err.Error()
					return m, nil
				}
				return m, tea.Quit
			}
		}
		routeToForm := true
		switch msg.(type) {
		case tickMsg, spinner.TickMsg, runsMsg, reposMsg, launchedMsg, clonedMsg, noticeMsg, tea.WindowSizeMsg:
			routeToForm = false
		}
		if routeToForm {
			updated, formCmd := m.settings.Update(msg)
			if f, ok := updated.(*huh.Form); ok {
				m.settings.Form = f
			}
			if m.settings.State == huh.StateCompleted {
				opts := optionsFromForm(m.settings)
				if err := opts.validate(); err != nil {
					m.notice = err.Error()
					m.settings = settingsForm(m.options, m.width-12)
					return m, m.settings.Init()
				}
				m.options = opts
				m.settings = nil
				m.notice = "Loop settings updated."
				if err := m.saveDraft(); err != nil {
					m.notice = err.Error()
				}
				return m, m.input.Focus()
			}
			return m, formCmd
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		if m.page == workshop {
			m.updateChat()
		}
		return m, nil
	case spinner.TickMsg:
		m.spin, cmd = m.spin.Update(msg)
		if m.busy && m.page == workshop {
			m.refreshChat()
		}
		return m, cmd
	case tickMsg:
		if !m.demo && !m.refreshing {
			m.refreshing = true
			return m, tea.Batch(clockTick(), m.refresh())
		}
		return m, clockTick()
	case reposMsg:
		m.repos = msg.repos
		if msg.err != nil {
			m.notice = "GitHub unavailable; showing local repos. " + msg.err.Error()
		}
		if !msg.remote {
			c, local := m.config, m.repos
			return m, tea.Batch(m.refresh(), func() tea.Msg { rs, e := orgRepos(c, local); return reposMsg{rs, e, true} })
		}
		m.repoIndex = min(m.repoIndex, max(0, len(m.filteredRepos())-1))
		return m, nil
	case runsMsg:
		m.refreshing = false
		selected, _ := m.selectedRun()
		m.runs = msg.runs
		for i, r := range m.runs {
			if r.ID == selected.ID {
				m.runIndex = i
				break
			}
		}
		m.runIndex = min(m.runIndex, max(0, len(m.runs)-1))
		if r, ok := m.selectedRun(); ok && r.ID == msg.id {
			m.logs.SetContent(safeText(msg.log))
			m.info.SetContent(runDetails(r))
			if m.following {
				m.logs.GotoBottom()
			}
		}
		return m, nil
	case assistantMsg:
		if msg.request != m.request {
			return m, nil
		}
		m.busy = false
		m.cancel = nil
		if msg.err != nil {
			m.refreshChat()
			m.notice = msg.err.Error() + " · ctrl+s retries, ctrl+p uses your text"
			return m, nil
		}
		m.notice = ""
		if msg.draft {
			m.refreshChat()
			m.prompt = msg.text
			m.page = review
			m.input.SetValue(msg.text)
			m.resize()
			cmd = m.input.Focus()
		} else {
			m.messages = append(m.messages, Message{Role: "assistant", Content: msg.text})
			m.updateChat()
		}
		if err := m.saveDraft(); err != nil {
			m.notice = err.Error()
		}
		return m, cmd
	case launchedMsg:
		m.busy = false
		if msg.err != nil {
			m.notice = msg.err.Error()
			return m, nil
		}
		m.page = board
		m.runs = append([]Run{msg.run}, m.runs...)
		m.runIndex = 0
		m.following = true
		m.details = false
		m.resize()
		m.logs.SetContent("Ralph is setting up the worktree…")
		m.notice = "Loop launched. You can close this window; Ralph keeps going."
		return m, m.refresh()
	case clonedMsg:
		m.busy = false
		if msg.err != nil {
			m.notice = msg.err.Error()
			return m, nil
		}
		for i, r := range m.repos {
			if r.Name == msg.repo.Name {
				m.repos[i] = msg.repo
			}
		}
		cmd = m.openWorkshop(msg.repo)
		return m, cmd
	case noticeMsg:
		if msg.err != nil {
			m.notice = msg.err.Error()
		} else {
			m.notice = msg.text
		}
		return m, nil
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			if err := m.saveDraft(); err != nil {
				m.notice = "Draft save failed: " + err.Error()
				return m, nil
			}
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		if m.help {
			if key == "?" || key == "esc" {
				m.help = false
			}
			return m, nil
		}
		if m.page == workshop || m.page == review {
			switch key {
			case "esc":
				if m.busy {
					if m.cancel != nil {
						m.cancel()
						m.cancel = nil
						m.request++
						m.busy = false
						m.refreshChat()
						m.notice = "Request cancelled. Your conversation is saved."
					}
					return m, nil
				}
				if err := m.saveDraft(); err != nil {
					m.notice = err.Error()
					return m, nil
				}
				if m.page == review {
					m.prompt = m.input.Value()
					m.page = workshop
					m.input.Reset()
					m.resize()
					m.updateChat()
				} else {
					m.page = repositories
					m.filter.Blur()
				}
				return m, nil
			case "enter", "ctrl+s":
				if m.page == workshop {
					cmd = m.ask(false)
					return m, cmd
				}
			case "shift+enter":
				if m.page == workshop || m.page == review {
					if m.busy {
						return m, nil
					}
					m.input, cmd = m.input.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
					return m, cmd
				}
			case "ctrl+d":
				if m.page == workshop {
					cmd = m.ask(true)
					return m, cmd
				}
			case "ctrl+p":
				if m.page == workshop && !m.busy {
					p := strings.TrimSpace(m.input.Value())
					if p == "" {
						p = m.prompt
					}
					if p == "" {
						for i := len(m.messages) - 1; i >= 0; i-- {
							if m.messages[i].Role == "user" {
								p = m.messages[i].Content
								break
							}
						}
					}
					if p == "" {
						m.notice = "Write a prompt first."
						return m, nil
					}
					m.prompt = p
					m.page = review
					m.input.SetValue(p)
					m.resize()
				}
				return m, nil
			case "ctrl+o":
				if m.page == review && !m.busy {
					m.settings = settingsForm(m.options, m.width-12)
					return m, m.settings.Init()
				}
				return m, nil
			case "ctrl+l":
				if m.page == review && !m.busy {
					if m.demo {
						m.notice = "Demo only. Launch the app without --demo to start a real run."
						return m, nil
					}
					if err := m.saveDraft(); err != nil {
						m.notice = err.Error()
						return m, nil
					}
					m.busy = true
					c, r, p, n := m.config, m.repo, m.input.Value(), m.options
					return m, func() tea.Msg { r, e := launchRun(c, r, p, n); return launchedMsg{r, e} }
				}
				return m, nil
			case "ctrl+b":
				if m.page == workshop {
					m.chat.PageUp()
					return m, nil
				}
			case "ctrl+f":
				if m.page == workshop {
					m.chat.PageDown()
					return m, nil
				}
			}
			if m.busy {
				return m, nil
			}
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		if m.page == repositories && m.filter.Focused() {
			if key == "esc" || key == "enter" {
				m.filter.Blur()
				return m, nil
			}
			m.filter, cmd = m.filter.Update(msg)
			m.repoIndex = 0
			return m, cmd
		}
		switch key {
		case "q":
			return m, tea.Quit
		case "?":
			m.help = true
			return m, nil
		case "1", "esc":
			m.page = board
			m.resize()
			return m, nil
		case "2", "n":
			m.page = repositories
			m.resize()
			return m, nil
		case "3":
			if m.repo.Name != "" {
				cmd = m.openWorkshop(m.repo)
				return m, cmd
			}
			m.page = repositories
			return m, nil
		case "/":
			if m.page == repositories {
				return m, m.filter.Focus()
			}
		case "r":
			if m.page == repositories && !m.demo {
				c, local := m.config, m.repos
				m.notice = "Refreshing organisation repos…"
				return m, func() tea.Msg { rs, e := orgRepos(c, local); return reposMsg{rs, e, true} }
			}
		case "j", "down", "k", "up":
			delta := 1
			if key == "k" || key == "up" {
				delta = -1
			}
			if m.page == repositories {
				m.repoIndex = max(0, min(len(m.filteredRepos())-1, m.repoIndex+delta))
			} else {
				m.runIndex = max(0, min(len(m.runs)-1, m.runIndex+delta))
				m.following = true
				m.logs.SetContent("Loading output…")
				m.info.GotoTop()
				if m.demo {
					m.logs.SetContent(demoLog)
				} else {
					return m, m.refresh()
				}
			}
			return m, nil
		case "enter":
			if m.page == repositories && !m.busy {
				rs := m.filteredRepos()
				if len(rs) == 0 {
					return m, nil
				}
				r := rs[m.repoIndex]
				if r.Archived {
					m.notice = "This repository is archived."
					return m, nil
				}
				if r.Path == "" && !m.demo {
					m.notice = "Press c to clone this repository, then open the workshop."
					return m, nil
				}
				cmd = m.openWorkshop(r)
				return m, cmd
			}
		case "c":
			if m.page == repositories && !m.busy && !m.demo {
				rs := m.filteredRepos()
				if len(rs) == 0 {
					return m, nil
				}
				r := rs[m.repoIndex]
				if r.Path != "" {
					return m, nil
				}
				m.busy = true
				m.notice = "Cloning " + r.Name + "…"
				c := m.config
				return m, func() tea.Msg { repo, e := cloneRepo(c, r); return clonedMsg{repo, e} }
			}
		case "x":
			if r, ok := m.selectedRun(); ok && m.page == board {
				if m.demo {
					m.notice = "Demo: Ralph will stop now."
					return m, nil
				}
				return m, func() tea.Msg { return noticeMsg{"Stopping now. The worktree is preserved.", requestAbort(r)} }
			}
		case "s":
			if r, ok := m.selectedRun(); ok && m.page == board {
				if m.demo {
					m.notice = "Demo: Ralph will stop after this iteration."
					return m, nil
				}
				return m, func() tea.Msg {
					return noticeMsg{"Stop requested. The current iteration will finish first.", requestStop(r)}
				}
			}
		case "f":
			m.following = true
			m.logs.GotoBottom()
			return m, nil
		case "tab":
			m.details = !m.details
			if r, ok := m.selectedRun(); ok {
				m.info.SetContent(runDetails(r))
			}
			return m, nil
		case "ctrl+b", "ctrl+f":
			if m.page == board {
				if m.details {
					if key == "ctrl+b" {
						m.info.PageUp()
					} else {
						m.info.PageDown()
					}
					return m, cmd
				}
				m.following = false
				if key == "ctrl+b" {
					m.logs.PageUp()
				} else {
					m.logs.PageDown()
				}
				return m, cmd
			}
		}
	}
	if m.page == workshop || m.page == review {
		m.input, cmd = m.input.Update(msg)
	} else if m.page == board {
		m.logs, cmd = m.logs.Update(msg)
	}
	return m, cmd
}

func safeText(s string) string {
	s = redactCredentials(ansi.Strip(s))
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || r == '\u202e' || r == '\u202d' {
			return -1
		}
		return r
	}, s)
}
func clip(s string, w int) string { return ansi.Truncate(safeText(s), max(1, w), "…") }
func statusStyle(s string) lipgloss.Style {
	switch s {
	case "running", "complete":
		return lipgloss.NewStyle().Foreground(green)
	case "failed", "interrupted", "aborted":
		return lipgloss.NewStyle().Foreground(red)
	case "cooldown", "stopping", "aborting", "budget reached":
		return lipgloss.NewStyle().Foreground(amber)
	}
	return dim
}
func age(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
func meter(n, total, width int) string {
	p := progress.New(progress.WithWidth(width), progress.WithColors(purple, green), progress.WithoutPercentage(), progress.WithFillCharacters('━', '─'))
	fraction := 0.0
	if total > 0 {
		fraction = min(1, float64(n)/float64(total))
	}
	return p.ViewAs(fraction)
}

func keyboardHelp(text string, width int) string {
	h := help.New()
	h.SetWidth(width)
	h.Styles.ShortKey = accent
	h.Styles.ShortDesc = dim
	h.Styles.ShortSeparator = dim
	bindings := []key.Binding{}
	for _, part := range strings.Split(text, "   ") {
		k, desc, ok := strings.Cut(part, " ")
		if ok {
			bindings = append(bindings, key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc)))
		}
	}
	return ansi.Truncate(h.ShortHelpView(bindings), width, "…")
}

func (m model) boardView() string {
	if len(m.runs) == 0 {
		return mainPanel("\n"+accent.Render("     ╭─────╮\n     │ ◕ ◕ │\n     ╰──┬──╯\n       ╱ ╲")+"\n\n     Nothing looping. Yet.\n\n     Pick a repo, talk through an idea, and send Ralph to work.\n     Each loop gets a fresh context and a worktree of its own.\n\n"+accent.Render("     n  Start your first loop")+"\n\n"+dim.Render("     Existing .ralph/logs folders appear here automatically."), m.width-6, m.height-12)
	}
	leftWidth := 32
	listHeight := max(1, (m.height-14)/4)
	if m.width < 100 {
		listHeight = 1
	}
	start := max(0, m.runIndex-listHeight+1)
	end := min(len(m.runs), start+listHeight)
	var rows []string
	for i := start; i < end; i++ {
		r := m.runs[i]
		title := clip(strings.TrimPrefix(r.Repo.Name, m.config.Org+"/"), leftWidth-6)
		prefix := "  "
		if i == m.runIndex {
			prefix = "▸ "
			title = chip(title, violet)
		}
		progress := fmt.Sprintf("%d/%d iterations", r.Iteration, r.Max)
		if r.External {
			progress = "script log · " + age(r.Updated)
		}
		indicator := "● "
		if r.active() {
			indicator = m.spin.View() + " "
		}
		rows = append(rows, prefix+title+"\n  "+indicator+statusStyle(r.Status).Render(r.Status)+"\n  "+dim.Render(progress))
	}
	left := chip(fmt.Sprintf("LOOPS  %02d", len(m.runs)), pink) + "\n\n" + strings.Join(rows, "\n\n")
	r, _ := m.selectedRun()
	title := accent.Render(clip(r.Repo.Name, max(24, m.width-55)))
	meta := statusStyle(r.Status).Render(r.Status) + "  " + meter(r.Iteration, r.Max, 12) + "  " + dim.Render(age(r.Started))
	view := scrolledView(m.logs)
	if m.details {
		view = scrolledView(m.info)
	}

	mode := "OUTPUT  ·  following"
	if !m.following {
		mode = "OUTPUT  ·  paused, f to follow"
	}
	if m.details {
		mode = "RUN DETAILS"
	}
	right := title + "\n" + meta + "\n\n" + chip(mode, mint) + "\n" + view
	if m.width < 100 {
		return panel.Width(m.width-6).Render(strings.Join(rows, "\n")) + "\n" + mainPanel(right, m.width-6, 0)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, panel.Width(34).Height(m.height-10).Render(left), " ", mainPanel(right, m.width-37, m.height-10))
}

func (m model) reposView() string {
	rs := m.filteredRepos()
	heading := chip("PICK YOUR NEXT RABBIT HOLE", pink) + "\n" + dim.Render(clip(fmt.Sprintf("%d repositories · %s", len(rs), m.config.ReposDir), m.width-10)) + "\n\n" + m.filter.View() + "\n\n"
	count := max(1, (m.height-19)/3)
	start := max(0, m.repoIndex-count+1)
	end := min(len(rs), start+count)
	var rows []string
	for i := start; i < end; i++ {
		r := rs[i]
		prefix := "  "
		name := clip(r.Name, m.width-30)
		if i == m.repoIndex {
			prefix = "▸ "
			name = chip(name, violet)
		}
		badge := "cloud"
		if r.Path != "" {
			badge = "local"
		}
		if r.Archived {
			badge = "archived"
		}
		desc := r.Description
		if desc == "" {
			desc = "A fresh loop is a good place to start."
		}
		rows = append(rows, prefix+name+"  "+chip(badge, mint)+"\n  "+dim.Render(clip(desc, m.width-14)))
	}
	if len(rs) == 0 {
		rows = append(rows, dim.Render("No matching repositories. Press r to refresh GitHub, or / to change the filter."))
	}
	return panel.Width(m.width - 6).Height(m.height - 12).Render(heading + strings.Join(rows, "\n\n"))
}

func demoData() ([]Repo, []Run) {
	repos := []Repo{{Name: "seankoji-com/zooma", Description: "A better way to buy and sell cars.", Path: "/demo/zooma"}, {Name: "seankoji-com/ralph", Description: "Small loops. Big ideas.", Path: "/demo/ralph"}, {Name: "seankoji-com/careynas.net", Description: "A homelab with a little too much ambition.", Path: "/demo/careynas.net"}, {Name: "seankoji-com/frugalbar", Description: "Keep an eye on what your AI is spending."}}
	now := time.Now()
	runs := []Run{{ID: "demo-search", Repo: repos[0], Status: "running", Iteration: 3, Max: 8, Started: now.Add(-12 * time.Minute), Model: "litellm/deepseek-v4-flash", Prompt: "Improve search keyboard navigation and test the complete flow.", Worktree: "~/.local/state/ralph/runs/demo-search/worktree", Branch: "codex/ralph-demo-search"}, {ID: "demo-docs", Repo: repos[2], Status: "cooldown", Iteration: 2, Max: 5, Started: now.Add(-25 * time.Minute)}, {ID: "demo-tests", Repo: repos[1], Status: "complete", Iteration: 4, Max: 6, Started: now.Add(-90 * time.Minute)}}
	return repos, runs
}

const demoLog = `ralph: worktree ready on codex/ralph-search
ralph: ── iteration 3/8 ──

Reading .ralph-ledger.md…
  ✓ Empty results state is covered.
  ✓ Search query persists when returning from a listing.
  → Next: keyboard navigation through suggestions.

Checking the existing combobox and focus behaviour.

  src/components/Search.tsx       +18 −6
  tests/search-keyboard.test.ts   +42

Running focused tests…
  PASS  arrow keys move between suggestions
  PASS  escape closes suggestions and restores focus
  PASS  enter opens the highlighted result

3 passed. Updating the ledger before the next loop.

Ralph is working. You can go make a coffee.`

// Keep os linked here for terminal snapshots without running the Bubble Tea event loop.
func renderSnapshot(c Config) { fmt.Fprintln(os.Stdout, newModel(c, true).View().Content) }

func runDetails(r Run) string {
	if r.External {
		return safeText("EXISTING SCRIPT\n\nLatest iteration log\n" + r.LogPath + "\n\nLast write " + age(r.Updated) + "\n\nThis script has no worker heartbeat. Ralph cannot determine whether it is running from its logs alone.\n\nStop it through its scripts/ralph/STOP file.")
	}
	return safeText("Run       " + r.ID + "\nState     " + r.Status + "\nModel     " + r.Model + "\nBranch    " + r.Branch + "\nWorktree  " + r.Worktree + "\nLogs      " + r.logPath() + "\n\n" + r.Error + "\n\nPROMPT\n" + r.Prompt)
}
