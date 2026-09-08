package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	Provider string `json:"provider,omitempty"`
}

const workshopSystem = `You are Ralph's prompt workshop, a practical coding partner. Help the user turn a rough idea into a bounded prompt for an autonomous coding loop. Discuss tradeoffs, ask one useful question at a time, and preserve the user's choices. You only draft; you have no tools and cannot inspect the repository. Never claim to have read code. A loop starts a fresh agent context each iteration in a dedicated git worktree; the agent carries progress in .ralph-ledger.md. Prompts should state the goal, constraints, validation and a clear stopping condition. Never add publishing, merging, purchases or sending messages unless the user requested them. When asked to draft, output only the ready-to-run prompt, without fences or preamble. Keep conversation concise.`

func askAssistant(ctx context.Context, c Config, repo Repo, history []Message, draft bool) (string, string, error) {
	models, discoveryErr := providerModels(ctx, c)
	if ctx.Err() != nil {
		return "", "", ctx.Err()
	}
	msgs := []Message{{Role: "system", Content: workshopSystem + "\nSelected repository: " + repo.Name + "\n\n" + reviewGuidance(c.Model, models, discoveryErr)}}
	for _, msg := range history {
		// Provider attribution belongs to local history, not the API schema.
		msgs = append(msgs, Message{Role: msg.Role, Content: msg.Content})
	}
	if draft {
		msgs = append(msgs, Message{Role: "user", Content: "Write the final loop prompt based on our conversation. Output only the prompt."})
	}
	body, err := json.Marshal(map[string]any{"model": c.AssistModel, "messages": msgs, "max_tokens": 3000, "temperature": 0.6})
	if err != nil {
		return "", "", err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	resp, provider, err := providerRequest(requestCtx, c, http.MethodPost, "chat/completions", body)
	if err != nil {
		return "", provider, err
	}
	defer resp.Body.Close()
	// Do not surface a proxy's raw error body: it may echo credentials or headers.
	if resp.StatusCode != http.StatusOK {
		return "", provider, fmt.Errorf("%s returned HTTP %d; check endpoint, key and model %s", provider, resp.StatusCode, c.AssistModel)
	}
	var result struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&result); err != nil {
		return "", provider, fmt.Errorf("invalid %s response: %w", provider, err)
	}
	if len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", provider, fmt.Errorf("%s returned no answer", provider)
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), provider, nil
}
