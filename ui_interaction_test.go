package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func pointer(t *testing.T, m model, id string, index int) model {
	t.Helper()
	for _, r := range m.layout().regions {
		if r.id == id && r.index == index {
			result, _ := m.Update(tea.MouseClickMsg{X: r.x, Y: r.y, Button: tea.MouseLeft})
			return result.(model)
		}
	}
	t.Fatalf("no hit region %s/%d", id, index)
	return m
}
func TestPointerNavigationAndActions(t *testing.T) {
	m := newModel(Config{}, true)
	m = pointer(t, m, "tab1", 1)
	if m.page != repositories {
		t.Fatal("repositories tab")
	}
	m = pointer(t, m, "repo", 1)
	if m.repoIndex != 1 {
		t.Fatal("select repository")
	}
	m = pointer(t, m, "enter", 0)
	if m.page != workshop {
		t.Fatal("open workshop")
	}
	m.input.SetValue("Build a useful thing")
	m = pointer(t, m, "ctrl+p", 0)
	if m.page != review || m.input.Value() != "Build a useful thing" {
		t.Fatal("use text")
	}
	m = pointer(t, m, "more", 0)
	if m.options.Max != 6 {
		t.Fatal("iteration button")
	}
	m = pointer(t, m, "ctrl+o", 0)
	if m.settings == nil {
		t.Fatal("open settings")
	}
	m = pointer(t, m, "esc", 0)
	if m.settings != nil {
		t.Fatal("cancel settings")
	}
}
func TestMouseScrollingAndMacKeys(t *testing.T) {
	m := newModel(Config{}, true)
	content := strings.Repeat("a line of output\n", 100)
	m.logs.SetContent(content)
	m.logs.GotoBottom()
	for _, r := range m.layout().regions {
		if r.id == "output" {
			before := m.logs.YOffset()
			result, _ := m.Update(tea.MouseWheelMsg{X: r.x, Y: r.y, Button: tea.MouseWheelUp})
			m = result.(model)
			if m.following || m.logs.YOffset() >= before {
				t.Fatal("wheel must pause follow and scroll output")
			}
		}
	}
	before := m.logs.YOffset()
	result, _ := m.Update(actionKey("ctrl+b"))
	m = result.(model)
	if m.logs.YOffset() >= before {
		t.Fatal("control B pages output back")
	}
	m.openWorkshop(m.repos[0])
	m.chat.SetContent(content)
	m.chat.GotoBottom()
	m.input.SetValue("untouched")
	before = m.chat.YOffset()
	result, _ = m.Update(actionKey("ctrl+b"))
	m = result.(model)
	if m.chat.YOffset() >= before || m.input.Value() != "untouched" {
		t.Fatal("conversation scrolling changed composer")
	}
}
func TestPointerEditorSelection(t *testing.T) {
	m := newModel(Config{}, true)
	m.openWorkshop(m.repos[0])
	m.input.SetValue("hello world")
	for _, r := range m.layout().regions {
		if r.id == "input" {
			result, _ := m.Update(tea.MouseClickMsg{X: r.x + 2, Y: r.y, Button: tea.MouseLeft})
			m = result.(model)
			result, _ = m.Update(tea.MouseMotionMsg{X: r.x + 7, Y: r.y, Button: tea.MouseLeft})
			m = result.(model)
			result, _ = m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft})
			m = result.(model)
			if m.input.SelectedText() != "hello" {
				t.Fatalf("selection = %q", m.input.SelectedText())
			}
		}
	}
}
func TestVisibleActionRegions(t *testing.T) {
	for _, size := range [][2]int{{64, 24}, {80, 30}, {110, 34}, {160, 50}} {
		for _, page := range []screen{board, repositories, workshop, review} {
			m := newModel(Config{Org: "seankoji-com", AssistModel: "deepseek-v4-flash"}, true)
			m.width, m.height = size[0], size[1]
			m.page = page
			m.repo = m.repos[0]
			m.resize()
			m.updateChat()
			l := m.layout()
			lines := strings.Split(l.content, "\n")
			for _, r := range l.regions {
				if r.y+r.h > m.height-1 {
					t.Errorf("%v page %d: %s extends offscreen", size, page, r.id)
				}
				if r.y == m.height-4 {
					text := ansi.Strip(ansi.Cut(lines[r.y], r.x, r.x+r.w))
					for _, a := range m.actions() {
						if a.id == r.id && !strings.Contains(text, a.label) {
							t.Errorf("%v page %d: button %s hit %q instead of %q", size, page, r.id, text, a.label)
						}
					}
				}
			}
		}
	}
}

// Optional snapshots contain demo data only and render the actual Lip Gloss view.
func TestVisualSnapshots(t *testing.T) {
	dir := os.Getenv("RALPH_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("set RALPH_SNAPSHOT_DIR for visual inspection")
	}
	for _, size := range [][2]int{{110, 34}, {160, 50}, {64, 24}} {
		for _, page := range []screen{board, repositories, workshop, review} {
			m := newModel(Config{Org: "seankoji-com", AssistModel: "deepseek-v4-flash"}, true)
			m.width, m.height = size[0], size[1]
			m.page = page
			m.repo = m.repos[0]
			m.resize()
			m.updateChat()
			if page == review {
				m.input.SetValue("Improve repository search so it matches names and descriptions.\n\nKeep the change focused, cover empty results, and verify the existing tests.\nUse an independent OCR reviewer before declaring the work complete.")
			}
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%dx%d-%d.ansi", size[0], size[1], page)), []byte(m.View().Content), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestStarterPreservesExistingInput(t *testing.T) {
	m := newModel(Config{}, true)
	m.openWorkshop(m.repos[0])
	m = pointer(t, m, "starter", 1)
	first := m.input.Value()
	if !strings.Contains(first, "feature") {
		t.Fatal("starter did not seed the composer")
	}
	m = pointer(t, m, "starter", 0)
	if m.input.Value() != first {
		t.Fatal("starter overwrote a draft")
	}
}
