package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func stubGitHubCLI(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestOrgReposMergesPaginatedResultsWithLocalPaths(t *testing.T) {
	stubGitHubCLI(t, `printf '%s' '[[{"full_name":"org/zulu","description":"remote info"},{"full_name":"../unsafe"}],[{"full_name":"ORG/ALPHA","archived":true}]]'`)
	local := []Repo{{Name: "org/alpha", Path: "/fixture/alpha"}, {Name: "org/local", Path: "/fixture/local"}}
	got, err := orgRepos(Config{Org: "org"}, local)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Name != "ORG/ALPHA" || got[0].Path != local[0].Path || !got[0].Archived || got[1] != local[1] || got[2].Description != "remote info" {
		t.Fatalf("merged repos=%+v", got)
	}
}

func TestOrgReposKeepsLocalResultsOnCLIAndDecodeFailure(t *testing.T) {
	for _, script := range []string{"echo forbidden >&2; exit 1", "echo '{bad-json'"} {
		t.Run(script, func(t *testing.T) {
			stubGitHubCLI(t, script)
			local := []Repo{{Name: "org/local", Path: "/fixture/local"}}
			got, err := orgRepos(Config{Org: "org"}, local)
			if err == nil || !reflect.DeepEqual(got, local) {
				t.Fatalf("repos=%+v err=%v", got, err)
			}
		})
	}
}

func TestCloneRefusesExistingDirectoryAndSymlinkWithoutInvokingCLI(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "invoked")
	t.Setenv("CLI_MARKER", marker)
	stubGitHubCLI(t, `printf called > "$CLI_MARKER"`)
	for _, name := range []string{"directory", "link"} {
		path := filepath.Join(root, name)
		if name == "directory" {
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.Symlink(filepath.Join(root, "missing-target"), path); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := cloneRepo(Config{ReposDir: root}, Repo{Name: "org/" + name}); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("CLI invoked for existing path")
	}
}

func TestCloneFailureDoesNotClaimLocalCheckout(t *testing.T) {
	stubGitHubCLI(t, "echo denied >&2; exit 1")
	got, err := cloneRepo(Config{ReposDir: t.TempDir()}, Repo{Name: "org/repo"})
	if err == nil || got.Path != "" || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("repo=%+v err=%v", got, err)
	}
}
