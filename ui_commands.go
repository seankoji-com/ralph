package main

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type commandPalette struct {
	input textinput.Model
	index int
}
type stationCommand struct{ id, label, key string }

func (m *model) openPalette() tea.Cmd {
	input := textinput.New()
	input.Placeholder = "Search commands…"
	input.SetVirtualCursor(true)
	m.palette = &commandPalette{input: input}
	return m.palette.input.Focus()
}
func (m model) commandItems() []stationCommand {
	items := []stationCommand{}
	if !m.busy && !m.help {
		items = append(items, stationCommand{"tab0", "Go to loops", "1"}, stationCommand{"tab1", "Choose a repository", "2"}, stationCommand{"tab2", "Open prompt workshop", "3"})
	}
	labels := map[string]string{"enter": "Open selected repository", "ctrl+d": "Draft a prompt with Ralph", "ctrl+p": "Review your prompt", "ctrl+l": "Launch this loop", "ctrl+o": "Change loop settings", "n": "Start a new loop", "tab": "Switch output / run details", "f": "Follow live output", "s": "Stop after this iteration", "?": "Show keyboard help", "c": "Clone selected repository", "r": "Refresh organisation repositories", "/": "Find a repository", "esc": "Go back"}
	if m.page == workshop {
		labels["enter"] = "Send message to Ralph"
	}
	for _, a := range m.actions() {
		if a.id == "more" || a.id == "less" {
			continue
		}
		if m.busy && a.id != "esc" {
			continue
		}
		if label, ok := labels[a.id]; ok {
			items = append(items, stationCommand{a.id, label, a.id})
		}
	}
	query := strings.ToLower(strings.TrimSpace(m.palette.input.Value()))
	filtered := []stationCommand{}
	for _, item := range items {
		match := true
		for _, word := range strings.Fields(query) {
			if !strings.Contains(strings.ToLower(item.label+" "+item.key), word) {
				match = false
				break
			}
		}
		if match {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
func (m model) updatePalette(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.palette = nil
		return m, nil
	case "up", "down", "tab", "shift+tab":
		delta := 1
		if k.String() == "up" || k.String() == "shift+tab" {
			delta = -1
		}
		m.palette.index = max(0, min(len(m.commandItems())-1, m.palette.index+delta))
		return m, nil
	case "enter":
		items := m.commandItems()
		if len(items) == 0 {
			return m, nil
		}
		id := items[min(m.palette.index, len(items)-1)].id
		m.palette = nil
		return m.executeCommand(id)
	}
	var cmd tea.Cmd
	m.palette.input, cmd = m.palette.input.Update(k)
	m.palette.index = 0
	return m, cmd
}
func (m model) executeCommand(id string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(id, "tab") && len(id) == 4 {
		return m.switchPage(int(id[3] - '0'))
	}
	if m.page == repositories {
		m.filter.Blur()
	}
	return m.Update(actionKey(id))
}
func (m model) switchPage(index int) (tea.Model, tea.Cmd) {
	if m.settings != nil || m.help {
		return m, nil
	}
	if m.busy {
		m.notice = "Finish or cancel the current request before switching tabs."
		return m, nil
	}
	if err := m.saveDraft(); err != nil {
		m.notice = err.Error()
		return m, nil
	}
	switch index {
	case 0:
		m.page = board
	case 1:
		m.page = repositories
		m.filter.Blur()
	case 2:
		if m.page == workshop || m.page == review {
			return m, nil
		}
		if m.repo.Name == "" {
			m.page = repositories
		} else {
			cmd := m.openWorkshop(m.repo)
			return m, cmd
		}
	}
	m.resize()
	return m, nil
}
func (m model) paletteLayout(base stationLayout) stationLayout {
	w, h := min(72, m.width-8), min(18, m.height-6)
	x, y := (m.width-w)/2, (m.height-h)/2
	input := m.palette.input
	input.SetWidth(w - 6)
	items := m.commandItems()
	count := h - 8
	start := max(0, m.palette.index-count+1)
	rows := []string{}
	l := stationLayout{}
	l.add("palette", x, y, w, h, 0)
	for i := start; i < min(len(items), start+count); i++ {
		item := items[i]
		key := dim.Render(item.key)
		name := clip(item.label, w-10-lipgloss.Width(key))
		row := name + strings.Repeat(" ", max(1, w-4-lipgloss.Width(name)-lipgloss.Width(key))) + key
		style := lipgloss.NewStyle().Width(w - 4)
		if i == m.palette.index {
			style = style.Background(violet).Foreground(ink).Bold(true)
		}
		rows = append(rows, style.Render(row))
		l.add("command", x+2, y+5+i-start, w-4, 1, i)
	}
	if len(rows) == 0 {
		rows = append(rows, dim.Render("No commands match. Try a different word."))
	}
	for len(rows) < count {
		rows = append(rows, "")
	}
	title := gradient("Commands") + dim.Render(fmt.Sprintf("  %d available", len(items)))
	popup := panel.BorderForeground(pink).Background(mainBackground).Width(w).Height(h).Render(title + "\n\n" + input.View() + "\n\n" + strings.Join(rows, "\n") + "\n\n" + keyboardHelp("↑/↓ choose   enter run   esc close", w-4))
	// Set the background on empty cells too; otherwise underlying output can bleed through.
	pw, ph := lipgloss.Size(popup)
	surface := lipgloss.NewCanvas(pw, ph).Compose(lipgloss.NewLayer(popup))
	for yy := 0; yy < ph; yy++ {
		for xx := 0; xx < pw; xx++ {
			cell := surface.CellAt(xx, yy)
			if cell != nil && cell.Style.Bg == nil {
				cell.Style.Bg = mainBackground
				surface.SetCell(xx, yy, cell)
			}
		}
	}
	backdrop := lipgloss.NewCanvas(m.width, m.height).Compose(lipgloss.NewLayer(base.content))
	fade := func(c color.Color) color.Color {
		r, g, b, _ := c.RGBA()
		br, bg, bb, _ := mainBackground.RGBA()
		return color.RGBA{uint8((r>>8)*2/5 + (br>>8)*3/5), uint8((g>>8)*2/5 + (bg>>8)*3/5), uint8((b>>8)*2/5 + (bb>>8)*3/5), 255}
	}
	for yy := 0; yy < m.height; yy++ {
		for xx := 0; xx < m.width; xx++ {
			cell := backdrop.CellAt(xx, yy)
			if cell == nil {
				continue
			}
			fg := cell.Style.Fg
			if fg == nil {
				fg = ink
			}
			cell.Style.Fg = fade(fg)
			if cell.Style.Bg != nil {
				cell.Style.Bg = fade(cell.Style.Bg)
			}
			backdrop.SetCell(xx, yy, cell)
		}
	}
	l.content = lipgloss.NewCanvas(m.width, m.height).Compose(lipgloss.NewCompositor(lipgloss.NewLayer(backdrop.Render()), lipgloss.NewLayer(surface.Render()).X(x).Y(y))).Render()

	return l
}
