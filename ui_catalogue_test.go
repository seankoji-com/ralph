package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestDeleteDialogRenderedTargetsAndIsolation(t *testing.T) {
	for _, size := range [][2]int{{64, 24}, {80, 30}, {110, 34}, {160, 50}} {
		for _, confirm := range []bool{false, true} {
			m := newModel(Config{}, true)
			m.runs = []Run{{ID: "20260908-120000-example", Repo: Repo{Name: "org/project"}, Prompt: "Repair keyboard navigation", Status: "failed"}}
			result, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m = result.(model)
			result, _ = m.Update(actionKey("d"))
			m = result.(model)
			layout := m.layout()
			if lipgloss.Width(layout.content) > size[0] || lipgloss.Height(layout.content) > size[1] {
				t.Fatalf("dialog exceeds %v", size)
			}
			plain := ansi.Strip(layout.content)
			for _, text := range []string{"Remove this loop?", m.runs[0].ID, "cannot be undone", "Repair keyboard navigation"} {
				if !strings.Contains(plain, text) {
					t.Fatalf("%v: missing dialog content %q", size, text)
				}
			}
			if len(layout.regions) != 2 {
				t.Fatal("background controls remain clickable during confirmation")
			}
			for _, region := range layout.regions {
				row := strings.Split(layout.content, "\n")[region.y]
				label := ansi.Strip(ansi.Cut(row, region.x, region.x+region.w))
				want := "Keep loop"
				if region.id == "y" {
					want = "Remove loop"
				}
				if !strings.Contains(label, want) {
					t.Fatalf("%v: %s target hits %q", size, region.id, label)
				}
				if (region.id == "y") == confirm {
					result, _ = m.Update(tea.MouseClickMsg{X: region.x + 1, Y: region.y, Button: tea.MouseLeft})
					m = result.(model)
				}
			}
			if m.pendingDelete != nil || len(m.runs) != map[bool]int{false: 1, true: 0}[confirm] {
				t.Fatalf("%v: confirmation=%t did not produce expected board", size, confirm)
			}
		}
	}
}

func TestDeleteDialogKeyboardAndRefresh(t *testing.T) {
	for _, key := range []string{"ctrl+k", "ctrl+c", "Y"} {
		m := newModel(Config{}, true)
		m.runs = []Run{{ID: "original", Status: "complete"}}
		result, _ := m.Update(actionKey("d"))
		m = result.(model)
		m.runs = append([]Run{{ID: "new", Status: "complete"}}, m.runs...)
		result, cmd := m.Update(actionKey(key))
		m = result.(model)
		if m.palette != nil {
			t.Fatal("palette opened over destructive confirmation")
		}
		if key == "ctrl+c" {
			if cmd == nil {
				t.Fatal("quit swallowed by confirmation")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatal("confirmation did not quit")
			}
		}
		if key == "Y" && (len(m.runs) != 1 || m.runs[0].ID != "new") {
			t.Fatal("refresh changed deletion target")
		}
	}
}

func TestContextualActionsDoNotAdvertiseImpossibleOperations(t *testing.T) {
	for _, state := range []string{"empty", "external", "complete", "orphaned", "busy"} {
		m := newModel(Config{}, true)
		switch state {
		case "empty":
			m.runs = nil
		case "external":
			m.runs = []Run{{External: true, Status: "external"}}
		case "complete", "orphaned":
			m.runs = []Run{{Status: state}}
		case "busy":
			m.page, m.busy = workshop, true
		}
		for _, a := range m.actions() {
			if a.id == "s" || a.id == "x" || (a.id == "d" && state != "complete") {
				t.Errorf("%s offers unavailable action %s", state, a.id)
			}
			if state == "busy" && a.id != "esc" {
				t.Errorf("busy composer offers %s", a.id)
			}
		}
	}
}

func TestWorkshopFailureRemainsReadableAndRetryable(t *testing.T) {
	m := newModel(Config{}, true)
	m.openWorkshop(m.repos[0])
	m.input.SetValue("Preserve this brief")
	m.ask(false)
	m.cancel()
	reason := "Provider unavailable: " + strings.Repeat("long diagnostic detail ", 12)
	result, _ := m.Update(assistantMsg{request: m.request, err: errors.New(reason)})
	m = result.(model)
	if !strings.Contains(ansi.Strip(m.chat.View()), "Reply interrupted") || !strings.Contains(m.workshopError, reason) {
		t.Fatal("provider failure only appears in a truncated footer")
	}
	if m.actions()[0].id != "ctrl+s" {
		t.Fatal("retry action missing")
	}
	result, cmd := m.Update(actionKey("ctrl+s"))
	m = result.(model)
	defer m.cancel()
	if cmd == nil || !m.busy || m.workshopError != "" || len(m.messages) != 1 {
		t.Fatal("retry lost or duplicated the brief, or retained the old error")
	}
}

func TestRepositoryPrimaryActionMatchesAvailability(t *testing.T) {
	for _, tc := range []struct {
		name, path, want string
		archived         bool
	}{
		{"local", "/fixture/repo", "enter", false},
		{"remote", "", "c", false},
		{"archived", "", "/", true},
	} {
		m := newModel(Config{}, true)
		m.page = repositories
		m.repos = []Repo{{Name: tc.name, Path: tc.path, Archived: tc.archived}}
		if got := m.actions()[0].id; got != tc.want {
			t.Errorf("%s primary action=%s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestCatalogueSnapshots(t *testing.T) {
	dir := os.Getenv("RALPH_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("optional design evidence")
	}
	for _, size := range [][2]int{{64, 24}, {110, 34}, {160, 50}} {
		for _, state := range []string{"empty", "failed", "delete"} {
			m := newModel(Config{Org: "seankoji-com"}, true)
			result, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m = result.(model)
			if state == "empty" {
				m.runs = nil
			} else {
				m.runs = []Run{{ID: "20260908-120000-example", Repo: m.repos[0], Prompt: "Make search work with a keyboard", Status: "failed", Iteration: 1, Max: 5, Error: "Provider unavailable. Open Details to inspect the saved run."}}
				m.logs.SetContent("The agent stopped. Worktree and logs are preserved.")
				if state == "delete" {
					result, _ = m.Update(actionKey("d"))
					m = result.(model)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%dx%d-%s.ansi", size[0], size[1], state)), []byte(m.View().Content), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
