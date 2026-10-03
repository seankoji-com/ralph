package main

import (
	"context"
	"testing"
)

func TestCommandDisablesInteractivePrompts(t *testing.T) {
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	t.Setenv("GH_PROMPT_DISABLED", "")
	out, err := command(context.Background(), "", "sh", "-c", `printf '%s %s' "$GIT_TERMINAL_PROMPT" "$GH_PROMPT_DISABLED"`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "0 1" {
		t.Fatalf("child env = %q, want GIT_TERMINAL_PROMPT=0 and GH_PROMPT_DISABLED=1", out)
	}
}
