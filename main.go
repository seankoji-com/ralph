package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

func main() {
	if len(os.Args) >= 4 && os.Args[1] == "guardian" {
		if err := guardian(os.Args[2], os.Args[3:]); err != nil {
			fmt.Fprintln(os.Stderr, redactCredentials(err.Error()))
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "worker" {
		if err := worker(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, redactCredentials(err.Error()))
			os.Exit(1)
		}
		return
	}
	demo := flag.Bool("demo", false, "explore the UI with sample runs, without agents or API calls")
	snapshot := flag.Bool("snapshot", false, "print a demo frame and exit")
	doctor := flag.Bool("doctor", false, "check local tools and provider configuration")
	checkAI := flag.Bool("check-ai", false, "send a small test request to the configured LiteLLM model")
	listModels := flag.Bool("models", false, "list models visible on the current prompt provider")
	prune := flag.Bool("prune", false, "remove finished runs older than 30 days only when worktrees are clean and branches are merged")
	colour := flag.String("color", "auto", "terminal colours: auto, always (true colour), or never")
	flag.Parse()
	var programOptions []tea.ProgramOption
	switch *colour {
	case "auto":
	case "always":
		programOptions = append(programOptions, tea.WithColorProfile(colorprofile.TrueColor))
	case "never":
		programOptions = append(programOptions, tea.WithColorProfile(colorprofile.ASCII))
	default:
		fmt.Fprintln(os.Stderr, "ralph: --color must be auto, always or never")
		os.Exit(2)
	}
	c := loadConfig()
	if *prune {
		removed, kept, err := pruneRuns(c, time.Now().Add(-30*24*time.Hour))
		if err != nil {
			fmt.Fprintln(os.Stderr, redactCredentials(err.Error()))
			os.Exit(1)
		}
		fmt.Printf("Removed %d old runs; preserved %d recent, unfinished, or unsafe-to-remove runs.\n", removed, kept)
		return
	}
	if *snapshot {
		renderSnapshot(c)
		return
	}
	if *doctor {
		fmt.Printf("Ralph · doctor\nOrganisation: %s\nRepos: %s\nState: %s\nRunner model: %s\nAssistant model: %s\n", c.Org, c.ReposDir, c.StateDir, c.Model, c.AssistModel)
		for _, name := range []string{"git", "gh", c.Runner} {
			p, err := exec.LookPath(name)
			if err != nil {
				fmt.Printf("MISSING %s\n", name)
			} else {
				fmt.Printf("OK      %s (%s)\n", name, p)
			}
		}
		fmt.Printf("LiteLLM endpoint configured: %t\nLiteLLM key configured: %t\n", c.BaseURL != "", c.APIKey != "")
		fmt.Printf("Local org repos: %d\n", len(localRepos(c)))
		return
	}
	if *checkAI {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		answer, err := askAssistant(ctx, c, Repo{Name: c.Org + "/ralph"}, []Message{{Role: "user", Content: "Reply with only: Ralph is ready."}}, false)
		if err != nil {
			fmt.Fprintln(os.Stderr, redactCredentials(err.Error()))
			os.Exit(1)
		}
		fmt.Println(safeText(answer))
		return
	}
	if *listModels {
		ids, err := providerModels(context.Background(), c)
		if err != nil {
			fmt.Fprintln(os.Stderr, redactCredentials(err.Error()))
			os.Exit(1)
		}
		for _, id := range ids {
			fmt.Println(safeText(id))
		}
		return
	}
	if _, err := tea.NewProgram(newModel(c, *demo), programOptions...).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "ralph:", err)
		os.Exit(1)
	}
}
