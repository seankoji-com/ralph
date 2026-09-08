package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestProviderModelsUsesCredentialAndDeduplicates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer scoped-key" {
			t.Errorf("incorrect model discovery request")
		}
		fmt.Fprint(w, `{"data":[{"id":"writer"},{"id":"reviewer"},{"id":"reviewer"},{"id":""},{"id":"unsafe\u001bname"}]}`)
	}))
	defer server.Close()
	ids, err := providerModels(context.Background(), Config{BaseURL: server.URL + "/v1", APIKey: "scoped-key"})
	if err != nil || !reflect.DeepEqual(ids, []string{"reviewer", "writer"}) {
		t.Fatalf("models=%v err=%v", ids, err)
	}
}

func TestRetryableProviderStatuses(t *testing.T) {
	for _, status := range []int{408, 429, 500, 502, 503, 504, 599} {
		if !retryableProviderStatus(status) {
			t.Errorf("status %d should fall back", status)
		}
	}
	for _, status := range []int{200, 400, 401, 403, 404} {
		if retryableProviderStatus(status) {
			t.Errorf("status %d should not fall back", status)
		}
	}
}

func TestHealthyPrimaryResponseSurvivesUntilBodyIsRead(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
		fmt.Fprint(w, "complete response")
	}))
	defer server.Close()
	resp, _, err := providerRequest(context.Background(), Config{FallbackEnabled: true, BaseURL: server.URL, DevPassURL: server.URL, DevPassAPIKey: "fallback"}, http.MethodGet, "models", nil)
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "complete response" {
		t.Fatalf("healthy response lost: %q, %v", body, err)
	}
}

func TestReviewGuidanceHandlesUnavailableAlternatives(t *testing.T) {
	onlyWriter := reviewGuidance("litellm/writer", []string{"writer", "litellm/writer"}, nil)
	if !strings.Contains(onlyWriter, "no different reviewer model") {
		t.Fatal("same model offered as an independent reviewer")
	}
	unavailable := reviewGuidance("litellm/writer", []string{"stale-reviewer"}, fmt.Errorf("unavailable"))
	if strings.Contains(unavailable, "stale-reviewer") || !strings.Contains(unavailable, "could not verify") {
		t.Fatal("stale catalogue reused after failure")
	}
}

func TestAssistantContinuesWhenModelDiscoveryFails(t *testing.T) {
	var system string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(403)
			fmt.Fprint(w, "private-key-must-not-leak")
			return
		}
		var body struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		system = body.Messages[0].Content
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"I can suggest OCR, but could not verify a reviewer model."}}]}`)
	}))
	defer server.Close()
	answer, _, err := askAssistant(context.Background(), Config{BaseURL: server.URL + "/v1", Model: "litellm/writer"}, Repo{Name: "org/repo"}, []Message{{Role: "user", Content: "Fix the code"}}, false)
	if err != nil || answer == "" || !strings.Contains(system, "discovery was unavailable") || strings.Contains(system, "private-key") {
		t.Fatalf("answer=%s err=%v", answer, err)
	}
}

func TestLiveReviewerSuggestion(t *testing.T) {
	if os.Getenv("RALPH_LIVE_TEST") != "1" {
		t.Skip("set RALPH_LIVE_TEST=1 to call the configured provider")
	}
	c := loadConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	models, err := providerModels(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	answer, _, err := askAssistant(ctx, c, Repo{Name: c.Org + "/ralph"}, []Message{{Role: "user", Content: "I want Ralph to implement better keyboard navigation and tests in this Go TUI. Suggest a code-review workflow and one reviewer model from our provider, different from the coding model. Keep it brief."}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(answer), "ocr") && !strings.Contains(strings.ToLower(answer), "open code review") {
		t.Fatalf("no OCR suggestion: %s", answer)
	}
	_, writer, _ := strings.Cut(c.Model, "/")
	found := false
	for _, id := range models {
		if id != c.Model && id != writer && strings.Contains(answer, id) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no different live model suggested: %s", answer)
	}
	if strings.Contains(answer, "<provider>/") {
		t.Fatalf("invented OCR model prefix: %s", answer)
	}
	t.Logf("Provider exposed %d models. Workshop response:\n%s", len(models), answer)
}
