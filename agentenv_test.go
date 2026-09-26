package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAgentEnvKeepsOnlyAllowlistAndRequiredNames(t *testing.T) {
	environ := []string{"PATH=/bin", "HOME=/home/fixture", "LC_ALL=C", "OPENCODE2_ROOT=/oc", "SSH_AUTH_SOCK=/agent",
		"RALPH_LITELLM_API_KEY=fixture-litellm", "DEVPASS_API_KEY=fixture-devpass", "GH_TOKEN=fixture-gh", "PROVIDER_KEY=fixture-provider"}
	got := agentEnv(environ, []string{"PROVIDER_KEY"})
	want := []string{"PATH=/bin", "HOME=/home/fixture", "LC_ALL=C", "OPENCODE2_ROOT=/oc", "SSH_AUTH_SOCK=/agent", "PROVIDER_KEY=fixture-provider"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestRunnerEnvNamesFollowRunnerConfigAndOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OPENCODE2_ROOT", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "empty"))
	t.Setenv("RALPH_AGENT_ENV", " EXTRA_ONE, ,EXTRA_TWO")
	path := filepath.Join(root, "config/opencode/opencode.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"provider":{"litellm":{"options":{"apiKey":"{env:RUNNER_KEY}"}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := runnerEnvNames(); !slices.Equal(got, []string{"RUNNER_KEY", "EXTRA_ONE", "EXTRA_TWO"}) {
		t.Fatalf("got %q", got)
	}
}

func TestE2EAgentDoesNotInheritRalphCredentials(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OPENCODE2_ROOT", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "empty"))
	t.Setenv("RALPH_AGENT_ENV", "")
	t.Setenv("RALPH_LITELLM_API_KEY", "fixture-litellm-key")
	t.Setenv("DEVPASS_API_KEY", "fixture-devpass-key")
	r := fixtureRun(t, `env > "__RUN_DIR__/agent-env"`, 1)
	if err := worker(r.Dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(r.Dir, "agent-env"))
	if err != nil {
		t.Fatal(err)
	}
	env := string(data)
	if strings.Contains(env, "fixture-litellm-key") || strings.Contains(env, "fixture-devpass-key") || !strings.Contains(env, "RALPH_COMPLETION_TOKEN=") || !strings.Contains(env, "PATH=") {
		t.Fatalf("agent env: %s", env)
	}
}
