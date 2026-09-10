package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestCommandsPreserveDraftAndRouteSelection(t *testing.T) {
	m := newModel(Config{}, true)
	m.openWorkshop(m.repos[0])
	m.input.SetValue("keep this exact draft")
	result, _ := m.Update(actionKey("ctrl+k"))
	m = result.(model)
	if m.palette == nil {
		t.Fatal("palette did not open")
	}
	for _, r := range "review prompt" {
		result, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = result.(model)
	}
	if m.input.Value() != "keep this exact draft" {
		t.Fatal("search leaked into editor")
	}
	if len(m.commandItems()) != 1 {
		t.Fatalf("matches: %+v", m.commandItems())
	}
	result, _ = m.Update(actionKey("enter"))
	m = result.(model)
	if m.palette != nil || m.page != review || m.input.Value() != "keep this exact draft" {
		t.Fatal("command did not review existing draft")
	}
	result, _ = m.Update(actionKey("ctrl+k"))
	m = result.(model)
	m.palette.input.SetValue("no such command")
	result, _ = m.Update(actionKey("enter"))
	m = result.(model)
	if m.palette == nil || m.page != review {
		t.Fatal("empty search executed an action")
	}
	result, _ = m.Update(actionKey("esc"))
	m = result.(model)
	if m.input.Value() != "keep this exact draft" {
		t.Fatal("cancel discarded draft")
	}
}

func TestCommandMouseAndBounds(t *testing.T) {
	for _, size := range [][2]int{{64, 24}, {110, 34}, {160, 50}} {
		m := newModel(Config{}, true)
		m.width, m.height = size[0], size[1]
		m.resize()
		m.openPalette()
		m.palette.input.SetValue("repository")
		l := m.layout()
		lines := strings.Split(l.content, "\n")
		if lipgloss.Width(l.content) > m.width || lipgloss.Height(l.content) > m.height {
			t.Fatalf("overlay exceeds %v", size)
		}
		found := false
		for _, r := range l.regions {
			if r.id == "command" {
				found = true
				row := ansi.Strip(ansi.Cut(lines[r.y], r.x, r.x+r.w))
				if !strings.Contains(row, "Choose a repository") {
					t.Fatalf("%v: command click target misses visible label: %q", size, row)
				}
				result, _ := m.Update(tea.MouseClickMsg{X: r.x, Y: r.y, Button: tea.MouseLeft})
				m = result.(model)
				if m.palette != nil || m.page != repositories {
					t.Fatal("click did not execute command")
				}
				break
			}
		}
		if !found {
			t.Fatal("no command row")
		}
	}
}

func TestConversationScrollSurvivesReply(t *testing.T) {
	m := newModel(Config{}, true)
	m.openWorkshop(m.repos[0])
	m.messages = []Message{{Role: "assistant", Content: strings.Repeat("A line to read.\n\n", 60)}}
	m.updateChat()
	m.chat.SetYOffset(6)
	m.messages = append(m.messages, Message{Role: "assistant", Content: "A new reply"})
	m.updateChat()
	if m.chat.YOffset() != 6 {
		t.Fatalf("reply moved reading position to %d", m.chat.YOffset())
	}
	m.chat.GotoBottom()
	m.messages = append(m.messages, Message{Role: "assistant", Content: "Another reply"})
	m.updateChat()
	if !m.chat.AtBottom() {
		t.Fatal("following conversation did not follow reply")
	}
}

func TestScrollbarClickAndWideEditor(t *testing.T) {
	m := newModel(Config{}, true)
	m.width, m.height = 160, 50
	m.openWorkshop(m.repos[0])
	if m.input.Width() >= m.width-35 {
		t.Fatal("editor overlaps context sidebar")
	}
	m.messages = []Message{{Role: "assistant", Content: strings.Repeat("Lots of text.\n\n", 70)}}
	m.updateChat()
	m.chat.GotoTop()
	for _, r := range m.layout().regions {
		if r.id == "chat-scroll" {
			result, _ := m.Update(tea.MouseClickMsg{X: r.x, Y: r.y + r.h - 1, Button: tea.MouseLeft})
			m = result.(model)
			if !m.chat.AtBottom() {
				t.Fatal("scrollbar did not jump to end")
			}
		}
	}
	m.height = 24
	m.resize()
	if m.workshopSidebar() {
		t.Fatal("short terminal should use compact layout")
	}
}

func TestPatternSnapshots(t *testing.T) {
	dir := os.Getenv("RALPH_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("optional visual snapshots")
	}
	for _, mode := range []string{"conversation", "commands", "thinking"} {
		m := newModel(Config{Org: "seankoji-com", AssistModel: "deepseek-v4.1-flash", Model: "litellm/deepseek-v4.1-flash"}, true)
		m.width, m.height = 160, 50
		m.openWorkshop(m.repos[0])
		m.messages = []Message{{Role: "user", Content: "Make repository search easier to use. Keep it focused."}, {Role: "assistant", Content: "Let's start with keyboard navigation and clear empty states.\n\n### Proposed scope\n- Match repository names and descriptions.\n- Keep selection visible as results change.\n- Explain how to recover when nothing matches.\n\nRun `go test ./...` after the change. Since Ralph will write code, include an independent OCR review before declaring the loop complete.\n\nShould the filter also include archived repositories?"}}
		m.updateChat()
		m.chat.GotoTop()
		if mode == "commands" {
			m.openPalette()
		}
		if mode == "thinking" {
			m.input.SetValue("Keep archived repositories out of search results.")
			m.ask(false)
			defer m.cancel()
		}
		err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("160x50-%s.ansi", mode)), []byte(m.View().Content), 0600)
		if err != nil {
			t.Fatal(err)
		}
	}
}
