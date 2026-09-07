package main

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestHoveredActionsRemainPlainLabels(t *testing.T) {
	for _, page := range []screen{board, repositories, workshop, review} {
		m := newModel(Config{}, true)
		m.page = page
		m.resize()
		for _, action := range m.actions() {
			m.hover = action.id
			l := m.layout()
			lines := strings.Split(l.content, "\n")
			found := false
			for _, r := range l.regions {
				if r.id == action.id {
					found = true
					label := strings.TrimSpace(ansi.Strip(ansi.Cut(lines[r.y], r.x, r.x+r.w)))
					if label != action.label {
						t.Errorf("hover %s: visible label %q, want %q", action.id, label, action.label)
					}
					if r.w != len([]rune(action.label))+2 {
						t.Errorf("hover %s changed button width to %d", action.id, r.w)
					}
				}
			}
			if !found {
				t.Errorf("hover %s hid its button", action.id)
			}
		}
	}
}

func TestThinkingAppearsAfterMessageAndDisappears(t *testing.T) {
	m := newModel(Config{}, true)
	m.openWorkshop(m.repos[0])
	m.input.SetValue("Improve search")
	m.ask(false)
	if m.cancel != nil {
		defer m.cancel()
	}
	view := ansi.Strip(m.chat.View())
	if !strings.Contains(view, "Thinking") || strings.Index(view, "Thinking") < strings.Index(view, "Improve search") {
		t.Fatalf("pending response missing below message: %q", view)
	}
	if !m.chat.AtBottom() {
		t.Fatal("send did not reveal pending response")
	}
	if len(m.messages) != 1 {
		t.Fatal("pending response must not enter saved history")
	}
	result, _ := m.Update(assistantMsg{request: m.request, text: "Let's start with keyboard navigation."})
	m = result.(model)
	if strings.Contains(ansi.Strip(m.chat.View()), "Thinking") {
		t.Fatal("pending response remained after reply")
	}
	m.ask(false)
	if m.cancel != nil {
		defer m.cancel()
	}
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = result.(model)
	if m.busy || strings.Contains(ansi.Strip(m.chat.View()), "Thinking") {
		t.Fatal("pending response remained after cancellation")
	}
}

func TestThinkingAnimationAndFailure(t *testing.T) {
	m := newModel(Config{}, true)
	m.openWorkshop(m.repos[0])
	m.input.SetValue("Check search")
	m.ask(false)
	defer m.cancel()
	before := m.chat.View()
	result, _ := m.Update(m.spin.Tick())
	m = result.(model)
	if before == m.chat.View() {
		t.Fatal("pending spinner did not animate")
	}
	m.messages[0].Content = strings.Repeat("Earlier conversation.\n\n", 40)
	m.updateChat()
	m.chat.SetYOffset(4)
	result, _ = m.Update(m.spin.Tick())
	m = result.(model)
	if m.chat.YOffset() != 4 {
		t.Fatal("spinner moved reading position")
	}
	result, _ = m.Update(assistantMsg{request: m.request - 1, text: "stale reply"})
	m = result.(model)
	if !m.busy {
		t.Fatal("stale reply cleared thinking state")
	}
	result, _ = m.Update(assistantMsg{request: m.request, err: errors.New("timeout")})
	m = result.(model)
	m.chat.GotoBottom()
	if m.busy || strings.Contains(ansi.Strip(m.chat.View()), "Thinking") {
		t.Fatal("failure left pending response behind")
	}
	if !strings.Contains(m.notice, "timeout") {
		t.Fatal("failure explanation disappeared")
	}
}

func TestRemovedSelectionClearsOldOutput(t *testing.T) {
	m := newModel(Config{}, true)
	m.runs = []Run{{ID: "A"}, {ID: "B"}, {ID: "C"}}
	m.runIndex = 1
	m.logs.SetContent("B's output")
	updated, cmd := m.Update(runsMsg{runs: []Run{{ID: "A"}, {ID: "C"}}, id: "B", log: "B's output"})
	m = updated.(model)
	if r, _ := m.selectedRun(); r.ID != "A" {
		t.Fatalf("selection=%s", r.ID)
	}
	if cmd == nil || strings.Contains(m.logs.View(), "B's output") {
		t.Fatal("stale output retained or refresh missing")
	}
}
