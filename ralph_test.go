package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestMain(m *testing.M) {
	if len(os.Args) >= 4 && os.Args[1] == "guardian" {
		if err := guardian(os.Args[2], os.Args[3:]); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestAssistantConversation(t *testing.T) {
	var received struct {
		Model    string    `json:"model"`
		Messages []Message `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing provider auth")
		}
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"deepseek-v4-flash"},{"id":"independent-reviewer"}]}`)
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing auth")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Bounded prompt."}}]}`)
	}))
	defer server.Close()
	c := Config{BaseURL: server.URL + "/v1/", APIKey: "test-key", AssistModel: "deepseek-v4-flash", Model: "litellm/deepseek-v4-flash"}
	answer, err := askAssistant(context.Background(), c, Repo{Name: "org/repo"}, []Message{{Role: "user", Content: "Fix search"}, {Role: "assistant", Content: "Which part?"}, {Role: "user", Content: "Keyboard"}}, true)
	if err != nil || answer != "Bounded prompt." {
		t.Fatalf("answer %q, err %v", answer, err)
	}
	if received.Model != c.AssistModel || len(received.Messages) != 5 || received.Messages[3].Content != "Keyboard" {
		t.Fatalf("conversation lost: %+v", received)
	}
	system := received.Messages[0].Content
	if !strings.Contains(system, "Open Code Review (OCR)") || !strings.Contains(system, `["independent-reviewer"]`) || !strings.Contains(system, c.Model) {
		t.Fatalf("missing live review guidance: %s", system)
	}
}

func TestAssistantErrorDoesNotExposeProxyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, "secret-should-not-leak")
	}))
	defer server.Close()
	_, err := askAssistant(context.Background(), Config{BaseURL: server.URL, AssistModel: "test"}, Repo{}, nil, false)
	if err == nil || strings.Contains(err.Error(), "secret-should-not-leak") || !strings.Contains(err.Error(), "401") {
		t.Fatal(err)
	}
}

func TestAssistantCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := askAssistant(ctx, Config{BaseURL: "http://127.0.0.1:1/v1"}, Repo{}, nil, false)
	if err == nil {
		t.Fatal("cancelled request succeeded")
	}
}

func TestGitHubNames(t *testing.T) {
	for _, remote := range []string{"git@github.com:org/repo.git", "https://github.com/org/repo.git", "ssh://git@github.com/org/repo"} {
		if got := githubName(remote); got != "org/repo" {
			t.Errorf("%s: %s", remote, got)
		}
	}
	if githubName("https://evil.example/org/repo") != "" {
		t.Fatal("accepted non-GitHub remote")
	}
	for _, bad := range []string{"../repo", "org/../../tmp", "--bad", "org/repo;echo hi"} {
		if repoNameRE.MatchString(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
	_, err := cloneRepo(Config{ReposDir: t.TempDir()}, Repo{Name: "org/../../tmp"})
	if err == nil {
		t.Fatal("accepted traversal")
	}
}

func fixtureRun(t *testing.T, script string, max int) Run {
	t.Helper()
	root := t.TempDir()
	ctx := context.Background()
	git := func(dir string, args ...string) string {
		t.Helper()
		s, e := command(ctx, dir, "git", args...)
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	remote := filepath.Join(root, "origin.git")
	git(root, "init", "--bare", "--initial-branch=main", remote)
	repo := filepath.Join(root, "repo")
	git(root, "clone", remote, repo)
	git(repo, "config", "user.email", "ralph-test@example.invalid")
	git(repo, "config", "user.name", "Ralph test")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(repo, "add", "README")
	git(repo, "commit", "-m", "fixture")
	git(repo, "push", "-u", "origin", "main")
	git(repo, "remote", "set-head", "origin", "-a")
	// Unrelated dirty work must stay in the user's original checkout.
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("uncommitted user work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "runs", "fixture")
	script = strings.ReplaceAll(script, "__RUN_DIR__", dir)
	runner := filepath.Join(root, "fake-runner")
	if err := os.WriteFile(runner, []byte("#!/bin/sh\nset -eu\n"+script+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	r := Run{ID: "fixture", Repo: Repo{Name: "org/repo", Path: repo}, Prompt: "A literal prompt: $(touch should-never-exist)", Runner: runner, Model: "fake/test", Status: "queued", Max: max, Timeout: 1, Started: time.Now(), Updated: time.Now(), Dir: dir, Worktree: filepath.Join(root, "worktrees", "fixture"), Branch: "codex/ralph-test"}
	if err := writeJSON(filepath.Join(dir, "run.json"), r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWorkerFreshIterationsAndIsolation(t *testing.T) {
	r := fixtureRun(t, `printf '%s\n' "$@" > args.txt
count=0
if [ -f .ralph-ledger.md ]; then count=$(cat .ralph-ledger.md); fi
count=$((count + 1))
printf '%s' "$count" > .ralph-ledger.md
if [ "$count" -eq 2 ]; then printf '{"token":"%s","iteration":%s}' "$RALPH_COMPLETION_TOKEN" "$RALPH_ITERATION" > .ralph-complete.json; fi
echo trailing-runner-banner`, 4)
	if err := worker(r.Dir); err != nil {
		t.Fatal(err)
	}
	var got Run
	if err := readJSON(filepath.Join(r.Dir, "run.json"), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "complete" || got.Iteration != 2 {
		t.Fatalf("state: %+v", got)
	}
	b, _ := os.ReadFile(filepath.Join(r.Repo.Path, "README"))
	if string(b) != "uncommitted user work\n" {
		t.Fatal("original checkout changed")
	}
	b, _ = os.ReadFile(filepath.Join(r.Worktree, "README"))
	if string(b) != "fixture\n" {
		t.Fatal("worktree not from default branch")
	}
	b, _ = os.ReadFile(filepath.Join(r.Worktree, "args.txt"))
	if strings.Contains(string(b), "--continue") || !strings.Contains(string(b), r.Prompt) {
		t.Fatal("runner arguments lost fresh context or literal prompt")
	}
	if _, err := os.Stat(filepath.Join(r.Worktree, "should-never-exist")); !os.IsNotExist(err) {
		t.Fatal("prompt was interpreted by shell")
	}
	if err := worker(r.Dir); err == nil {
		t.Fatal("completed worker restarted")
	}
}

func TestWorkerFailureAndBudget(t *testing.T) {
	for _, tc := range []struct {
		name, script, status string
		fails                bool
	}{{"failure", "exit 7", "failed", true}, {"budget", "echo still-working", "budget reached", false}} {
		t.Run(tc.name, func(t *testing.T) {
			r := fixtureRun(t, tc.script, 1)
			err := worker(r.Dir)
			if (err != nil) != tc.fails {
				t.Fatalf("err=%v", err)
			}
			var got Run
			_ = readJSON(filepath.Join(r.Dir, "run.json"), &got)
			if got.Status != tc.status {
				t.Fatalf("state=%s", got.Status)
			}
		})
	}
}

func TestWorkerGracefulStop(t *testing.T) {
	r := fixtureRun(t, `echo current-iteration-finished
touch "__RUN_DIR__/STOP"`, 4)
	if err := worker(r.Dir); err != nil {
		t.Fatal(err)
	}
	var got Run
	_ = readJSON(filepath.Join(r.Dir, "run.json"), &got)
	if got.Status != "stopped" || got.Iteration != 1 {
		t.Fatalf("state=%+v", got)
	}
}

func TestLostHeartbeatAndExternalLogs(t *testing.T) {
	c := Config{StateDir: t.TempDir()}
	dir := filepath.Join(c.StateDir, "runs", "old")
	r := Run{ID: "old", Status: "running", Updated: time.Now().Add(-time.Hour), Started: time.Now()}
	if err := writeJSON(filepath.Join(dir, "run.json"), r); err != nil {
		t.Fatal(err)
	}
	repo := Repo{Name: "org/legacy", Path: t.TempDir()}
	logdir := filepath.Join(repo.Path, ".ralph", "logs")
	_ = os.MkdirAll(logdir, 0700)
	_ = os.WriteFile(filepath.Join(logdir, "20260907-100000-01.log"), []byte("one\n"), 0600)
	_ = os.WriteFile(filepath.Join(logdir, "20260907-100000-02.log"), []byte("two\n"), 0600)
	rs := loadRuns(c, []Repo{repo})
	if len(rs) != 2 {
		t.Fatalf("runs=%+v", rs)
	}
	for _, r := range rs {
		if r.External {
			if r.Status != "external" || requestStop(r) == nil {
				t.Fatal("unsafe external controls")
			}
		} else if r.Status != "interrupted" {
			t.Fatalf("stale run=%s", r.Status)
		}
	}
}

func TestTailAndTerminalSafety(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	_ = os.WriteFile(p, []byte(strings.Repeat("0123456789\n", 100)+"last\n"), 0600)
	tail := tailFile(p, 32)
	if len(tail) > 32 || !strings.HasSuffix(tail, "last\n") {
		t.Fatalf("tail=%q", tail)
	}
	if safeText("\x1b[31mhello\x1b[0m\x1b]52;c;payload\a\x00") != "hello" {
		t.Fatal("terminal control sequence survived")
	}

}

func TestDraftRecoveryAndDemoIsolation(t *testing.T) {
	c := Config{StateDir: t.TempDir()}
	r := Repo{Name: "org/repo", Path: "/repo"}
	m := newModel(c, false)
	m.openWorkshop(r)
	m.messages = []Message{{Role: "user", Content: "Keep my context"}}
	m.input.SetValue("unfinished thought")
	if err := m.saveDraft(); err != nil {
		t.Fatal(err)
	}
	restored := newModel(c, false)
	restored.openWorkshop(r)
	if restored.input.Value() != "unfinished thought" || len(restored.messages) != 1 {
		t.Fatal("draft not restored")
	}
	m.demo = true
	m.input.SetValue("demo must not persist")
	if err := m.saveDraft(); err != nil {
		t.Fatal(err)
	}
	var d draftState
	_ = readJSON(m.draftPath(), &d)
	if d.Input != "unfinished thought" {
		t.Fatal("demo wrote a draft")
	}
}

func TestScreenBounds(t *testing.T) {
	for _, size := range [][2]int{{64, 24}, {80, 30}, {110, 34}, {160, 50}} {
		for _, page := range []screen{board, repositories, workshop, review} {
			t.Run(fmt.Sprintf("%dx%d-page%d", size[0], size[1], page), func(t *testing.T) {
				m := newModel(Config{Org: "seankoji-com", AssistModel: "deepseek-v4-flash"}, true)
				m.page = page
				m.width, m.height = size[0], size[1]
				m.repo = m.repos[0]
				m.resize()
				m.updateChat()
				view := m.View().Content
				if w := lipgloss.Width(view); w > m.width {
					t.Errorf("width %d > %d", w, m.width)
					for _, line := range strings.Split(view, "\n") {
						if lipgloss.Width(line) > m.width {
							t.Log(safeText(line))
						}
					}
				}
				if h := lipgloss.Height(view); h > m.height {
					t.Errorf("height %d > %d", h, m.height)
				}
			})
		}
	}
}

func TestWorkshopEditingDoesNotTriggerNavigation(t *testing.T) {
	m := newModel(Config{}, true)
	m.openWorkshop(m.repos[0])
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = updated.(model)
	if m.page != workshop || m.input.Value() != "q" {
		t.Fatal("q quit the editor")
	}
	m.input.SetValue("Build something useful")
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = updated.(model)
	if m.page != review || m.input.Value() != "Build something useful" {
		t.Fatal("manual prompt did not enter review")
	}
}

func TestSettingsRetainFinalField(t *testing.T) {
	opts := RunOptions{Max: 7, Cooldown: 9, Timeout: 42, Model: "litellm/deepseek-v4-flash"}
	f := settingsForm(opts, 90)
	if got := optionsFromForm(f); got != opts {
		t.Fatalf("unvisited field values lost: %+v", got)
	}
	f.timeout = "0"
	if optionsFromForm(f).validate() == nil {
		t.Fatal("accepted invalid timeout")
	}
}
