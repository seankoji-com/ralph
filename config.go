package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Org, ReposDir, StateDir, Runner, Model string
	BaseURL, APIKey, AssistModel           string
}

func envOr(key, fallback string) string {
	if s := os.Getenv(key); s != "" {
		return s
	}
	return fallback
}

func loadConfig() Config {
	home, _ := os.UserHomeDir()
	c := Config{
		Org:         envOr("RALPH_ORG", "seankoji-com"),
		ReposDir:    envOr("RALPH_REPOS_DIR", filepath.Join(home, "repos")),
		StateDir:    envOr("RALPH_STATE_DIR", filepath.Join(envOr("XDG_STATE_HOME", filepath.Join(home, ".local", "state")), "ralph")),
		Runner:      envOr("RALPH_RUNNER", "opencode2"),
		Model:       envOr("RALPH_MODEL", "litellm/deepseek-v4-flash"),
		AssistModel: envOr("RALPH_ASSIST_MODEL", "deepseek-v4-flash"),
	}
	// Reuse the existing provider in memory; never copy credentials into run records.
	paths := []string{
		filepath.Join(envOr("OPENCODE2_ROOT", filepath.Join(home, ".local/share/opencode2")), "config/opencode/opencode.json"),
		filepath.Join(envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "opencode/opencode.json"),
	}
	for _, path := range paths {
		var d struct {
			Provider map[string]struct {
				Options struct {
					BaseURL string `json:"baseURL"`
					APIKey  string `json:"apiKey"`
				} `json:"options"`
			} `json:"provider"`
		}
		b, err := os.ReadFile(path)
		if err != nil || json.Unmarshal(b, &d) != nil {
			continue
		}
		p, ok := d.Provider["litellm"]
		if !ok || p.Options.BaseURL == "" {
			continue
		}
		c.BaseURL, c.APIKey = p.Options.BaseURL, resolveSecret(p.Options.APIKey)
		break
	}
	c.BaseURL = envOr("RALPH_LITELLM_URL", c.BaseURL)
	c.APIKey = envOr("RALPH_LITELLM_API_KEY", envOr("LITELLM_API_KEY", c.APIKey))
	return c
}

func resolveSecret(s string) string {
	if strings.HasPrefix(s, "{env:") && strings.HasSuffix(s, "}") {
		return os.Getenv(strings.TrimSuffix(strings.TrimPrefix(s, "{env:"), "}"))
	}
	if strings.HasPrefix(s, "{file:") && strings.HasSuffix(s, "}") {
		path := strings.TrimSuffix(strings.TrimPrefix(s, "{file:"), "}")
		if strings.HasPrefix(path, "~/") {
			home, _ := os.UserHomeDir()
			path = filepath.Join(home, path[2:])
		}
		b, _ := os.ReadFile(path)
		return strings.TrimSpace(string(b))
	}
	return s
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".ralph-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	return nil
}
