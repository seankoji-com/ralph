package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

const workshopSystem = `You are Ralph's prompt workshop, a practical coding partner. Help the user turn a rough idea into a bounded prompt for an autonomous coding loop. Discuss tradeoffs, ask one useful question at a time, and preserve the user's choices. You only draft; you have no tools and cannot inspect the repository. Never claim to have read code. A loop starts a fresh agent context each iteration in a dedicated git worktree; the agent carries progress in .ralph-ledger.md. Prompts should state the goal, constraints, validation and a clear stopping condition. Never add publishing, merging, purchases or sending messages unless the user requested them. When asked to draft, output only the ready-to-run prompt, without fences or preamble. Keep conversation concise.`

func askAssistant(ctx context.Context, c Config, repo Repo, history []Message, draft bool) (string, error) {
	endpoint, err := providerURL(c, "chat/completions")
	if err != nil {
		return "", err
	}
	models, discoveryErr := providerModels(ctx, c)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	msgs := []Message{{Role: "system", Content: workshopSystem + "\nSelected repository: " + repo.Name + "\n\n" + reviewGuidance(c.Model, models, discoveryErr)}}
	msgs = append(msgs, history...)
	if draft {
		msgs = append(msgs, Message{Role: "user", Content: "Write the final loop prompt based on our conversation. Output only the prompt."})
	}
	body, err := json.Marshal(map[string]any{"model": c.AssistModel, "messages": msgs, "max_tokens": 3000, "temperature": 0.6})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	client := &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("LiteLLM request failed: %w", err)
	}
	defer resp.Body.Close()
	// Do not surface a proxy's raw error body: it may echo credentials or headers.
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LiteLLM returned HTTP %d; check endpoint, key and model %s", resp.StatusCode, c.AssistModel)
	}
	var result struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&result); err != nil {
		return "", fmt.Errorf("invalid LiteLLM response: %w", err)
	}
	if len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("LiteLLM returned no answer")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}
