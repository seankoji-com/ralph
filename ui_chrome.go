package main

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	pink               = lipgloss.Color("#FF4FA3")
	violet             = lipgloss.Color("#7948ED")
	mint               = lipgloss.Color("#37E6B5")
	cyan               = lipgloss.Color("#69DDF4")
	composerBackground = lipgloss.Color("#36215A")
)

func chipStyle(colour color.Color) lipgloss.Style {
	fg := mainBackground
	if colour == violet {
		fg = ink
	}
	return lipgloss.NewStyle().Background(colour).Foreground(fg).Bold(true).Padding(0, 1)
}

func chip(text string, colour color.Color) string { return chipStyle(colour).Render(text) }

func workshopWelcome(width, height int) string {
	title := gradient("r a l p h  /  prompt workshop")
	intro := "Bring an idea. Ralph will help shape the brief."
	if height < 10 {
		return title + "\n\n" + intro + "\n" + lipgloss.NewStyle().Foreground(mint).Render("What would you like to make better?")
	}
	cardWidth := (width - 2) / 3
	colours := []color.Color{pink, violet, cyan}
	titles := []string{"Fix a bug", "Build a feature", "Tidy up"}
	subtitles := []string{"Find the cause.\nMake it stay fixed.", "Start small.\nShip something useful.", "Clear the clutter.\nKeep what works."}
	cards := make([]string, 3)
	for i := range cards {
		fg := mainBackground
		if i == 1 {
			fg = ink
		}
		cards[i] = lipgloss.NewStyle().Background(colours[i]).Foreground(fg).Padding(1, 1).Width(cardWidth).Height(6).Render(lipgloss.NewStyle().Bold(true).Render(titles[i]) + "\n\n" + subtitles[i])
	}
	return title + "\n" + intro + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, cards[0], " ", cards[1], " ", cards[2]) + "\n\n" + chip("YOU BRING THE IDEA", mint) + "  Ralph helps with scope, checks and a stopping point."
}

type hitRegion struct {
	id                string
	x, y, w, h, index int
}

func (r hitRegion) contains(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

type stationLayout struct {
	content string
	regions []hitRegion
}

func (l *stationLayout) add(id string, x, y, w, h, index int) {
	l.regions = append(l.regions, hitRegion{id, x, y, w, h, index})
}
func (l stationLayout) hit(x, y int) hitRegion {
	for i := len(l.regions) - 1; i >= 0; i-- {
		if l.regions[i].contains(x, y) {
			return l.regions[i]
		}
	}
	return hitRegion{}
}

type stationAction struct {
	id, label string
	colour    color.Color
}

func (m model) actions() []stationAction {
	if m.settings != nil {
		return []stationAction{{"enter", "Enter  Next / save", mint}, {"shift+tab", "Previous", violet}, {"esc", "Esc  Cancel", pink}}
	}
	if m.help {
		return []stationAction{{"esc", "Esc  Close help", pink}}
	}
	switch m.page {
	case workshop:
		return []stationAction{{"enter", "Enter  Send", pink}, {"ctrl+d", "⌃D  Draft", violet}, {"ctrl+p", "⌃P  Use text", mint}, {"esc", "Esc  Back", cyan}}
	case review:
		return []stationAction{{"ctrl+l", "⌃L  Launch", mint}, {"ctrl+o", "⌃O  Settings", violet}, {"less", "−", cyan}, {"more", "+", cyan}, {"esc", "Esc  Back", pink}}
	case repositories:
		return []stationAction{{"enter", "Enter  Open", pink}, {"/", "/  Find", violet}, {"c", "C  Clone", mint}, {"r", "R  Refresh", cyan}, {"esc", "Esc  Loops", violet}}
	default:
		return []stationAction{{"n", "N  New loop", pink}, {"tab", "Tab  Details", violet}, {"f", "F  Follow", mint}, {"s", "S  Stop", amber}, {"?", "?  Help", cyan}}
	}
}

func (m model) layout() stationLayout {
	l := stationLayout{}
	width := m.width - 2
	brand := chip("r a l p h", pink)
	label := " LOOP STATION"
	if m.demo {
		label += " / DEMO"
	}
	commands := chip("⌃K  Commands", violet)
	header := brand + lipgloss.NewStyle().Background(violet).Foreground(ink).Bold(true).Width(width-lipgloss.Width(brand)-lipgloss.Width(commands)).Render(label) + commands
	l.add("commands", m.width-1-lipgloss.Width(commands), 0, lipgloss.Width(commands), 1, 0)
	selected := int(m.page)
	if m.page == review {
		selected = 2
	}
	names := []string{"1  Loops", "2  Repositories", "3  Workshop"}
	tabs := []string{}
	x := 1
	for i, name := range names {
		colour := violet
		if i == selected {
			colour = pink
		}
		style := lipgloss.NewStyle().Background(colour).Foreground(ink).Bold(true).Padding(0, 2)
		if i == selected {
			style = style.Foreground(mainBackground)
		}
		if m.hover == fmt.Sprintf("tab%d", i) {
			style = style.Underline(true)
		}
		tab := style.Render(name)
		l.add(fmt.Sprintf("tab%d", i), x, 2, lipgloss.Width(tab), 1, i)
		x += lipgloss.Width(tab) + 1
		tabs = append(tabs, tab)
	}
	top := header + "\n\n" + strings.Join(tabs, " ") + "\n\n"
	var body string
	if m.settings != nil {
		body = chip("TUNE YOUR LOOP", pink) + "\n" + dim.Render("A little ambition. A sensible stopping point.") + "\n\n" + panel.Width(m.width-6).Render(m.settings.View())
	} else if m.help {
		body = panel.Width(m.width - 6).Height(m.height - 10).Render(chip("MAKE YOURSELF AT HOME", pink) + "\n\nClick tabs, actions and list rows. Scroll with your mouse or trackpad.\nDrag in the prompt to select text.\n\nctrl+k       Search commands\n1 / 2 / 3    Loops / repositories / workshop\nn            Start with a repository\n/            Filter repositories; enter exits search\nc            Clone selected remote repository\n↑ / ↓        Select a repository or run\ntab          Switch output / run details\nctrl+b / f   Scroll output or conversation back / forward\nf            Follow live output\ns            Stop after the current iteration\n\nWorkshop     enter send · shift+enter newline · ctrl+d draft\nLaunch       ctrl+o settings · ctrl+l launch · esc back\n\nq / ctrl+c   Leave the station. Detached loops keep running.")
	} else {
		switch m.page {
		case board:
			body = m.boardView()
			if len(m.runs) > 0 {
				if m.width >= 100 {
					l.add("runlist", 3, 5, 30, m.height-12, 0)
				} else {
					l.add("runlist", 3, 5, m.width-10, 3, 0)
				}
				count := max(1, (m.height-14)/4)
				if m.width < 100 {
					count = 1
				}
				start := max(0, m.runIndex-count+1)
				for i := start; i < min(len(m.runs), start+count); i++ {
					y := 7 + (i-start)*4
					w := 30
					if m.width < 100 {
						y = 5
						w = m.width - 10
					}
					l.add("run", 3, y, w, 3, i)
				}
				if m.width >= 100 {
					l.add("output", 38, 9, m.logs.Width(), m.logs.Height(), 0)
					l.add("output-scroll", 38+m.logs.Width(), 9, 1, m.logs.Height(), 0)
				} else {
					l.add("output", 3, 14, m.logs.Width(), m.logs.Height(), 0)
					l.add("output-scroll", 3+m.logs.Width(), 14, 1, m.logs.Height(), 0)
				}
			}
		case repositories:
			body = m.reposView()
			l.add("repolist", 3, 10, m.width-10, max(1, m.height-19), 0)
			l.add("filter", 3, 8, m.width-10, 1, 0)
			count := max(1, (m.height-19)/3)
			start := max(0, m.repoIndex-count+1)
			for i := start; i < min(len(m.filteredRepos()), start+count); i++ {
				l.add("repo", 3, 10+(i-start)*3, m.width-10, 2, i)
			}
		case workshop:
			heading := chip("PROMPT WORKSHOP", pink) + " " + lipgloss.NewStyle().Foreground(cyan).Render(clip(m.repo.Name, m.width-33))
			chat := mainPanel(scrolledView(m.chat), m.workshopWidth(), 0)
			editor := m.composerView()
			body = heading + "\n" + dim.Render(m.config.AssistModel+" · conversation saved locally") + "\n" + chat + "\n" + editor
			if m.workshopSidebar() {
				left := heading + "\n" + dim.Render("Talk through the idea. Save a brief. Start a loop.") + "\n" + chat + "\n" + editor
				left = lipgloss.NewStyle().Width(m.workshopWidth()).MaxWidth(m.workshopWidth()).Render(left)
				body = lipgloss.JoinHorizontal(lipgloss.Top, left, " ", m.workshopContext(lipgloss.Height(left)))
			}
			l.add("chat", 3, 7, m.chat.Width(), m.chat.Height(), 0)
			l.add("chat-scroll", 3+m.chat.Width(), 7, 1, m.chat.Height(), 0)
			if len(m.messages) == 0 && m.chat.Height() >= 10 {
				cardWidth := (m.chat.Width() - 2) / 3
				for i := 0; i < 3; i++ {
					y := 11 - m.chat.YOffset()
					top := max(7, y)
					bottom := min(7+m.chat.Height(), y+6)
					if bottom > top {
						l.add("starter", 3+i*(cardWidth+1), top, cardWidth, bottom-top, i)
					}
				}
			}
			l.add("input", 3, 7+lipgloss.Height(chat), m.input.Width(), m.input.Height(), 0)
		case review:
			editor := m.composerView()
			body = chip("READY TO RALPH?", mint) + " " + lipgloss.NewStyle().Foreground(cyan).Render(clip(m.repo.Name, m.width-25)) + "\n" + dim.Render("Edit the prompt. Every fresh agent will receive this brief.") + "\n" + editor + "\n" + chip(fmt.Sprintf("%d iterations", m.options.Max), violet) + " " + dim.Render(fmt.Sprintf("%ds rest · %dm per iteration · %s", m.options.Cooldown, m.options.Timeout, m.options.Model)) + "\n" + dim.Render("New worktree from origin's default branch · --auto.") + "\n" + dim.Render("Agent can edit files and run tools. Worktree and logs are kept.")
			l.add("input", 3, 7, m.input.Width(), m.input.Height(), 0)
		}
	}
	content := lipgloss.NewStyle().Height(m.height - 5).MaxHeight(m.height - 5).Render(top + body)
	active := 0
	for _, r := range m.runs {
		if r.active() {
			active++
		}
	}
	status := chip(fmt.Sprintf("● %d LOOPING", active), mint)
	notice := m.notice
	if notice == "" {
		notice = "Ready when you are."
	}
	right := chip(clip(m.config.Org, max(8, width/4)), violet)
	statusWidth := max(1, width-lipgloss.Width(status)-lipgloss.Width(right))
	status += lipgloss.NewStyle().Background(composerBackground).Foreground(ink).Width(statusWidth).Render(clip(" "+notice, statusWidth)) + right
	var buttons []string
	x = 1
	for _, a := range m.actions() {
		button := chipStyle(a.colour).Underline(m.hover == a.id).Render(a.label)
		w := lipgloss.Width(button)
		if x+w > m.width-1 {
			break
		}
		l.add(a.id, x, lipgloss.Height(content)+1, w, 1, 0)
		x += w + 1
		buttons = append(buttons, button)
	}
	hint := "wheel scroll   ctrl+b/ctrl+f page   ctrl+c quit"
	if m.page == workshop {
		hint = "shift+enter newline   ctrl+b/ctrl+f conversation   drag select   ctrl+c quit"
	}
	if m.page == review {
		hint = "−/+ iterations   ctrl+o settings   drag select   ctrl+c quit"
	}
	if m.page == repositories {
		hint = "↑/↓ select   click select/open   wheel scroll   ctrl+c quit"
	}
	if m.settings != nil {
		hint = "tab next field   shift+tab previous field   ctrl+c quit"
	}
	content += "\n" + status + "\n" + strings.Join(buttons, " ") + "\n" + keyboardHelp(hint, width)
	l.content = lipgloss.NewStyle().Padding(0, 1).MaxWidth(m.width).Foreground(ink).Render(content)
	if m.palette != nil {
		return m.paletteLayout(l)
	}
	return l
}

func (m model) View() tea.View {
	if m.width < 64 || m.height < 24 {
		v := tea.NewView("Ralph needs a little room.\nResize to at least 64 × 24.\nctrl+c to leave; running loops keep going.")
		v.AltScreen = true
		return v
	}
	v := tea.NewView(m.layout().content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	v.WindowTitle = "Ralph · Loop station"
	v.BackgroundColor = mainBackground
	return v
}

// Fill unstyled viewport padding as well as text cells, so ANSI resets inside
// the Bubbles textarea cannot punch dark holes in the composer surface.
func (m model) composerView() string {
	width := m.width - 6
	if m.page == workshop {
		width = m.workshopWidth()
	}
	frame := panel.BorderForeground(pink).Width(width).Render(m.input.View())
	w, h := lipgloss.Size(frame)
	canvas := lipgloss.NewCanvas(w, h).Compose(lipgloss.NewLayer(frame))
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			cell := canvas.CellAt(x, y)
			if cell != nil && cell.Style.Bg == nil {
				cell.Style.Bg = composerBackground
				canvas.SetCell(x, y, cell)
			}
		}
	}
	return canvas.Render()
}
