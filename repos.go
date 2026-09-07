package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Repo struct {
	Name        string `json:"full_name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Archived    bool   `json:"archived"`
}

var repoNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*/[A-Za-z0-9_.-]*[A-Za-z0-9_-]$`)

func command(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(string(b)), nil
}

func localRepos(c Config) []Repo {
	entries, _ := os.ReadDir(c.ReposDir)
	repos := []Repo{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(c.ReposDir, entry.Name())
		if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
			continue
		}
		// Read the configured URL before Git applies insteadOf rewrites.
		remote, err := command(ctx, path, "git", "config", "--get", "remote.origin.url")
		if err != nil {
			continue
		}
		name := githubName(remote)
		if !repoNameRE.MatchString(name) || !strings.EqualFold(strings.Split(name, "/")[0], c.Org) {
			continue
		}
		repos = append(repos, Repo{Name: name, Path: path})
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
	return repos
}

func githubName(remote string) string {
	for _, prefix := range []string{"https://github.com/", "git@github.com:", "ssh://git@github.com/"} {
		if strings.HasPrefix(remote, prefix) {
			return strings.TrimSuffix(strings.TrimPrefix(remote, prefix), ".git")
		}
	}
	return ""
}

func orgRepos(c Config, local []Repo) ([]Repo, error) {
	if !repoNameRE.MatchString(c.Org + "/repo") {
		return nil, fmt.Errorf("invalid organisation name")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	// REST pagination avoids GraphQL's separate, often exhausted quota.
	out, err := command(ctx, "", "gh", "api", "-X", "GET", "--paginate", "--slurp", "orgs/"+c.Org+"/repos?per_page=100&type=all")
	if err != nil {
		return local, err
	}
	var pages [][]Repo
	if err = json.Unmarshal([]byte(out), &pages); err != nil {
		return local, err
	}
	byName := map[string]Repo{}
	for _, page := range pages {
		for _, r := range page {
			if repoNameRE.MatchString(r.Name) {
				byName[strings.ToLower(r.Name)] = r
			}
		}
	}
	for _, r := range local {
		key := strings.ToLower(r.Name)
		v, ok := byName[key]
		if !ok {
			v = r
		}
		v.Path = r.Path
		byName[key] = v
	}
	repos := make([]Repo, 0, len(byName))
	for _, r := range byName {
		repos = append(repos, r)
	}
	sort.Slice(repos, func(i, j int) bool {
		if (repos[i].Path != "") != (repos[j].Path != "") {
			return repos[i].Path != ""
		}
		return repos[i].Name < repos[j].Name
	})
	return repos, nil
}

func cloneRepo(c Config, r Repo) (Repo, error) {
	if !repoNameRE.MatchString(r.Name) {
		return r, fmt.Errorf("invalid repository name")
	}
	path := filepath.Join(c.ReposDir, strings.Split(r.Name, "/")[1])
	if _, err := os.Lstat(path); err == nil {
		return r, fmt.Errorf("%s already exists; refusing to overwrite it", path)
	} else if !os.IsNotExist(err) {
		return r, err
	}
	if err := os.MkdirAll(c.ReposDir, 0755); err != nil {
		return r, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, err := command(ctx, "", "gh", "repo", "clone", r.Name, path)
	if err != nil {
		return r, err
	}
	r.Path = path
	return r, nil
}
