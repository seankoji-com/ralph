package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestWorkshopFallbackAttributionSurvivesDraftRestore(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			fmt.Fprint(w, `{"data":[{"id":"reviewer"}]}`)
			return
		}
		var body struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, msg := range body.Messages {
			if _, ok := msg["provider"]; ok {
				t.Error("local attribution sent to API")
			}
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"A verified fixture reply."}}]}`)
	}))
	defer fallback.Close()
	c := Config{StateDir: t.TempDir(), Model: "litellm/coder", AssistModel: "assistant", BaseURL: primary.URL, DevPassURL: fallback.URL, DevPassAPIKey: "fixture"}
	m := newModel(c, false)
	repo := Repo{Name: "org/fixture"}
	m.openWorkshop(repo)
	m.input.SetValue("Keep this brief")
	cmd := m.ask(false)
	updated, _ := m.Update(cmd())
	m = updated.(model)
	if len(m.messages) != 2 || m.messages[1].Provider != "DevPass" || !strings.Contains(ansi.Strip(m.chat.View()), "DevPass") {
		t.Fatal("reply provider not shown")
	}
	saveFailoverSnapshot(t, m, "reply")
	cmd = m.ask(true)
	updated, _ = m.Update(cmd())
	m = updated.(model)
	if m.page != review || m.promptProvider != "DevPass" || !strings.Contains(ansi.Strip(m.layout().content), "Drafted via DevPass") {
		t.Fatal("draft provider not shown")
	}
	saveFailoverSnapshot(t, m, "draft")
	restored := newModel(c, false)
	restored.openWorkshop(repo)
	if len(restored.messages) != 2 || restored.messages[1].Provider != "DevPass" || restored.promptProvider != "DevPass" {
		t.Fatal("provider lost on draft restore")
	}
}

func TestFallbackVisibleBeforeSendingAndOnBoard(t *testing.T) {
	for _, size := range [][2]int{{64, 24}, {80, 30}, {110, 34}, {160, 50}} {
		m := newModel(Config{DevPassAPIKey: "fixture", Model: "litellm/coder"}, true)
		m.openWorkshop(m.repos[0])
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = updated.(model)
		if !strings.Contains(ansi.Strip(m.layout().content), "sends full conversation") {
			t.Fatalf("%v: workshop fallback disclosure missing", size)
		}
		m.page = review
		m.resize()
		if !strings.Contains(ansi.Strip(m.layout().content), "Outage retry sends full prompt to devpass/coder") {
			t.Fatalf("%v: launch fallback disclosure missing", size)
		}
		m.page = board
		m.runs = []Run{{ID: "fixture", Repo: Repo{Name: "org/fixture"}, Status: "running", Model: "litellm/coder", ActiveModel: "devpass/coder", Max: 2, Iteration: 1}}
		m.resize()
		if !strings.Contains(ansi.Strip(m.layout().content), "Fallback · devpass/coder") {
			t.Fatalf("%v: board fallback missing", size)
		}
		saveFailoverSnapshot(t, m, "board")
	}
}

func saveFailoverSnapshot(t *testing.T, m model, state string) {
	t.Helper()
	if dir := os.Getenv("RALPH_SNAPSHOT_DIR"); dir != "" {
		path := filepath.Join(dir, fmt.Sprintf("%dx%d-fallback-%s.ansi", m.width, m.height, state))
		if err := os.WriteFile(path, []byte(m.View().Content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDeleteQuitSavesDraftAndCancelsRequest(t *testing.T) {
	for _, saveFails := range []bool{false, true} {
		m := newModel(Config{StateDir: t.TempDir()}, false)
		m.openWorkshop(Repo{Name: "org/fixture"})
		m.input.SetValue("Keep my unsent work")
		m.pendingDelete = &Run{ID: "untouched"}
		cancelled := false
		m.cancel = func() { cancelled = true }
		if saveFails {
			if err := os.WriteFile(filepath.Join(m.config.StateDir, "drafts"), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		updated, cmd := m.Update(actionKey("ctrl+c"))
		m = updated.(model)
		if saveFails {
			if cmd != nil || cancelled || !strings.Contains(m.notice, "Draft save failed") || m.pendingDelete == nil {
				t.Fatal("failed save discarded state")
			}
			continue
		}
		if cmd == nil || !cancelled {
			t.Fatal("quit did not cancel")
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatal("not a quit command")
		}
		var saved draftState
		if err := readJSON(m.draftPath(), &saved); err != nil || saved.Input != "Keep my unsent work" {
			t.Fatalf("unsaved draft: %v", err)
		}
	}
}
