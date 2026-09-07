package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func actionKey(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	}
	if strings.HasPrefix(name, "ctrl+") {
		return tea.KeyPressMsg{Code: []rune(strings.TrimPrefix(name, "ctrl+"))[0], Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(name)[0], Text: name}
}

func (m model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.width < 64 || m.height < 24 {
		return m, nil
	}
	mouse := msg.Mouse()
	layout := m.layout()
	hit := layout.hit(mouse.X, mouse.Y)
	if m.pendingDelete != nil && hit.id != "y" && hit.id != "esc" {
		return m, nil
	}
	if m.palette != nil {
		switch msg.(type) {
		case tea.MouseClickMsg:
			if mouse.Button != tea.MouseLeft {
				return m, nil
			}
			if hit.id == "command" {
				items := m.commandItems()
				if hit.index < len(items) {
					id := items[hit.index].id
					m.palette = nil
					return m.executeCommand(id)
				}
			}
			if hit.id == "" {
				m.palette = nil
			}
		case tea.MouseWheelMsg:
			delta := 0
			if mouse.Button == tea.MouseWheelDown {
				delta = 1
			}
			if mouse.Button == tea.MouseWheelUp {
				delta = -1
			}
			m.palette.index = max(0, min(len(m.commandItems())-1, m.palette.index+delta))
		}
		return m, nil
	}

	switch msg.(type) {
	case tea.MouseMotionMsg:
		m.hover = hit.id
		if m.dragging && !m.busy {
			for _, r := range layout.regions {
				if r.id == "input" {
					m.input.ExtendSelection(mouse.X-r.x, mouse.Y-r.y)
					break
				}
			}
		}
		return m, nil
	case tea.MouseReleaseMsg:
		if m.dragging {
			m.input.EndSelection()
			m.dragging = false
		}
		return m, nil
	case tea.MouseWheelMsg:
		down := mouse.Button == tea.MouseWheelDown
		if !down && mouse.Button != tea.MouseWheelUp {
			return m, nil
		}
		switch hit.id {
		case "chat", "starter", "chat-scroll":
			if down {
				m.chat.ScrollDown(3)
			} else {
				m.chat.ScrollUp(3)
			}
		case "output", "output-scroll":
			view := &m.logs
			if m.details {
				view = &m.info
			} else {
				m.following = false
			}
			if down {
				view.ScrollDown(3)
			} else {
				view.ScrollUp(3)
			}
		case "repo", "run", "repolist", "runlist":
			delta := -1
			if down {
				delta = 1
			}
			if hit.id == "repo" || hit.id == "repolist" {
				m.repoIndex = max(0, min(len(m.filteredRepos())-1, m.repoIndex+delta))
			} else {
				return m.selectRun(max(0, min(len(m.runs)-1, m.runIndex+delta)))
			}
		}
		return m, nil
	case tea.MouseClickMsg:
		if mouse.Button != tea.MouseLeft {
			return m, nil
		}
	default:
		return m, nil
	}
	if hit.id == "" {
		return m, nil
	}
	if strings.HasPrefix(hit.id, "tab") {
		return m.switchPage(hit.index)
	}
	switch hit.id {
	case "commands":
		if m.settings != nil {
			return m, nil
		}
		cmd := m.openPalette()
		return m, cmd
	case "chat-scroll":
		scrollToPointer(&m.chat, mouse.Y-hit.y, hit.h)
		return m, nil
	case "output-scroll":
		if m.details {
			scrollToPointer(&m.info, mouse.Y-hit.y, hit.h)
		} else {
			m.following = false
			scrollToPointer(&m.logs, mouse.Y-hit.y, hit.h)
		}
		return m, nil
	case "starter":
		if m.busy {
			return m, nil
		}
		if strings.TrimSpace(m.input.Value()) != "" {
			m.notice = "Your idea is already in the composer. Edit it or send it to Ralph."
			return m, nil
		}
		ideas := []string{"Help me investigate and fix a bug in this repository. Ask me about the symptoms first.", "Help me plan a new feature for this repository. Ask me what I want to build first.", "Help me find a focused cleanup in this repository. Let's agree on scope and checks before changing code."}
		m.input.SetValue(ideas[hit.index])
		cmd := m.input.Focus()
		return m, cmd
	case "input":
		if !m.busy {
			cmd := m.input.Focus()
			m.input.BeginSelection(mouse.X-hit.x, mouse.Y-hit.y)
			m.dragging = true
			return m, cmd
		}
		return m, nil
	case "filter":
		cmd := m.filter.Focus()
		return m, cmd
	case "repo":
		m.filter.Blur()
		// One click selects; clicking the selected repository opens its workshop.
		if m.repoIndex == hit.index {
			return m.Update(actionKey("enter"))
		}
		m.repoIndex = hit.index
		return m, nil
	case "run":
		return m.selectRun(hit.index)
	case "more":
		if !m.busy {
			m.options.Max = min(100, m.options.Max+1)
		}
		return m, nil
	case "less":
		if !m.busy {
			m.options.Max = max(1, m.options.Max-1)
		}
		return m, nil
	case "chat", "output", "repolist", "runlist":
		return m, nil
	}
	if m.page == repositories {
		m.filter.Blur()
	}
	return m.Update(actionKey(hit.id))
}

func (m model) selectRun(index int) (tea.Model, tea.Cmd) {
	if index == m.runIndex {
		return m, nil
	}
	m.runIndex = index
	m.following = true
	m.info.GotoTop()
	if r, ok := m.selectedRun(); ok {
		m.info.SetContent(runDetails(r))
	}
	if m.demo {
		m.logs.SetContent(demoLog)
		m.logs.GotoBottom()
		return m, nil
	}
	m.logs.SetContent("Loading output…")
	return m, m.refresh()
}
